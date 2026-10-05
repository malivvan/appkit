//go:build linux || freebsd || netbsd

package dialog

import (
	"errors"
	"sync"
	"unsafe"

	"github.com/malivvan/purego"
)

const (
	rtldNoLoad = 0x4

	gtkFileChooserActionOpen         = 0
	gtkFileChooserActionSave         = 1
	gtkFileChooserActionSelectFolder = 2

	gtkResponseAccept = -3
)

var (
	initOnce sync.Once
	initErr  error
	gtk4     bool

	gtkInitCheck3 func(argc, argv uintptr) bool
	gtkInitCheck4 func() bool

	gtkFileChooserNativeNew         func(title string, parent uintptr, action int, accept, cancel string) uintptr
	gtkNativeDialogShow             func(dialog uintptr)
	gtkNativeDialogHide             func(dialog uintptr)
	gtkNativeDialogSetModal         func(dialog uintptr, modal bool)
	gtkFileChooserSetSelectMultiple func(chooser uintptr, multiple bool)
	gtkFileChooserSetCurrentName    func(chooser uintptr, name string)
	gtkFileFilterNew                func() uintptr
	gtkFileFilterSetName            func(filter uintptr, name string)
	gtkFileFilterAddPattern         func(filter uintptr, pattern string)
	gtkFileChooserAddFilter         func(chooser, filter uintptr)

	gtkFileChooserGetFilename      func(chooser uintptr) uintptr
	gtkFileChooserGetFilenames     func(chooser uintptr) uintptr
	gtkFileChooserSetCurrentFolder func(chooser uintptr, path string) bool
	gSListFree                     func(list uintptr)

	gtkFileChooserGetFile           func(chooser uintptr) uintptr
	gtkFileChooserGetFiles          func(chooser uintptr) uintptr
	gtkFileChooserSetCurrentFolder4 func(chooser, file, err uintptr) bool
	gFileNewForPath                 func(path string) uintptr
	gFileGetPath                    func(file uintptr) uintptr
	gListModelGetNItems             func(model uintptr) uint32
	gListModelGetItem               func(model uintptr, pos uint32) uintptr

	gFree                 func(p uintptr)
	gObjectUnref          func(obj uintptr)
	gSignalConnectData    func(instance uintptr, signal string, handler, data uintptr, destroy, flags uintptr) uint64
	gMainContextIteration func(ctx uintptr, mayBlock bool) bool

	responseCallback uintptr

	gtkInitOnce sync.Once
	gtkInitOK   bool
)

var (
	responseMu     sync.Mutex
	responseStates = map[uintptr]*responseState{}
	responseSeq    uintptr
)

type responseState struct {
	response int
	done     bool
}

func loadGTK() (uintptr, error) {
	lib, err := purego.Dlopen("libgtk-4.so.1", purego.RTLD_LAZY|rtldNoLoad)
	if err == nil {
		gtk4 = true
		return lib, nil
	}
	lib, err = purego.Dlopen("libgtk-3.so.0", purego.RTLD_LAZY|rtldNoLoad)
	if err == nil {
		return lib, nil
	}
	lib, err = purego.Dlopen("libgtk-3.so.0", purego.RTLD_LAZY|purego.RTLD_GLOBAL)
	if err == nil {
		return lib, nil
	}
	lib, err = purego.Dlopen("libgtk-4.so.1", purego.RTLD_LAZY|purego.RTLD_GLOBAL)
	if err == nil {
		gtk4 = true
		return lib, nil
	}
	return 0, errors.New("dialog: neither libgtk-3.so.0 nor libgtk-4.so.1 could be loaded")
}

func ensureInit() error {
	initOnce.Do(func() {
		gtkLib, err := loadGTK()
		if err != nil {
			initErr = err
			return
		}
		glib, err := purego.Dlopen("libglib-2.0.so.0", purego.RTLD_LAZY|purego.RTLD_GLOBAL)
		if err != nil {
			initErr = err
			return
		}
		gobject, err := purego.Dlopen("libgobject-2.0.so.0", purego.RTLD_LAZY|purego.RTLD_GLOBAL)
		if err != nil {
			initErr = err
			return
		}

		purego.RegisterLibFunc(&gtkFileChooserNativeNew, gtkLib, "gtk_file_chooser_native_new")
		purego.RegisterLibFunc(&gtkNativeDialogShow, gtkLib, "gtk_native_dialog_show")
		purego.RegisterLibFunc(&gtkNativeDialogHide, gtkLib, "gtk_native_dialog_hide")
		purego.RegisterLibFunc(&gtkNativeDialogSetModal, gtkLib, "gtk_native_dialog_set_modal")
		purego.RegisterLibFunc(&gtkFileChooserSetSelectMultiple, gtkLib, "gtk_file_chooser_set_select_multiple")
		purego.RegisterLibFunc(&gtkFileChooserSetCurrentName, gtkLib, "gtk_file_chooser_set_current_name")
		purego.RegisterLibFunc(&gtkFileFilterNew, gtkLib, "gtk_file_filter_new")
		purego.RegisterLibFunc(&gtkFileFilterSetName, gtkLib, "gtk_file_filter_set_name")
		purego.RegisterLibFunc(&gtkFileFilterAddPattern, gtkLib, "gtk_file_filter_add_pattern")
		purego.RegisterLibFunc(&gtkFileChooserAddFilter, gtkLib, "gtk_file_chooser_add_filter")

		purego.RegisterLibFunc(&gFree, glib, "g_free")
		purego.RegisterLibFunc(&gMainContextIteration, glib, "g_main_context_iteration")
		purego.RegisterLibFunc(&gObjectUnref, gobject, "g_object_unref")
		purego.RegisterLibFunc(&gSignalConnectData, gobject, "g_signal_connect_data")

		responseCallback = purego.NewCallback(func(_, responseID, token uintptr) uintptr {
			responseMu.Lock()
			st := responseStates[token]
			if st != nil {
				st.response = int(int32(uint32(responseID)))
				st.done = true
			}
			responseMu.Unlock()
			return 0
		})

		if gtk4 {
			purego.RegisterLibFunc(&gtkInitCheck4, gtkLib, "gtk_init_check")
			purego.RegisterLibFunc(&gtkFileChooserGetFile, gtkLib, "gtk_file_chooser_get_file")
			purego.RegisterLibFunc(&gtkFileChooserGetFiles, gtkLib, "gtk_file_chooser_get_files")
			purego.RegisterLibFunc(&gtkFileChooserSetCurrentFolder4, gtkLib, "gtk_file_chooser_set_current_folder")
			gio, e := purego.Dlopen("libgio-2.0.so.0", purego.RTLD_LAZY|purego.RTLD_GLOBAL)
			if e != nil {
				initErr = e
				return
			}
			purego.RegisterLibFunc(&gFileNewForPath, gio, "g_file_new_for_path")
			purego.RegisterLibFunc(&gFileGetPath, gio, "g_file_get_path")
			purego.RegisterLibFunc(&gListModelGetNItems, gio, "g_list_model_get_n_items")
			purego.RegisterLibFunc(&gListModelGetItem, gio, "g_list_model_get_item")
			return
		}
		purego.RegisterLibFunc(&gtkInitCheck3, gtkLib, "gtk_init_check")
		purego.RegisterLibFunc(&gtkFileChooserGetFilename, gtkLib, "gtk_file_chooser_get_filename")
		purego.RegisterLibFunc(&gtkFileChooserGetFilenames, gtkLib, "gtk_file_chooser_get_filenames")
		purego.RegisterLibFunc(&gtkFileChooserSetCurrentFolder, gtkLib, "gtk_file_chooser_set_current_folder")
		purego.RegisterLibFunc(&gSListFree, glib, "g_slist_free")
	})
	return initErr
}

func gtkReady() bool {
	err := ensureInit()
	if err != nil {
		return false
	}
	gtkInitOnce.Do(func() {
		if gtk4 {
			gtkInitOK = gtkInitCheck4()
			return
		}
		gtkInitOK = gtkInitCheck3(0, 0)
	})
	return gtkInitOK
}

func runChooser(action int, multi bool, opts Options) []string {
	if !gtkReady() {
		return nil
	}
	accept := "_Open"
	if action == gtkFileChooserActionSave {
		accept = "_Save"
	}
	dlg := gtkFileChooserNativeNew(opts.Title, 0, action, accept, "_Cancel")
	if dlg == 0 {
		return nil
	}
	defer gObjectUnref(dlg)

	if multi {
		gtkFileChooserSetSelectMultiple(dlg, true)
	}
	if opts.Directory != "" {
		setFolder(dlg, opts.Directory)
	}
	if action == gtkFileChooserActionSave && opts.Filename != "" {
		gtkFileChooserSetCurrentName(dlg, opts.Filename)
	}
	if action != gtkFileChooserActionSelectFolder {
		if len(opts.Filters) > 0 {
			addFilters(dlg, opts.Filters)
		} else {
			addExtensionFilter(dlg, opts.Extensions)
		}
	}

	if showNativeDialog(dlg) != gtkResponseAccept {
		return nil
	}
	return chooserSelection(dlg, multi)
}

func showNativeDialog(dlg uintptr) int {
	responseMu.Lock()
	responseSeq++
	token := responseSeq
	st := &responseState{}
	responseStates[token] = st
	responseMu.Unlock()
	defer func() {
		responseMu.Lock()
		delete(responseStates, token)
		responseMu.Unlock()
	}()

	gSignalConnectData(dlg, "response", responseCallback, token, 0, 0)
	gtkNativeDialogSetModal(dlg, true)
	gtkNativeDialogShow(dlg)
	for {
		responseMu.Lock()
		done := st.done
		responseMu.Unlock()
		if done {
			break
		}
		gMainContextIteration(0, true)
	}
	gtkNativeDialogHide(dlg)
	return st.response
}

func setFolder(chooser uintptr, dir string) {
	if gtk4 {
		file := gFileNewForPath(dir)
		if file == 0 {
			return
		}
		gtkFileChooserSetCurrentFolder4(chooser, file, 0)
		gObjectUnref(file)
		return
	}
	gtkFileChooserSetCurrentFolder(chooser, dir)
}

func addExtensionFilter(chooser uintptr, exts []string) {
	clean := normalizeExtensions(exts)
	if clean == nil {
		return
	}
	filter := gtkFileFilterNew()
	name := ""
	for i, e := range clean {
		if i > 0 {
			name += ", "
		}
		name += "*." + e
	}
	gtkFileFilterSetName(filter, name)
	for _, e := range clean {
		gtkFileFilterAddPattern(filter, "*."+e)
	}
	gtkFileChooserAddFilter(chooser, filter)
}

func addFilters(chooser uintptr, filters []FileFilter) {
	for _, f := range filters {
		clean := normalizeExtensions(f.Extensions)
		if clean == nil {
			continue
		}
		name := f.Name
		if name == "" {
			name = ""
			for i, e := range clean {
				if i > 0 {
					name += ", "
				}
				name += "*." + e
			}
		}
		filter := gtkFileFilterNew()
		gtkFileFilterSetName(filter, name)
		for _, e := range clean {
			gtkFileFilterAddPattern(filter, "*."+e)
		}
		gtkFileChooserAddFilter(chooser, filter)
	}
}

func chooserSelection(chooser uintptr, multi bool) []string {
	if gtk4 {
		if multi {
			return listModelPaths(gtkFileChooserGetFiles(chooser))
		}
		file := gtkFileChooserGetFile(chooser)
		if file == 0 {
			return nil
		}
		defer gObjectUnref(file)
		if p := filePathOf(file); p != "" {
			return []string{p}
		}
		return nil
	}
	if multi {
		return sListPaths(gtkFileChooserGetFilenames(chooser))
	}
	cs := gtkFileChooserGetFilename(chooser)
	if cs == 0 {
		return nil
	}
	p := cstr(cs)
	gFree(cs)
	if p == "" {
		return nil
	}
	return []string{p}
}

func filePathOf(file uintptr) string {
	cs := gFileGetPath(file)
	if cs == 0 {
		return ""
	}
	p := cstr(cs)
	gFree(cs)
	return p
}

func listModelPaths(model uintptr) []string {
	if model == 0 {
		return nil
	}
	defer gObjectUnref(model)
	n := gListModelGetNItems(model)
	paths := make([]string, 0, n)
	for i := uint32(0); i < n; i++ {
		file := gListModelGetItem(model, i)
		if file == 0 {
			continue
		}
		if p := filePathOf(file); p != "" {
			paths = append(paths, p)
		}
		gObjectUnref(file)
	}
	return paths
}

func sListPaths(list uintptr) []string {
	if list == 0 {
		return nil
	}
	var paths []string
	for node := list; node != 0; {
		data := *(*uintptr)(ptr(node))
		next := *(*uintptr)(ptr(node + unsafe.Sizeof(uintptr(0))))
		if data != 0 {
			if p := cstr(data); p != "" {
				paths = append(paths, p)
			}
			gFree(data)
		}
		node = next
	}
	gSListFree(list)
	return paths
}

func open(opts Options) string {
	return firstOrEmpty(runChooser(gtkFileChooserActionOpen, false, opts))
}

func openMultiple(opts Options) []string {
	return runChooser(gtkFileChooserActionOpen, true, opts)
}

func save(opts Options) string {
	return firstOrEmpty(runChooser(gtkFileChooserActionSave, false, opts))
}

func pickDirectory(opts Options) string {
	return firstOrEmpty(runChooser(gtkFileChooserActionSelectFolder, false, opts))
}

func ptr(u uintptr) unsafe.Pointer { return *(*unsafe.Pointer)(unsafe.Pointer(&u)) }

func cstr(p uintptr) string {
	if p == 0 {
		return ""
	}
	base := ptr(p)
	var n int
	for *(*byte)(unsafe.Add(base, n)) != 0 {
		n++
	}
	return string(unsafe.Slice((*byte)(base), n))
}

package dialog

import (
	"fmt"
	"sync"
	"unsafe"

	"github.com/ebitengine/purego/objc"
)

const nsModalResponseOK = 1

var selCache sync.Map

func sel(name string) objc.SEL {
	v, ok := selCache.Load(name)
	if ok {
		return v.(objc.SEL)
	}
	s := objc.RegisterName(name)
	selCache.Store(name, s)
	return s
}

func class(name string) objc.ID {
	c := objc.GetClass(name)
	if c == 0 {
		panic(fmt.Sprintf("dialog: objc class %q not found", name))
	}
	return objc.ID(c)
}

func nsstr(s string) objc.ID {
	return class("NSString").Send(sel("stringWithUTF8String:"), s)
}

func cstr(id objc.ID) string {
	if id == 0 {
		return ""
	}
	ptr := *(*unsafe.Pointer)(unsafe.Pointer(&id))
	var n int
	for *(*byte)(unsafe.Add(ptr, n)) != 0 {
		n++
	}
	return string(unsafe.Slice((*byte)(ptr), n))
}

func autorelease(f func()) {
	pool := class("NSAutoreleasePool").Send(sel("alloc")).Send(sel("init"))
	defer pool.Send(sel("drain"))
	f()
}

func restoreFocus(app, prev objc.ID) {
	if prev != 0 {
		prev.Send(sel("makeKeyAndOrderFront:"), objc.ID(0))
	}
	app.Send(sel("activateIgnoringOtherApps:"), true)
}

func allowedExtensions(exts []string) objc.ID {
	clean := normalizeExtensions(exts)
	if clean == nil {
		return 0
	}
	arr := class("NSMutableArray").Send(sel("array"))
	for _, e := range clean {
		arr.Send(sel("addObject:"), nsstr(e))
	}
	return arr
}

func applyCommonOptions(panel objc.ID, opts Options) {
	if opts.Title != "" {
		panel.Send(sel("setMessage:"), nsstr(opts.Title))
	}
	if opts.Directory != "" {
		url := class("NSURL").Send(sel("fileURLWithPath:"), nsstr(opts.Directory))
		panel.Send(sel("setDirectoryURL:"), url)
	}
	types := allowedExtensions(extensionList(opts))
	if types != 0 {
		panel.Send(sel("setAllowedFileTypes:"), types)
	}
}

func runOpenPanel(opts Options, chooseDirs, multiple bool) []string {
	if chooseDirs {
		opts.Extensions = nil
		opts.Filters = nil
	}
	var paths []string
	autorelease(func() {
		app := class("NSApplication").Send(sel("sharedApplication"))
		prev := app.Send(sel("keyWindow"))
		defer restoreFocus(app, prev)
		panel := class("NSOpenPanel").Send(sel("openPanel"))
		panel.Send(sel("setCanChooseFiles:"), !chooseDirs)
		panel.Send(sel("setCanChooseDirectories:"), chooseDirs)
		panel.Send(sel("setAllowsMultipleSelection:"), multiple)
		applyCommonOptions(panel, opts)
		if int(panel.Send(sel("runModal"))) != nsModalResponseOK {
			return
		}
		urls := panel.Send(sel("URLs"))
		if urls == 0 {
			return
		}
		n := int(urls.Send(sel("count")))
		for i := range n {
			u := urls.Send(sel("objectAtIndex:"), uint(i))
			if p := cstr(u.Send(sel("path")).Send(sel("UTF8String"))); p != "" {
				paths = append(paths, p)
			}
		}
	})
	return paths
}

func open(opts Options) string {
	return firstOrEmpty(runOpenPanel(opts, false, false))
}

func openMultiple(opts Options) []string {
	return runOpenPanel(opts, false, true)
}

func pickDirectory(opts Options) string {
	return firstOrEmpty(runOpenPanel(opts, true, false))
}

func save(opts Options) string {
	var path string
	autorelease(func() {
		app := class("NSApplication").Send(sel("sharedApplication"))
		prev := app.Send(sel("keyWindow"))
		defer restoreFocus(app, prev)
		panel := class("NSSavePanel").Send(sel("savePanel"))
		if opts.Filename != "" {
			panel.Send(sel("setNameFieldStringValue:"), nsstr(opts.Filename))
		}
		applyCommonOptions(panel, opts)
		if int(panel.Send(sel("runModal"))) != nsModalResponseOK {
			return
		}
		u := panel.Send(sel("URL"))
		if u != 0 {
			path = cstr(u.Send(sel("path")).Send(sel("UTF8String")))
		}
	})
	return path
}

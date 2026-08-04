//go:build linux

package appkit

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"runtime"
	"strings"
	"sync"
	"unsafe"

	"github.com/ebitengine/purego"
)

const (
	gtkWindowToplevel = 0

	gPriorityHighIdle = 100
	gSourceRemove     = 0

	injectTopFrame        = 1
	injectAtDocumentStart = 0

	gdkHintMaxSize   = 1 << 2
	gSignalMatchData = 1 << 4

	defaultWidth  = 640
	defaultHeight = 480

	jscGCRealtimeSignal = 34
)

const (
	envBackendGTK4 = "webkitgtk-6.0"
	envBackendGTK3 = "webkit2gtk-4.1"
)

const (
	backendAuto int = iota
	backendGTK4
	backendGTK3
)

type gdkGeometry struct {
	MinWidth, MinHeight   int32
	MaxWidth, MaxHeight   int32
	BaseWidth, BaseHeight int32
	WidthInc, HeightInc   int32
	MinAspect, MaxAspect  float64
	WinGravity            int32
	_                     int32
}

const gtkStyleProviderPriorityApplication = 600

const gdkStateMaximized = 1 << 1

const gdkMemoryR8G8B8A8Premultiplied = 2

var (
	gIdleAddFull                     func(priority int, function, data, notify uintptr) uint32
	gMainContextIteration            func(context uintptr, mayBlock bool) bool
	gMainContextDefault              func() uintptr
	gMainContextIsOwner              func(context uintptr) bool
	gFree                            func(asPointer uintptr)
	gObjectRefSink                   func(obj uintptr) uintptr
	gObjectUnref                     func(obj uintptr)
	gSignalConnectData               func(instance uintptr, signal string, handler, data, destroy uintptr, flags int) uint64
	gSignalHandlersDisconnectMatched func(instance uintptr, mask int, signalID, detail uint32, closure, fn, data uintptr) uint32
	gBytesNew                        func(data unsafe.Pointer, size uintptr) uintptr
	gListAppend                      func(list, data uintptr) uintptr
	gSetPrgname                      func(name string)

	gtkInitCheck              func(argc, argv uintptr) bool
	gtkWindowNew              func(typ int) uintptr
	gtkWindowSetResizable     func(window uintptr, resizable bool)
	gtkWindowSetDecorated     func(window uintptr, decorated bool)
	gtkWindowResize           func(window uintptr, w, h int)
	gtkWidgetSetSizeRequest   func(widget uintptr, w, h int)
	gtkWindowSetGeometryHints func(window, widget uintptr, geom *gdkGeometry, mask int)
	gtkContainerAdd           func(container, widget uintptr)
	gtkContainerRemove        func(container, widget uintptr)
	gtkWidgetShow             func(widget uintptr)
	gtkWidgetHide             func(widget uintptr)
	gtkWidgetGrabFocus        func(widget uintptr)
	gtkWindowPresent          func(window uintptr)
	gtkWindowClose            func(window uintptr)
	gtkWindowMaximize         func(window uintptr)
	gtkWindowUnmaximize       func(window uintptr)
	gtkWindowIconify          func(window uintptr)
	gtkWindowDeiconify        func(window uintptr)
	gtkWindowMinimize         func(window uintptr)
	gtkWindowUnminimize       func(window uintptr)

	gdkDisplayGetDefaultSeat func(display uintptr) uintptr
	gdkSeatGetPointer        func(seat uintptr) uintptr
	gtkWidgetGetDisplay      func(widget uintptr) uintptr

	gdkDisplayGetDefault func() uintptr
	gdkDisplayGetName    func(display uintptr) uintptr
	gtkWidgetGetWindow   func(widget uintptr) uintptr

	gdkScreenGetDefault              func() uintptr
	gdkScreenGetRGBAVisual           func(screen uintptr) uintptr
	gtkWidgetSetVisual               func(widget, visual uintptr)
	gtkWidgetOverrideBackgroundColor func(widget uintptr, state int32, color *[4]float64)
	webkitWebViewSetBackgroundColor  func(webview uintptr, color *[4]float64)

	gtkCssProviderNew            func() uintptr
	gtkCssProviderLoadFromString func(provider uintptr, css string, length int)
	gtkWidgetGetStyleContext     func(widget uintptr) uintptr
	gtkStyleContextAddProvider   func(context, provider uintptr, priority uint32)

	gtkWindowBeginMoveDrag3   func(window uintptr, button int32, rootX, rootY int32, timestamp uint32)
	gtkWindowBeginResizeDrag3 func(window uintptr, edge int32, button int32, rootX, rootY int32, timestamp uint32)
	gdkToplevelBeginMove      func(toplevel, device uintptr, button int32, x, y float64, timestamp uint32)
	gdkToplevelBeginResize    func(toplevel uintptr, edge int32, device uintptr, button int32, x, y float64, timestamp uint32)
	gtkNativeGetSurface       func(native uintptr) uintptr

	gtkWindowSetDefaultIcon func(icon uintptr)
	gtkWindowSetIcon        func(window, icon uintptr)
	gdkPixbufNewFromData    func(data unsafe.Pointer, colorspace, hasAlpha, bitsPerSample, width, height, rowstride int32, destroyNotify, destroyData uintptr) uintptr
	gdkMemoryTextureNew     func(width, height, format int32, bytes, stride uintptr) uintptr
	gdkToplevelSetIconList  func(toplevel, list uintptr)
	haveGdkIcons            bool

	gdkX11SurfaceGetXid      func(surface uintptr) uintptr
	gdkX11DisplayGetXdisplay func(display uintptr) uintptr
	xInternAtom              func(display uintptr, name string, onlyIfExists int32) uintptr
	xChangeProperty          func(display, window, property, typeAtom uintptr, format int32, mode int32, data uintptr, nelements int32) int32
	haveX11Icons             bool

	gdkToplevelGetState func(toplevel uintptr) uint32
	gdkWindowGetState   func(window uintptr) uint32

	gtk4                    bool
	gtkInitCheck0           func() bool
	gtkWindowNew0           func() uintptr
	gtkWindowSetChild       func(window, widget uintptr)
	gtkWidgetSetVisible     func(widget uintptr, visible bool)
	gtkWindowSetDefaultSize func(window uintptr, w, h int)
	webkitRegisterHandler3  func(manager uintptr, name string, world uintptr)

	webkitWebViewNew                              func() uintptr
	webkitWebViewGetUserContentManager            func(webview uintptr) uintptr
	webkitWebViewGetSettings                      func(webview uintptr) uintptr
	webkitSettingsSetEnableMediaStream            func(settings uintptr, enabled bool)
	webkitSettingsSetJavascriptCanAccessClipboard func(settings uintptr, enabled bool)
	webkitSettingsSetEnableWriteConsoleToStdout   func(settings uintptr, enabled bool)
	webkitSettingsSetEnableDeveloperExtras        func(settings uintptr, enabled bool)
	webkitSettingsSetEnableJavascript             func(settings uintptr, enabled bool)
	webkitWebViewLoadURI                          func(webview uintptr, uri string)
	webkitWebViewLoadHTML                         func(webview uintptr, html string, baseURI uintptr)
	webkitWebViewGetURI                           func(webview uintptr) uintptr
	webkitUserContentManagerRegisterHandler       func(manager uintptr, name string)
	webkitUserContentManagerAddScript             func(manager, script uintptr)
	webkitUserContentManagerRemoveAllScripts      func(manager uintptr)
	webkitUserScriptNew                           func(source string, frames, time int, allow, block uintptr) uintptr
	webkitUserScriptUnref                         func(script uintptr)
	webkitJavascriptResultGetJSValue              func(result uintptr) uintptr

	webkitWebViewEvaluateJavascript func(webview uintptr, script string, length int, world, source, cancellable, callback, userData uintptr)
	webkitWebViewRunJavascript      func(webview uintptr, script string, cancellable, callback, userData uintptr)
	haveEvaluateJavascript          bool

	jscValueToString func(value uintptr) uintptr
)

var (
	initOnce     sync.Once
	initErr      error
	uiThreadOnce sync.Once

	dispatchSourceFn uintptr
	messageHandlerFn uintptr
	windowDestroyFn  uintptr
	loadChangedFn    uintptr

	gtkLib, glibLib uintptr
)

var (
	regMu     sync.Mutex
	registry  = map[uintptr]*webview{}
	engineSeq uintptr

	dispatchMu  sync.Mutex
	dispatchMap = map[uintptr]func(){}
	dispatchSeq uintptr
)

var (
	appIconPixbuf uintptr
	appIconPix    []byte
	appIconList   uintptr
	appIconBytes  uintptr
	appIconARGB   []uintptr
)

var (
	announceCSDOnce sync.Once
	announceCSD     func(window uintptr)
)

type webview struct {
	id         uintptr
	window     uintptr
	webview    uintptr
	manager    uintptr
	ownsWindow bool

	stopRunLoop   bool
	isWindowShown bool
	isSizeSet     bool

	mu             sync.Mutex
	bindings       map[string]binding
	userScriptSrcs []string
	events         *events
	calls          workQueue

	eventsGlobal string

	onReady      func()
	onReadyFired bool
	serve        contentFunc
	schemeCB     uintptr

	contentBase string
	transient   *localServer
}

func dlopenFirst(names ...string) (uintptr, error) {
	var lastErr error
	for _, n := range names {
		h, err := purego.Dlopen(n, purego.RTLD_NOW|purego.RTLD_GLOBAL)
		if err == nil {
			return h, nil
		}
		lastErr = err
	}
	return 0, fmt.Errorf("webview: none of %v could be loaded: %w", names, lastErr)
}

func loadGTK4Stack() (gtk, webkit, jsc uintptr, err error) {
	webkit, err = dlopenFirst("libwebkitgtk-6.0.so.4")
	if err != nil {
		return 0, 0, 0, err
	}
	gtk, err = dlopenFirst("libgtk-4.so.1")
	if err != nil {
		return 0, 0, 0, err
	}
	jsc, err = dlopenFirst("libjavascriptcoregtk-6.0.so.1")
	if err != nil {
		return 0, 0, 0, err
	}
	return gtk, webkit, jsc, nil
}

func loadGTK3Stack() (gtk, webkit, jsc uintptr, err error) {
	gtk, err = dlopenFirst("libgtk-3.so.0")
	if err != nil {
		return 0, 0, 0, err
	}
	webkit, err = dlopenFirst("libwebkit2gtk-4.1.so.0", "libwebkit2gtk-4.0.so.37")
	if err != nil {
		return 0, 0, 0, err
	}
	jsc, err = dlopenFirst("libjavascriptcoregtk-4.1.so.0", "libjavascriptcoregtk-4.0.so.18")
	if err != nil {
		return 0, 0, 0, err
	}
	return gtk, webkit, jsc, nil
}

func linuxBackendOverride() int {
	v := os.Getenv("APPKIT_BACKEND")
	if v == "" {
		return backendAuto
	}
	switch v {
	case envBackendGTK4:
		return backendGTK4
	case envBackendGTK3:
		return backendGTK3
	}
	fmt.Fprintf(os.Stderr, "appkit: warning: APPKIT_BACKEND=%q is not a known value (want %q or %q); using the auto-detected stack\n",
		v, envBackendGTK4, envBackendGTK3)
	return backendAuto
}

func platformBackend() string {
	if gtk4 {
		return envBackendGTK4
	}
	return envBackendGTK3
}

func ensureInit() error {
	initOnce.Do(func() {
		_ = os.Unsetenv("JSC_SIGNAL_FOR_GC")
		_ = os.Setenv("JSC_useSharedArrayBuffer", "1")

		glib, err := dlopenFirst("libglib-2.0.so.0")
		if err != nil {
			initErr = err
			return
		}
		gobject, err := dlopenFirst("libgobject-2.0.so.0")
		if err != nil {
			initErr = err
			return
		}
		var gtk, webkit, jsc uintptr
		switch linuxBackendOverride() {
		case backendGTK4:
			var err error
			if gtk, webkit, jsc, err = loadGTK4Stack(); err != nil {
				fmt.Fprintf(os.Stderr, "appkit: warning: APPKIT_BACKEND=%s is not available on this system (%v); using the auto-detected stack\n", envBackendGTK4, err)
			} else {
				gtk4 = true
			}
		case backendGTK3:
			var err error
			if gtk, webkit, jsc, err = loadGTK3Stack(); err != nil {
				fmt.Fprintf(os.Stderr, "appkit: warning: APPKIT_BACKEND=%s is not available on this system (%v); using the auto-detected stack\n", envBackendGTK3, err)
			}
		}
		if gtk == 0 {
			var err error
			if gtk, webkit, jsc, err = loadGTK4Stack(); err == nil {
				gtk4 = true
			} else if gtk, webkit, jsc, err = loadGTK3Stack(); err != nil {
				initErr = err
				return
			}
		}

		gtkLib, glibLib = gtk, glib

		if runtime.GOOS == "linux" {
			if addr, e := purego.Dlsym(jsc, "JSConfigureSignalForGC"); e == nil {
				var configureSignalForGC func(sig int32)
				purego.RegisterFunc(&configureSignalForGC, addr)
				configureSignalForGC(jscGCRealtimeSignal)
			}
		}

		purego.RegisterLibFunc(&gIdleAddFull, glib, "g_idle_add_full")
		purego.RegisterLibFunc(&gMainContextIteration, glib, "g_main_context_iteration")
		purego.RegisterLibFunc(&gMainContextDefault, glib, "g_main_context_default")
		purego.RegisterLibFunc(&gMainContextIsOwner, glib, "g_main_context_is_owner")
		purego.RegisterLibFunc(&gFree, glib, "g_free")
		purego.RegisterLibFunc(&gBytesNew, glib, "g_bytes_new")
		purego.RegisterLibFunc(&gListAppend, glib, "g_list_append")
		purego.RegisterLibFunc(&gSetPrgname, glib, "g_set_prgname")
		purego.RegisterLibFunc(&gObjectRefSink, gobject, "g_object_ref_sink")
		purego.RegisterLibFunc(&gObjectUnref, gobject, "g_object_unref")
		purego.RegisterLibFunc(&gSignalConnectData, gobject, "g_signal_connect_data")
		purego.RegisterLibFunc(&gSignalHandlersDisconnectMatched, gobject, "g_signal_handlers_disconnect_matched")

		if gtk4 {
			purego.RegisterLibFunc(&gtkInitCheck0, gtk, "gtk_init_check")
			purego.RegisterLibFunc(&gtkWindowNew0, gtk, "gtk_window_new")
			purego.RegisterLibFunc(&gtkWindowSetChild, gtk, "gtk_window_set_child")
			purego.RegisterLibFunc(&gtkWidgetSetVisible, gtk, "gtk_widget_set_visible")
			purego.RegisterLibFunc(&gtkWindowSetDefaultSize, gtk, "gtk_window_set_default_size")
			purego.RegisterLibFunc(&gtkNativeGetSurface, gtk, "gtk_native_get_surface")
			purego.RegisterLibFunc(&gdkToplevelBeginMove, gtk, "gdk_toplevel_begin_move")
			purego.RegisterLibFunc(&gdkToplevelBeginResize, gtk, "gdk_toplevel_begin_resize")
			purego.RegisterLibFunc(&gdkToplevelGetState, gtk, "gdk_toplevel_get_state")
			purego.RegisterLibFunc(&gtkCssProviderNew, gtk, "gtk_css_provider_new")
			purego.RegisterLibFunc(&gtkCssProviderLoadFromString, gtk, "gtk_css_provider_load_from_string")
			purego.RegisterLibFunc(&gtkWidgetGetStyleContext, gtk, "gtk_widget_get_style_context")
			purego.RegisterLibFunc(&gtkStyleContextAddProvider, gtk, "gtk_style_context_add_provider")
			purego.RegisterLibFunc(&gtkWindowMinimize, gtk, "gtk_window_minimize")
			purego.RegisterLibFunc(&gtkWindowUnminimize, gtk, "gtk_window_unminimize")
			if _, e := purego.Dlsym(gtk, "gdk_toplevel_set_icon_list"); e == nil {
				purego.RegisterLibFunc(&gdkMemoryTextureNew, gtk, "gdk_memory_texture_new")
				purego.RegisterLibFunc(&gdkToplevelSetIconList, gtk, "gdk_toplevel_set_icon_list")
				haveGdkIcons = true
			}
			if _, e := purego.Dlsym(gtk, "gdk_x11_surface_get_xid"); e == nil {
				if xlib, e := dlopenFirst("libX11.so.6", "libX11.so"); e == nil {
					purego.RegisterLibFunc(&gdkX11SurfaceGetXid, gtk, "gdk_x11_surface_get_xid")
					purego.RegisterLibFunc(&gdkX11DisplayGetXdisplay, gtk, "gdk_x11_display_get_xdisplay")
					purego.RegisterLibFunc(&xInternAtom, xlib, "XInternAtom")
					purego.RegisterLibFunc(&xChangeProperty, xlib, "XChangeProperty")
					haveX11Icons = true
				}
			}
		} else {
			purego.RegisterLibFunc(&gtkInitCheck, gtk, "gtk_init_check")
			purego.RegisterLibFunc(&gtkWindowNew, gtk, "gtk_window_new")
			purego.RegisterLibFunc(&gtkContainerAdd, gtk, "gtk_container_add")
			purego.RegisterLibFunc(&gtkContainerRemove, gtk, "gtk_container_remove")
			purego.RegisterLibFunc(&gtkWidgetShow, gtk, "gtk_widget_show")
			purego.RegisterLibFunc(&gtkWidgetHide, gtk, "gtk_widget_hide")
			purego.RegisterLibFunc(&gtkWindowIconify, gtk, "gtk_window_iconify")
			purego.RegisterLibFunc(&gtkWindowDeiconify, gtk, "gtk_window_deiconify")
			purego.RegisterLibFunc(&gtkWindowResize, gtk, "gtk_window_resize")
			purego.RegisterLibFunc(&gtkWindowSetGeometryHints, gtk, "gtk_window_set_geometry_hints")
			purego.RegisterLibFunc(&gtkWindowBeginMoveDrag3, gtk, "gtk_window_begin_move_drag")
			purego.RegisterLibFunc(&gtkWindowBeginResizeDrag3, gtk, "gtk_window_begin_resize_drag")
			purego.RegisterLibFunc(&gdkWindowGetState, gtk, "gdk_window_get_state")
			purego.RegisterLibFunc(&gtkWindowSetDefaultIcon, gtk, "gtk_window_set_default_icon")
			purego.RegisterLibFunc(&gtkWindowSetIcon, gtk, "gtk_window_set_icon")
			if pb, e := dlopenFirst("libgdk_pixbuf-2.0.so.0", "libgdk_pixbuf-2.0.so"); e == nil {
				if _, se := purego.Dlsym(pb, "gdk_pixbuf_new_from_data"); se == nil {
					purego.RegisterLibFunc(&gdkPixbufNewFromData, pb, "gdk_pixbuf_new_from_data")
				}
			}
		}
		purego.RegisterLibFunc(&gtkWindowSetResizable, gtk, "gtk_window_set_resizable")
		purego.RegisterLibFunc(&gtkWindowSetDecorated, gtk, "gtk_window_set_decorated")
		purego.RegisterLibFunc(&gtkWidgetSetSizeRequest, gtk, "gtk_widget_set_size_request")
		purego.RegisterLibFunc(&gtkWidgetGrabFocus, gtk, "gtk_widget_grab_focus")
		purego.RegisterLibFunc(&gtkWidgetGetDisplay, gtk, "gtk_widget_get_display")
		purego.RegisterLibFunc(&gdkDisplayGetDefaultSeat, gtk, "gdk_display_get_default_seat")
		purego.RegisterLibFunc(&gdkSeatGetPointer, gtk, "gdk_seat_get_pointer")
		purego.RegisterLibFunc(&gdkDisplayGetDefault, gtk, "gdk_display_get_default")
		purego.RegisterLibFunc(&gdkDisplayGetName, gtk, "gdk_display_get_name")
		if !gtk4 {
			purego.RegisterLibFunc(&gtkWidgetGetWindow, gtk, "gtk_widget_get_window")
			purego.RegisterLibFunc(&gdkScreenGetDefault, gtk, "gdk_screen_get_default")
			purego.RegisterLibFunc(&gdkScreenGetRGBAVisual, gtk, "gdk_screen_get_rgba_visual")
			purego.RegisterLibFunc(&gtkWidgetSetVisual, gtk, "gtk_widget_set_visual")
			purego.RegisterLibFunc(&gtkWidgetOverrideBackgroundColor, gtk, "gtk_widget_override_background_color")
		}
		purego.RegisterLibFunc(&gtkWindowPresent, gtk, "gtk_window_present")
		purego.RegisterLibFunc(&gtkWindowClose, gtk, "gtk_window_close")
		purego.RegisterLibFunc(&gtkWindowMaximize, gtk, "gtk_window_maximize")
		purego.RegisterLibFunc(&gtkWindowUnmaximize, gtk, "gtk_window_unmaximize")

		purego.RegisterLibFunc(&webkitWebViewNew, webkit, "webkit_web_view_new")
		purego.RegisterLibFunc(&webkitWebViewGetUserContentManager, webkit, "webkit_web_view_get_user_content_manager")
		purego.RegisterLibFunc(&webkitWebViewGetSettings, webkit, "webkit_web_view_get_settings")
		purego.RegisterLibFunc(&webkitSettingsSetEnableMediaStream, webkit, "webkit_settings_set_enable_media_stream")
		purego.RegisterLibFunc(&webkitSettingsSetJavascriptCanAccessClipboard, webkit, "webkit_settings_set_javascript_can_access_clipboard")
		purego.RegisterLibFunc(&webkitSettingsSetEnableWriteConsoleToStdout, webkit, "webkit_settings_set_enable_write_console_messages_to_stdout")
		purego.RegisterLibFunc(&webkitSettingsSetEnableDeveloperExtras, webkit, "webkit_settings_set_enable_developer_extras")
		purego.RegisterLibFunc(&webkitSettingsSetEnableJavascript, webkit, "webkit_settings_set_enable_javascript")
		purego.RegisterLibFunc(&webkitWebViewLoadURI, webkit, "webkit_web_view_load_uri")
		purego.RegisterLibFunc(&webkitWebViewLoadHTML, webkit, "webkit_web_view_load_html")
		purego.RegisterLibFunc(&webkitWebViewGetURI, webkit, "webkit_web_view_get_uri")
		purego.RegisterLibFunc(&webkitUserContentManagerAddScript, webkit, "webkit_user_content_manager_add_script")
		purego.RegisterLibFunc(&webkitUserContentManagerRemoveAllScripts, webkit, "webkit_user_content_manager_remove_all_scripts")
		purego.RegisterLibFunc(&webkitUserScriptNew, webkit, "webkit_user_script_new")
		purego.RegisterLibFunc(&webkitUserScriptUnref, webkit, "webkit_user_script_unref")
		if gtk4 {
			purego.RegisterLibFunc(&webkitRegisterHandler3, webkit, "webkit_user_content_manager_register_script_message_handler")
		} else {
			purego.RegisterLibFunc(&webkitUserContentManagerRegisterHandler, webkit, "webkit_user_content_manager_register_script_message_handler")
			purego.RegisterLibFunc(&webkitJavascriptResultGetJSValue, webkit, "webkit_javascript_result_get_js_value")
		}

		_, e := purego.Dlsym(webkit, "webkit_web_view_evaluate_javascript")
		if e == nil {
			purego.RegisterLibFunc(&webkitWebViewEvaluateJavascript, webkit, "webkit_web_view_evaluate_javascript")
			haveEvaluateJavascript = true
		} else {
			purego.RegisterLibFunc(&webkitWebViewRunJavascript, webkit, "webkit_web_view_run_javascript")
		}

		purego.RegisterLibFunc(&jscValueToString, jsc, "jsc_value_to_string")

		if addr, e := purego.Dlsym(webkit, "webkit_web_view_set_background_color"); e == nil {
			purego.RegisterFunc(&webkitWebViewSetBackgroundColor, addr)
		}

		dispatchSourceFn = purego.NewCallback(func(data uintptr) uintptr {
			dispatchMu.Lock()
			f := dispatchMap[data]
			delete(dispatchMap, data)
			dispatchMu.Unlock()
			if f != nil {
				f()
			}
			return gSourceRemove
		})
		messageHandlerFn = purego.NewCallback(func(_, jsResult, userData uintptr) uintptr {
			w := lookupEngine(userData)
			if w != nil {
				w.onMessage(jsResultToString(jsResult))
			}
			return 0
		})
		windowDestroyFn = purego.NewCallback(func(_, userData uintptr) uintptr {
			w := lookupEngine(userData)
			if w != nil {
				w.onWindowDestroy()
			}
			return 0
		})
		loadChangedFn = purego.NewCallback(func(_, loadEvent, userData uintptr) uintptr {
			if int32(loadEvent) == 3 {
				if w := lookupEngine(userData); w != nil {
					w.markReady()
				}
			}
			return 0
		})
	})
	return initErr
}

func gtkInit() bool {
	if gtk4 {
		return gtkInitCheck0()
	}
	return gtkInitCheck(0, 0)
}

func gtkNewWindow() uintptr {
	if gtk4 {
		return gtkWindowNew0()
	}
	return gtkWindowNew(gtkWindowToplevel)
}

func registerScriptHandler(manager uintptr, name string) {
	if gtk4 {
		webkitRegisterHandler3(manager, name, 0)
		return
	}
	webkitUserContentManagerRegisterHandler(manager, name)
}

func jsResultToString(arg uintptr) string {
	value := arg
	if !gtk4 {
		value = webkitJavascriptResultGetJSValue(arg)
	}
	cs := jscValueToString(value)
	s := cString(cs)
	if cs != 0 {
		gFree(cs)
	}
	return s
}

func cString(p uintptr) string {
	if p == 0 {
		return ""
	}
	asPointer := *(*unsafe.Pointer)(unsafe.Pointer(&p))
	var n int
	for *(*byte)(unsafe.Add(asPointer, n)) != 0 {
		n++
	}
	return string(unsafe.Slice((*byte)(asPointer), n))
}

func resolveMemdup(glib uintptr) (func(mem unsafe.Pointer, size int) unsafe.Pointer, error) {
	if f, ok := memdupFn[uint64](glib, "g_memdup2"); ok {
		return f, nil
	}
	if f, ok := memdupFn[uint32](glib, "g_memdup"); ok {
		return f, nil
	}
	return nil, errors.New("neither g_memdup2 nor g_memdup is available")
}

func memdupFn[T uint32 | uint64](glib uintptr, symbol string) (func(mem unsafe.Pointer, size int) unsafe.Pointer, bool) {
	addr, err := purego.Dlsym(glib, symbol)
	if err != nil || addr == 0 {
		return nil, false
	}
	var f func(unsafe.Pointer, T) unsafe.Pointer
	purego.RegisterFunc(&f, addr)
	return func(mem unsafe.Pointer, size int) unsafe.Pointer { return f(mem, T(size)) }, true
}

func registerEngine(w *webview) uintptr {
	regMu.Lock()
	engineSeq++
	id := engineSeq
	registry[id] = w
	regMu.Unlock()
	return id
}

func unregisterEngine(id uintptr) {
	regMu.Lock()
	delete(registry, id)
	regMu.Unlock()
}

func lookupEngine(id uintptr) *webview {
	regMu.Lock()
	defer regMu.Unlock()
	return registry[id]
}

func dispatchMain(f func()) {
	dispatchMu.Lock()
	dispatchSeq++
	id := dispatchSeq
	dispatchMap[id] = f
	dispatchMu.Unlock()
	gIdleAddFull(gPriorityHighIdle, dispatchSourceFn, id, 0)
}

func performOnMain(f func()) {
	if ctx := gMainContextDefault(); ctx != 0 && gMainContextIsOwner(ctx) {
		f()
		return
	}
	done := make(chan struct{})
	dispatchMain(func() {
		defer close(done)
		f()
	})
	<-done
}

func newView(v *View, serve contentFunc) (*webview, error) {
	err := ensureInit()
	if err != nil {
		return nil, err
	}
	uiThreadOnce.Do(runtime.LockOSThread)

	w := &webview{
		ownsWindow: true,
		bindings:   map[string]binding{},
		serve:      serve,
	}
	w.id = registerEngine(w)
	err = w.initWindow(uintptr(v.window))
	if err != nil {
		unregisterEngine(w.id)
		return nil, err
	}
	err = w.registerSchemes()
	if err != nil {
		w.Destroy()
		return nil, err
	}
	st := webkitWebViewGetSettings(w.webview)
	webkitSettingsSetEnableMediaStream(st, true)
	webkitSettingsSetJavascriptCanAccessClipboard(st, true)
	webkitSettingsSetEnableJavascript(st, true)
	webkitSettingsSetEnableWriteConsoleToStdout(st, v.Debug)
	webkitSettingsSetEnableDeveloperExtras(st, v.Debug)
	w.applyTransparentBackground()
	if w.ownsWindow {
		w.pushUserScript(buildRegionScript(v.State != StateFixed, false, "unix"))
	}
	if w.ownsWindow {
		w.applyViewGeometry(v)
	}
	w.contentBase, w.transient, err = contentRootFor(v, false)
	if err != nil {
		w.Destroy()
		return nil, err
	}
	return w, nil
}

func pumpUI() {
	gMainContextIteration(0, true)
}

func wakeUI() {
	dispatchMain(func() {})
}

func (w *webview) initWindow(window uintptr) error {
	if window != 0 {
		w.window = window
		w.ownsWindow = false
	} else {
		if !gtkInit() {
			return errors.New("webview: gtk_init_check failed (no display?)")
		}
		w.window = gtkNewWindow()
		gtkWindowSetDecorated(w.window, false)
		gSignalConnectData(w.window, "destroy", windowDestroyFn, w.id, 0, 0)
	}

	w.webview = webkitWebViewNew()
	gObjectRefSink(w.webview)
	w.manager = webkitWebViewGetUserContentManager(w.webview)

	gSignalConnectData(w.webview, "load-changed", loadChangedFn, w.id, 0, 0)

	gSignalConnectData(w.manager, "script-message-received::__webview__",
		messageHandlerFn, w.id, 0, 0)
	registerScriptHandler(w.manager, "__webview__")

	w.pushUserScript(buildInitScript(bridgePostFn))
	return nil
}

func (w *webview) registerSchemes() error {
	if w.serve == nil {
		return nil
	}
	if w.webview == 0 {
		return errors.New("webview: register scheme: web view not created")
	}
	webkitSonames := []string{"libwebkit2gtk-4.1.so.0", "libwebkit2gtk-4.0.so.37"}
	if gtk4 {
		webkitSonames = []string{"libwebkitgtk-6.0.so.4"}
	}
	webkit, err := dlopenFirst(webkitSonames...)
	if err != nil {
		return fmt.Errorf("webview: register schemes: load webkit: %w", err)
	}
	gio, err := dlopenFirst("libgio-2.0.so.0")
	if err != nil {
		return fmt.Errorf("webview: register schemes: load gio: %w", err)
	}
	gobject, err := dlopenFirst("libgobject-2.0.so.0")
	if err != nil {
		return fmt.Errorf("webview: register schemes: load gobject: %w", err)
	}
	gFreeAddr, err := purego.Dlsym(glibLib, "g_free")
	if err != nil {
		return fmt.Errorf("webview: register schemes: resolve g_free: %w", err)
	}
	memdup, err := resolveMemdup(glibLib)
	if err != nil {
		return fmt.Errorf("webview: register schemes: %w", err)
	}

	var (
		getContext               func(uintptr) uintptr
		registerScheme           func(ctx uintptr, scheme string, cb, data, notify uintptr)
		getSecurityManager       func(uintptr) uintptr
		registerAsSecure         func(sm uintptr, scheme string)
		requestGetURI            func(uintptr) uintptr
		schemeRequestFinish      func(req, stream uintptr, streamLen int64, contentType string)
		schemeRequestFinishError func(req, err uintptr)
		memInputStreamNew        func(data unsafe.Pointer, length int, destroy uintptr) uintptr
		gObjectUnref             func(uintptr)
		newErrorLiteral          func(domain uint32, code int32, message string) uintptr
		freeError                func(err uintptr)
		ioErrorQuark             func() uint32
	)
	purego.RegisterLibFunc(&getContext, webkit, "webkit_web_view_get_context")
	purego.RegisterLibFunc(&registerScheme, webkit, "webkit_web_context_register_uri_scheme")
	purego.RegisterLibFunc(&getSecurityManager, webkit, "webkit_web_context_get_security_manager")
	purego.RegisterLibFunc(&registerAsSecure, webkit, "webkit_security_manager_register_uri_scheme_as_secure")
	purego.RegisterLibFunc(&requestGetURI, webkit, "webkit_uri_scheme_request_get_uri")
	purego.RegisterLibFunc(&schemeRequestFinish, webkit, "webkit_uri_scheme_request_finish")
	purego.RegisterLibFunc(&schemeRequestFinishError, webkit, "webkit_uri_scheme_request_finish_error")
	purego.RegisterLibFunc(&memInputStreamNew, gio, "g_memory_input_stream_new_from_data")
	purego.RegisterLibFunc(&gObjectUnref, gobject, "g_object_unref")
	purego.RegisterLibFunc(&newErrorLiteral, glibLib, "g_error_new_literal")
	purego.RegisterLibFunc(&freeError, glibLib, "g_error_free")
	purego.RegisterLibFunc(&ioErrorQuark, gio, "g_io_error_quark")

	ctx := getContext(w.webview)
	if ctx == 0 {
		return errors.New("webview: register schemes: web context is nil")
	}
	sm := getSecurityManager(ctx)
	if sm == 0 {
		return errors.New("webview: register schemes: security manager is nil")
	}

	w.schemeCB = purego.NewCallback(func(req uintptr, data uintptr) uintptr {
		eng := lookupEngine(data)
		if eng == nil {
			return 0
		}
		url := cString(requestGetURI(req))
		resp := invokeContentFunc(eng.serve, &contentRequest{URL: url})
		if resp == nil {
			const gIOErrorNotFound = 1
			gerr := newErrorLiteral(ioErrorQuark(), gIOErrorNotFound, "resource not found")
			schemeRequestFinishError(req, gerr)
			freeError(gerr)
			return 0
		}
		body, mime := resp.Body, contentMIME(resp)
		var dataPtr unsafe.Pointer
		if len(body) > 0 {
			dataPtr = memdup(unsafe.Pointer(&body[0]), len(body))
		}
		stream := memInputStreamNew(dataPtr, len(body), uintptr(gFreeAddr))
		schemeRequestFinish(req, stream, int64(len(body)), mime)
		gObjectUnref(stream)
		return 0
	})
	registerScheme(ctx, schemeName, w.schemeCB, w.id, 0)
	registerAsSecure(sm, schemeName)
	return nil
}

func (w *webview) applyTransparentBackground() {
	rgba := [4]float64{0, 0, 0, 0}
	if webkitWebViewSetBackgroundColor != nil && w.webview != 0 {
		webkitWebViewSetBackgroundColor(w.webview, &rgba)
	}
	if gtk4 {
		if w.ownsWindow {
			makeWindowTransparent(w.window)
		}
		return
	}
	if !w.ownsWindow {
		return
	}
	screen := gdkScreenGetDefault()
	if vis := gdkScreenGetRGBAVisual(screen); vis != 0 {
		gtkWidgetSetVisual(w.webview, vis)
		gtkWidgetSetVisual(w.window, vis)
	}
	gtkWidgetOverrideBackgroundColor(w.window, 0, &rgba)
}

func makeWindowTransparent(window uintptr) {
	if gtkCssProviderNew == nil || gtkCssProviderLoadFromString == nil ||
		gtkWidgetGetStyleContext == nil || gtkStyleContextAddProvider == nil {
		return
	}
	provider := gtkCssProviderNew()
	if provider == 0 {
		return
	}
	gtkCssProviderLoadFromString(provider, "window.background { background-color: transparent; }", -1)
	if ctx := gtkWidgetGetStyleContext(window); ctx != 0 {
		gtkStyleContextAddProvider(ctx, provider, gtkStyleProviderPriorityApplication)
	}
}

func (w *webview) onWindowDestroy() {
	unregisterEngine(w.id)
	w.window = 0
	dispatchMain(func() { w.stopRunLoop = true })
	onAppWindowClosed()
}

func (w *webview) Run() {
	w.stopRunLoop = false
	for !w.stopRunLoop {
		gMainContextIteration(0, true)
	}
}

func (w *webview) Terminate() {
	dispatchMain(func() { w.stopRunLoop = true })
}

func (w *webview) Dispatch(f func()) { dispatchMain(f) }

func (w *webview) Window() unsafe.Pointer {
	p := w.window
	return *(*unsafe.Pointer)(unsafe.Pointer(&p))
}

func (w *webview) Destroy() {
	w.dropLoopback()
	hadWindow := w.window != 0 && w.ownsWindow
	if w.window != 0 && w.ownsWindow {
		gSignalHandlersDisconnectMatched(w.window, gSignalMatchData, 0, 0, 0, 0, w.id)
		gtkWindowClose(w.window)
		w.window = 0
	}
	if w.webview != 0 {
		if w.manager != 0 {
			gSignalHandlersDisconnectMatched(w.manager, gSignalMatchData, 0, 0, 0, 0, w.id)
			w.manager = 0
		}
		gSignalHandlersDisconnectMatched(w.webview, gSignalMatchData, 0, 0, 0, 0, w.id)
		gObjectUnref(w.webview)
		w.webview = 0
	}
	unregisterEngine(w.id)
	if w.ownsWindow {
		if hadWindow {
			onAppWindowClosed()
		}
		done := false
		dispatchMain(func() { done = true })
		for i := 0; i < 10000 && !done; i++ {
			gMainContextIteration(0, true)
		}
	}
}

func (w *webview) applyWindowSize(width, height int, state State) {
	gtkWindowSetResizable(w.window, state != StateFixed)
	switch state {
	case StateMin:
		gtkWidgetSetSizeRequest(w.window, width, height)
	case StateMax:
		if !gtk4 {
			g := gdkGeometry{MaxWidth: int32(width), MaxHeight: int32(height)}
			gtkWindowSetGeometryHints(w.window, 0, &g, gdkHintMaxSize)
		}
	case StateFixed:
		gtkWidgetSetSizeRequest(w.window, width, height)
		w.applyInitialWindowSize(width, height)
	default:
		w.applyInitialWindowSize(width, height)
	}
	w.isSizeSet = true
	w.showWindow()
	w.Eval(fmt.Sprintf("if(window.__webview__){window.__webview__.onAppRegionState({resizable:%v})}", state != StateFixed))
}

func (w *webview) applyInitialWindowSize(width, height int) {
	if gtk4 {
		gtkWindowSetDefaultSize(w.window, width, height)
		return
	}
	gtkWindowResize(w.window, width, height)
}

func (w *webview) applyViewGeometry(v *View) {
	width, height := v.Width, v.Height
	if width == 0 && height == 0 {
		width, height = defaultWidth, defaultHeight
	}
	w.applyWindowSize(width, height, v.State)
}

func waylandDisplay() bool {
	d := gdkDisplayGetDefault()
	return d != 0 && strings.HasPrefix(cString(gdkDisplayGetName(d)), "wayland")
}

func x11Display() bool {
	d := gdkDisplayGetDefault()
	return d != 0 && strings.Contains(cString(gdkDisplayGetName(d)), ":")
}

func gdkButtonFor(domButton int32) int32 {
	switch domButton {
	case 1:
		return 2
	case 2:
		return 3
	default:
		return 1
	}
}

func (w *webview) beginWindowMove(p dragRequest) {
	if w.window == 0 {
		return
	}
	button := gdkButtonFor(p.Button)
	if gtk4 {
		w.beginWindowMoveGTK4(button, p.ClientX, p.ClientY, p.Time)
		return
	}
	gtkWindowBeginMoveDrag3(w.window, button, p.ScreenX, p.ScreenY, p.Time)
}

func (w *webview) beginWindowMoveGTK4(button int32, x, y float64, timestamp uint32) {
	surface := gtkNativeGetSurface(w.window)
	if surface == 0 {
		return
	}
	gdkToplevelBeginMove(surface, w.pointerDevice(), button, x, y, timestamp)
}

func (w *webview) beginWindowResize(p dragRequest) {
	if w.window == 0 {
		return
	}
	edge := gdkEdgeFor(p.Direction)
	if edge < 0 {
		return
	}
	button := gdkButtonFor(p.Button)
	if gtk4 {
		surface := gtkNativeGetSurface(w.window)
		if surface == 0 {
			return
		}
		gdkToplevelBeginResize(surface, edge, w.pointerDevice(), button, p.ClientX, p.ClientY, p.Time)
		return
	}
	gtkWindowBeginResizeDrag3(w.window, edge, button, p.ScreenX, p.ScreenY, p.Time)
}

func (w *webview) isMaximized() bool {
	if w.window == 0 {
		return false
	}
	var state uint32
	if gtk4 {
		surface := gtkNativeGetSurface(w.window)
		if surface == 0 {
			return false
		}
		state = gdkToplevelGetState(surface)
	} else {
		gdkWindow := gtkWidgetGetWindow(w.window)
		if gdkWindow == 0 {
			return false
		}
		state = gdkWindowGetState(gdkWindow)
	}
	return state&gdkStateMaximized != 0
}

func (w *webview) Maximized() bool {
	var max bool
	performOnMain(func() { max = w.isMaximized() })
	return max
}

func (w *webview) toggleMaximize() {
	if w.window == 0 {
		return
	}
	if w.isMaximized() {
		gtkWindowUnmaximize(w.window)
	} else {
		gtkWindowMaximize(w.window)
	}
}

func (w *webview) pointerDevice() uintptr {
	display := gtkWidgetGetDisplay(w.window)
	if display == 0 {
		return 0
	}
	seat := gdkDisplayGetDefaultSeat(display)
	if seat == 0 {
		return 0
	}
	return gdkSeatGetPointer(seat)
}

func (w *webview) resolveURL(url string) string {
	if w.contentBase != "" {
		if w.transient != nil && w.transient.isClosed() {
			w.transient = nil
			w.contentBase = ""
			return url
		}
		return resolveAppURL(w.contentBase, url)
	}
	return url
}

func (w *webview) Navigate(url string) {
	if url == "" {
		url = "about:blank"
	}
	url = w.resolveURL(url)
	webkitWebViewLoadURI(w.webview, url)
}

func (w *webview) loadHTML(html string) {
	webkitWebViewLoadHTML(w.webview, html, 0)
}

func (w *webview) Init(js string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.pushUserScript(js)
}

func (w *webview) Eval(js string) {
	if w.webview == 0 {
		return
	}
	if webkitWebViewGetURI(w.webview) == 0 {
		return
	}
	if haveEvaluateJavascript {
		webkitWebViewEvaluateJavascript(w.webview, js, len(js), 0, 0, 0, 0, 0)
	} else {
		webkitWebViewRunJavascript(w.webview, js, 0, 0, 0)
	}
}

func gtk3InstallAppIcon(pix []byte, w, h int) error {
	if gdkPixbufNewFromData == nil || gtkWindowSetDefaultIcon == nil {
		return nil
	}
	stride := 4 * w
	appIconPix = pix
	icon := gdkPixbufNewFromData(unsafe.Pointer(&appIconPix[0]),
		0, 1, 8,
		int32(w), int32(h), int32(stride), 0, 0)
	if icon == 0 {
		appIconPix = nil
		return errors.New("appkit: application icon: gdk_pixbuf_new_from_data failed")
	}
	appIconPixbuf = icon
	gtkWindowSetDefaultIcon(icon)
	return nil
}

func gtk4InstallAppIcon(pix []byte, w, h int) error {
	if !haveGdkIcons && !haveX11Icons {
		return nil
	}
	if haveX11Icons {
		data := make([]uintptr, 2+w*h)
		data[0], data[1] = uintptr(uint32(w)), uintptr(uint32(h))
		for i, p := 0, 2; i+3 < len(pix); i, p = i+4, p+1 {
			data[p] = uintptr(uint32(pix[i+3])<<24 | uint32(pix[i])<<16 | uint32(pix[i+1])<<8 | uint32(pix[i+2]))
		}
		appIconARGB = data
	}
	if !haveGdkIcons {
		return nil
	}
	stride := 4 * w
	premul := make([]byte, stride*h)
	for i := 0; i+3 < len(pix); i += 4 {
		r, g, b, a := pix[i], pix[i+1], pix[i+2], pix[i+3]
		if a == 255 {
			premul[i], premul[i+1], premul[i+2], premul[i+3] = r, g, b, a
			continue
		}
		aa := uint32(a)
		premul[i] = uint8((uint32(r)*aa + 127) / 255)
		premul[i+1] = uint8((uint32(g)*aa + 127) / 255)
		premul[i+2] = uint8((uint32(b)*aa + 127) / 255)
		premul[i+3] = a
	}
	appIconBytes = gBytesNew(unsafe.Pointer(&premul[0]), uintptr(len(premul)))
	if appIconBytes == 0 {
		return errors.New("appkit: application icon: g_bytes_new failed")
	}
	tex := gdkMemoryTextureNew(int32(w), int32(h), gdkMemoryR8G8B8A8Premultiplied, appIconBytes, uintptr(stride))
	if tex == 0 {
		appIconBytes = 0
		return errors.New("appkit: application icon: gdk_memory_texture_new failed")
	}
	appIconList = gListAppend(appIconList, tex)
	return nil
}

func (w *webview) applySurfaceIconList() {
	if !gtk4 || w.window == 0 {
		return
	}
	if appIconList != 0 {
		if surface := gtkNativeGetSurface(w.window); surface != 0 {
			gdkToplevelSetIconList(surface, appIconList)
		}
	}
	w.applyX11WindowIcon()
}

func (w *webview) applyX11WindowIcon() {
	if !haveX11Icons || len(appIconARGB) == 0 || w.window == 0 || !x11Display() {
		return
	}
	surface := gtkNativeGetSurface(w.window)
	if surface == 0 {
		return
	}
	xid := gdkX11SurfaceGetXid(surface)
	if xid == 0 {
		return
	}
	display := gdkX11DisplayGetXdisplay(gdkDisplayGetDefault())
	if display == 0 {
		return
	}
	atom := xInternAtom(display, "_NET_WM_ICON", 0)
	cardinal := xInternAtom(display, "CARDINAL", 0)
	if atom == 0 || cardinal == 0 {
		return
	}
	xChangeProperty(display, xid, atom, cardinal, 32, 0,
		uintptr(unsafe.Pointer(&appIconARGB[0])), int32(len(appIconARGB)))
}

func (w *webview) applyGTKWindowIcon() {
	if gtk4 || w.window == 0 || appIconPixbuf == 0 || gtkWindowSetIcon == nil {
		return
	}
	gtkWindowSetIcon(w.window, appIconPixbuf)
}

func (w *webview) showWindow() {
	if w.isWindowShown {
		return
	}
	if gtk4 {
		gtkWindowSetChild(w.window, w.webview)
		gtkWidgetSetVisible(w.webview, true)
	} else {
		gtkContainerAdd(w.window, w.webview)
		gtkWidgetShow(w.webview)
		if w.ownsWindow {
			w.applyGTKWindowIcon()
		}
	}
	if w.ownsWindow {
		gtkWidgetGrabFocus(w.webview)
		if gtk4 {
			gtkWidgetSetVisible(w.window, true)
		} else {
			gtkWidgetShow(w.window)
		}
		w.announceFramelessCSD()
	}
	w.isWindowShown = true
	w.applySurfaceIconList()
}

func (w *webview) announceFramelessCSD() {
	if gtk4 || !w.ownsWindow || w.window == 0 {
		return
	}
	announceCSDOnce.Do(func() {
		addr, err := purego.Dlsym(gtkLib, "gdk_wayland_window_announce_csd")
		if err == nil && addr != 0 {
			purego.RegisterFunc(&announceCSD, addr)
		}
	})
	if announceCSD == nil {
		return
	}
	if !waylandDisplay() {
		return
	}
	gw := gtkWidgetGetWindow(w.window)
	if gw == 0 {
		return
	}
	announceCSD(gw)
}

func (w *webview) Focus() {
	if w.webview == 0 {
		return
	}
	gtkWidgetGrabFocus(w.webview)
}

func (w *webview) Raise() {
	if w.window == 0 {
		return
	}
	gtkWindowPresent(w.window)
}

func (w *webview) Show() {
	if w.window == 0 {
		return
	}
	dispatchMain(func() {
		if gtk4 {
			gtkWindowUnminimize(w.window)
		} else {
			gtkWindowDeiconify(w.window)
		}
		if gtk4 {
			gtkWidgetSetVisible(w.window, true)
		} else {
			gtkWidgetShow(w.window)
		}
		gtkWindowPresent(w.window)
		w.applySurfaceIconList()
	})
}

func (w *webview) Hide() {
	if w.window == 0 {
		return
	}
	dispatchMain(func() {
		if gtk4 {
			gtkWidgetSetVisible(w.window, false)
		} else {
			gtkWidgetHide(w.window)
		}
	})
}

func (w *webview) Maximize() {
	if w.window == 0 {
		return
	}
	dispatchMain(func() { gtkWindowMaximize(w.window) })
}

func (w *webview) gtkBackendSwitch(gtk4Fn, gtk3Fn func(uintptr)) {
	if gtk4 {
		gtk4Fn(w.window)
	} else {
		gtk3Fn(w.window)
	}
}

func (w *webview) Minimize() {
	if w.window == 0 {
		return
	}
	dispatchMain(func() { w.gtkBackendSwitch(gtkWindowMinimize, gtkWindowIconify) })
}

func (w *webview) Unminimize() {
	if w.window == 0 {
		return
	}
	dispatchMain(func() { w.gtkBackendSwitch(gtkWindowUnminimize, gtkWindowDeiconify) })
}

func (w *webview) Unmaximize() {
	if w.window == 0 {
		return
	}
	dispatchMain(func() { gtkWindowUnmaximize(w.window) })
}

func (w *webview) BindBatch(batch []bindItem) error {
	prepared, live, err := stageBindings(batch)
	if err != nil {
		return err
	}
	w.mu.Lock()
	for _, p := range prepared {
		replaceBindings(w.bindings, p.entries)
	}
	w.rebuildScriptsLocked()
	w.mu.Unlock()
	w.Eval(buildLiveBindScript(live))
	return nil
}

func (w *webview) Unbind(name string) error {
	w.mu.Lock()
	_, exists := w.bindings[name]
	if !exists {
		w.mu.Unlock()
		return errors.New("name not bound")
	}
	for _, n := range []string{name, accessorGetterKey(name), accessorSetterKey(name)} {
		delete(w.bindings, n)
	}
	w.rebuildScriptsLocked()
	w.mu.Unlock()
	w.Eval(buildLiveUnbindScript(name))
	return nil
}

func (w *webview) pushUserScript(src string) {
	w.userScriptSrcs = append(w.userScriptSrcs, src)
	w.rebuildScriptsLocked()
}

func (w *webview) rebuildScriptsLocked() {
	if w.manager == 0 {
		return
	}
	webkitUserContentManagerRemoveAllScripts(w.manager)
	for _, src := range w.userScriptSrcs {
		addUserScript(w.manager, src)
	}
	addUserScript(w.manager, buildBindScript(w.bindingEntries()))
}

func addUserScript(manager uintptr, src string) {
	script := webkitUserScriptNew(src, injectTopFrame, injectAtDocumentStart, 0, 0)
	webkitUserContentManagerAddScript(manager, script)
	webkitUserScriptUnref(script)
}

func (w *webview) onMessage(body string) {
	var m struct {
		ID     string          `json:"id"`
		Method string          `json:"method"`
		Params json.RawMessage `json:"params"`
	}
	err := json.Unmarshal([]byte(body), &m)
	if err != nil {
		return
	}
	switch m.Method {
	case methodWindowDrag:
		w.beginWindowMove(parseDragRequest(m.Params))
		return
	case methodWindowResize:
		w.beginWindowResize(parseDragRequest(m.Params))
		return
	case methodWindowMaximize:
		w.toggleMaximize()
		return
	case methodBindError:
		reportBindFailure(m.Params)
		return
	}
	w.mu.Lock()
	b, ok := w.bindings[m.Method]
	w.mu.Unlock()
	if !ok || b.kind != bindingFunc {
		return
	}
	w.calls.do(func() {
		status, result := callBinding(b.fn, m.ID, string(m.Params))
		w.resolve(m.ID, status, result)
	})
}

func (w *webview) resolve(id string, status int, resultJSON string) {
	js := fmt.Sprintf("window.__webview__.onReply(%s, %d, %s)",
		jsonQuote(id), status, jsonQuote(resultJSON))
	dispatchMain(func() { w.Eval(js) })
}

const bridgePostFn = `function(message) {
  return window.webkit.messageHandlers.__webview__.postMessage(message);
}`

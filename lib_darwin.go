package appkit

import (
	"encoding/json"
	"errors"
	"fmt"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"unsafe"

	"github.com/ebitengine/purego"
	"github.com/ebitengine/purego/objc"

	"github.com/malivvan/appkit/dialog"
)

const (
	nsWindowStyleMaskResizable = 1 << 3

	nsBackingStoreBuffered = 2

	nsApplicationActivationPolicyRegular = 0

	nsEventTypeKeyDown            = 10
	nsEventTypeApplicationDefined = 15
	nsEventMaskAny                = ^uint(0)

	nsViewWidthSizable  = 1 << 1
	nsViewHeightSizable = 1 << 4

	nsModalResponseOK = 1

	wkPermissionDecisionGrant = 1

	nsURLErrorFileDoesNotExist = -1100

	wkInjectionTimeAtDocumentStart = 0

	defaultWidth  = 640
	defaultHeight = 480
)

type cgPoint struct{ X, Y float64 }

type cgSize struct{ Width, Height float64 }

type cgRect struct {
	Origin cgPoint
	Size   cgSize
}

var selectorCache sync.Map

func selector(name string) objc.SEL {
	v, ok := selectorCache.Load(name)
	if ok {
		return v.(objc.SEL)
	}
	s := objc.RegisterName(name)
	selectorCache.Store(name, s)
	return s
}

func objcClass(name string) objc.ID {
	c := objc.GetClass(name)
	if c == 0 {
		panic(fmt.Sprintf("appkit: objc class %q not found", name))
	}
	return objc.ID(c)
}

func nsString(s string) objc.ID {
	return objcClass("NSString").Send(selector("stringWithUTF8String:"), s)
}

func cString(id objc.ID) string {
	if id == 0 {
		return ""
	}
	asPointer := *(*unsafe.Pointer)(unsafe.Pointer(&id))
	var n int
	for *(*byte)(unsafe.Add(asPointer, n)) != 0 {
		n++
	}
	return string(unsafe.Slice((*byte)(asPointer), n))
}

func autorelease(f func()) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	pool := objcClass("NSAutoreleasePool").Send(selector("alloc")).Send(selector("init"))
	defer pool.Send(selector("drain"))
	f()
}

var (
	initOnce sync.Once
	initErr  error

	dispatchAsyncF func(queue, context, work uintptr)
	mainQueue      uintptr
	dispatchWork   uintptr

	appDelegateClass, scriptHandlerClass, windowDelegateClass, uiDelegateClass objc.Class
	schemeHandlerClass, firstMouseViewClass, borderlessWindowClass             objc.Class
)

var (
	regMu    sync.Mutex
	registry = map[objc.ID]*webview{}
)

var (
	dispatchMu  sync.Mutex
	dispatchMap = map[uintptr]func(){}
	dispatchSeq uintptr
)

var (
	uiIsMainOnce sync.Once
	uiIsMain     bool
)

var (
	firstMu      sync.Mutex
	notFirst     bool
	windowCount  int32
	uiThreadOnce sync.Once

	appkitRunsLoop atomic.Bool
)

type webview struct {
	app            objc.ID
	appDelegate    objc.ID
	windowDelegate objc.ID
	uiDelegate     objc.ID
	window         objc.ID
	widget         objc.ID
	webView        objc.ID
	manager        objc.ID
	scriptHandler  objc.ID

	ownsWindow            bool
	firstMouse            bool
	lastWidth, lastHeight int
	savedFrame            cgRect
	maximized             bool
	minimized             bool

	isSizeSet         bool
	isInitScriptAdded bool

	closed    chan struct{}
	closeOnce sync.Once

	mu             sync.Mutex
	bindings       map[string]binding
	userScriptSrcs []string
	events         *events
	calls          workQueue

	eventsGlobal string

	onReady      func()
	onReadyFired bool
	serve        contentFunc

	contentBase       string
	transient         *localServer
	schemeHandlerObjs []objc.ID
}

func ensureInit() error {
	initOnce.Do(func() {
		for _, fw := range []string{
			"/System/Library/Frameworks/Cocoa.framework/Cocoa",
			"/System/Library/Frameworks/WebKit.framework/WebKit",
		} {
			_, err := purego.Dlopen(fw, purego.RTLD_GLOBAL|purego.RTLD_LAZY)
			if err != nil {
				initErr = fmt.Errorf("webview: dlopen %s: %w", fw, err)
				return
			}
		}
		q, err := purego.Dlsym(purego.RTLD_DEFAULT, "_dispatch_main_q")
		if err != nil {
			initErr = fmt.Errorf("webview: resolve _dispatch_main_q: %w", err)
			return
		}
		mainQueue = q
		purego.RegisterLibFunc(&dispatchAsyncF, purego.RTLD_DEFAULT, "dispatch_async_f")
		dispatchWork = purego.NewCallback(func(ctx uintptr) uintptr {
			dispatchMu.Lock()
			f := dispatchMap[ctx]
			delete(dispatchMap, ctx)
			dispatchMu.Unlock()
			if f != nil {
				f()
			}
			return 0
		})
		initErr = registerAppClasses()
	})
	return initErr
}

func registerAppClasses() error {
	var err error
	appDelegateClass, err = objc.RegisterClass(
		"AppkitAppDelegate", objc.GetClass("NSResponder"),
		[]*objc.Protocol{objc.GetProtocol("NSTouchBarProvider")}, nil,
		[]objc.MethodDef{
			{
				Cmd: selector("applicationShouldTerminateAfterLastWindowClosed:"),
				Fn:  func(self objc.ID, _cmd objc.SEL, sender objc.ID) bool { return false },
			},
			{
				Cmd: selector("applicationShouldHandleReopen:hasVisibleWindows:"),
				Fn: func(self objc.ID, _cmd objc.SEL, sender objc.ID, hasVisible bool) bool {
					if w := lookupEngine(self); w != nil {
						w.restoreOnReopen()
						return false
					}
					return true
				},
			},
			{
				Cmd: selector("applicationDidFinishLaunching:"),
				Fn: func(self objc.ID, _cmd objc.SEL, notification objc.ID) {
					w := lookupEngine(self)
					if w != nil {
						w.appDidFinishLaunching(notification.Send(selector("object")))
					}
				},
			},
		})
	if err != nil {
		return fmt.Errorf("webview: app delegate class: %w", err)
	}

	scriptHandlerClass, err = objc.RegisterClass(
		"AppkitScriptMessageHandler", objc.GetClass("NSResponder"),
		[]*objc.Protocol{objc.GetProtocol("WKScriptMessageHandler")}, nil,
		[]objc.MethodDef{{
			Cmd: selector("userContentController:didReceiveScriptMessage:"),
			Fn: func(self objc.ID, _cmd objc.SEL, ucc objc.ID, message objc.ID) {
				w := lookupEngine(self)
				if w != nil {
					w.onMessage(cString(message.Send(selector("body")).Send(selector("UTF8String"))))
				}
			},
		}})
	if err != nil {
		return fmt.Errorf("webview: script handler class: %w", err)
	}

	windowDelegateClass, err = objc.RegisterClass(
		"AppkitWindowDelegate", objc.GetClass("NSObject"),
		[]*objc.Protocol{objc.GetProtocol("NSWindowDelegate")}, nil,
		[]objc.MethodDef{{
			Cmd: selector("windowWillClose:"),
			Fn: func(self objc.ID, _cmd objc.SEL, notification objc.ID) {
				w := lookupEngine(self)
				if w != nil {
					w.windowWillClose()
				}
			},
		}})
	if err != nil {
		return fmt.Errorf("webview: window delegate class: %w", err)
	}

	uiDelegateClass, err = objc.RegisterClass(
		"AppkitUIDelegate", objc.GetClass("NSObject"),
		[]*objc.Protocol{objc.GetProtocol("WKUIDelegate")}, nil,
		[]objc.MethodDef{
			{
				Cmd: selector("webView:runOpenPanelWithParameters:initiatedByFrame:completionHandler:"),
				Fn:  runOpenPanel,
			},
			{
				Cmd: selector("webView:requestMediaCapturePermissionForOrigin:initiatedByFrame:type:decisionHandler:"),
				Fn:  mediaCapturePermission,
			},
			{
				Cmd: selector("webView:didFinishNavigation:"),
				Fn: func(self objc.ID, _cmd objc.SEL, _webView, _navigation objc.ID) {
					if w := lookupEngine(self); w != nil {
						w.markReady()
					}
				},
			},
		})
	if err != nil {
		return fmt.Errorf("webview: ui delegate class: %w", err)
	}

	schemeHandlerClass, err = objc.RegisterClass(
		"AppkitURLSchemeHandler", objc.GetClass("NSObject"),
		[]*objc.Protocol{objc.GetProtocol("WKURLSchemeHandler")}, nil,
		[]objc.MethodDef{
			{Cmd: selector("webView:startURLSchemeTask:"), Fn: startURLSchemeTask},
			{Cmd: selector("webView:stopURLSchemeTask:"), Fn: stopURLSchemeTask},
		})
	if err != nil {
		return fmt.Errorf("webview: url scheme handler class: %w", err)
	}

	borderlessWindowClass, err = objc.RegisterClass(
		"AppkitBorderlessWindow", objc.GetClass("NSWindow"), nil, nil,
		[]objc.MethodDef{
			{
				Cmd: selector("canBecomeKeyWindow"),
				Fn:  func(self objc.ID, _cmd objc.SEL) bool { return true },
			},
			{
				Cmd: selector("canBecomeMainWindow"),
				Fn:  func(self objc.ID, _cmd objc.SEL) bool { return true },
			},
		})
	if err != nil {
		return fmt.Errorf("webview: borderless window class: %w", err)
	}

	firstMouseViewClass, err = objc.RegisterClass(
		"AppkitFirstMouseWebView", objc.GetClass("WKWebView"), nil, nil,
		[]objc.MethodDef{{
			Cmd: selector("acceptsFirstMouse:"),
			Fn:  func(self objc.ID, _cmd objc.SEL, event objc.ID) bool { return true },
		}})
	if err != nil {
		return fmt.Errorf("webview: first-mouse web view class: %w", err)
	}
	return nil
}

func newView(v *View, serve contentFunc) (*webview, error) {
	err := ensureInit()
	if err != nil {
		return nil, err
	}

	app := objcClass("NSApplication").Send(selector("sharedApplication"))
	loopRunning := app.Send(selector("isRunning")) != 0
	uiIsMainOnce.Do(func() { uiIsMain = onMainThread() || loopRunning })

	if !onMainThread() && loopRunning {
		var w *webview
		performOnMain(func() { w = newWebView(v, serve, app, loopRunning) })
		return w, nil
	}

	uiThreadOnce.Do(runtime.LockOSThread)
	return newWebView(v, serve, app, loopRunning), nil
}

func newWebView(v *View, serve contentFunc, app objc.ID, loopRunning bool) *webview {
	w := &webview{
		ownsWindow: true,
		firstMouse: v.FirstMouse,
		bindings:   map[string]binding{},
		serve:      serve,
		closed:     make(chan struct{}),
		lastWidth:  defaultWidth,
		lastHeight: defaultHeight,
	}
	w.app = app
	w.initWindow(objc.ID(uintptr(v.window)))
	autorelease(func() {
		rect := cgRect{cgPoint{0, 0}, cgSize{defaultWidth, defaultHeight}}

		config := objcClass("WKWebViewConfiguration").Send(selector("new"))
		config.Send(selector("autorelease"))
		w.manager = config.Send(selector("userContentController"))

		prefs := config.Send(selector("preferences"))
		devTools := v.Debug
		num := func(b bool) objc.ID {
			return objcClass("NSNumber").Send(selector("numberWithBool:"), b)
		}
		numF := func(f float64) objc.ID {
			return objcClass("NSNumber").Send(selector("numberWithDouble:"), f)
		}
		push := func(key, setter string, v objc.ID) {
			if prefs.Send(selector("respondsToSelector:"), selector(setter)) != 0 {
				prefs.Send(selector("setValue:forKey:"), v, nsString(key))
			}
		}
		push("javaScriptEnabled", "setJavaScript:", num(true))
		push("fullScreenEnabled", "setFullScreenEnabled:", num(true))
		push("developerExtrasEnabled", "setDeveloperExtrasEnabled:", num(devTools))
		push("javaScriptCanOpenWindowsAutomatically", "setJavaScriptCanOpenWindowsAutomatically:", num(true))
		push("minimumFontSize", "setMinimumFontSize:", numF(0))
		push("tabFocusesLinks", "setTabFocusesLinks:", num(false))
		push("textInteractionEnabled", "setTextInteractionEnabled:", num(true))
		push("siteSpecificQuirksModeEnabled", "setSiteSpecificQuirksModeEnabled:", num(true))
		push("elementFullscreenEnabled", "setElementFullscreenEnabled:", num(false))
		push("fraudulentWebsiteWarningEnabled", "setFraudulentWebsiteWarningEnabled:", num(true))
		push("shouldPrintBackgrounds", "setShouldPrintBackgrounds:", num(false))
		push("javaEnabled", "setJavaEnabled:", num(false))
		push("plugInsEnabled", "setPlugInsEnabled:", num(false))

		if w.serve != nil {
			sh := objc.ID(schemeHandlerClass).Send(selector("new"))
			sh.Send(selector("autorelease"))
			registerEngine(sh, w)
			w.schemeHandlerObjs = append(w.schemeHandlerObjs, sh)
			config.Send(selector("setURLSchemeHandler:forURLScheme:"), sh, nsString(schemeName))
		}

		viewClass := objc.Class(objcClass("WKWebView"))
		if w.firstMouse {
			viewClass = firstMouseViewClass
		}
		wv := objc.ID(viewClass).Send(selector("alloc"))
		wv = wv.Send(selector("initWithFrame:configuration:"), rect, config)
		w.webView = wv.Send(selector("retain"))
		w.webView.Send(selector("setAutoresizingMask:"), uint(nsViewWidthSizable|nsViewHeightSizable))
		if devTools {
			w.webView.Send(selector("setInspectable:"), true)
		}

		w.window.Send(selector("setOpaque:"), false)
		clear := objcClass("NSColor").Send(selector("clearColor"))
		w.window.Send(selector("setBackgroundColor:"), clear)
		w.webView.Send(selector("setValue:forKey:"),
			objcClass("NSNumber").Send(selector("numberWithBool:"), false), nsString("drawsBackground"))

		w.uiDelegate = objc.ID(uiDelegateClass).Send(selector("new"))
		registerEngine(w.uiDelegate, w)
		w.webView.Send(selector("setUIDelegate:"), w.uiDelegate)
		w.webView.Send(selector("setNavigationDelegate:"), w.uiDelegate)

		handler := objc.ID(scriptHandlerClass).Send(selector("new"))
		registerEngine(handler, w)
		handler.Send(selector("autorelease"))
		w.scriptHandler = handler
		w.manager.Send(selector("addScriptMessageHandler:name:"), handler, nsString("__webview__"))

		w.pushUserScript(buildInitScript(bridgePostFn))
		w.isInitScriptAdded = true

		widget := objcClass("NSView").Send(selector("alloc")).Send(selector("initWithFrame:"), rect)
		w.widget = widget.Send(selector("retain"))
		w.widget.Send(selector("setAutoresizesSubviews:"), true)
		w.widget.Send(selector("addSubview:"), w.webView)

		w.window.Send(selector("setContentView:"), w.widget)
		if w.ownsWindow {
			w.window.Send(selector("makeKeyAndOrderFront:"), objc.ID(0))
			w.window.Send(selector("makeFirstResponder:"), w.webView)
		}
	})
	if w.ownsWindow {
		w.pushUserScript(buildRegionScript(v.State != StateFixed, false, "darwin"))
	}
	if loopRunning && w.ownsWindow {
		w.Raise()
	}
	if w.ownsWindow {
		w.applyViewGeometry(v)
	}
	if base, tr, err := contentRootFor(v, true); err == nil {
		w.contentBase, w.transient = base, tr
	} else if tr != nil {
		closeLoopbackServer(tr)
	}
	return w
}

func platformBackend() string { return "WKWebView" }

func startURLSchemeTask(self objc.ID, _cmd objc.SEL, webView objc.ID, task objc.ID) {
	w := lookupEngine(self)
	if w == nil {
		return
	}
	req := task.Send(selector("request"))
	nsurl := req.Send(selector("URL"))
	urlStr := cString(nsurl.Send(selector("absoluteString")).Send(selector("UTF8String")))

	resp := invokeContentFunc(w.serve, &contentRequest{URL: urlStr})
	autorelease(func() {
		if resp == nil {
			nsErr := objcClass("NSError").Send(selector("errorWithDomain:code:userInfo:"),
				nsString("NSURLErrorDomain"), nsURLErrorFileDoesNotExist, objc.ID(0))
			task.Send(selector("didFailWithError:"), nsErr)
			return
		}
		body := resp.Body
		var dataPtr unsafe.Pointer
		if len(body) > 0 {
			dataPtr = unsafe.Pointer(&body[0])
		}
		data := objcClass("NSData").Send(selector("dataWithBytes:length:"), dataPtr, len(body))
		headers := objcClass("NSMutableDictionary").Send(selector("dictionary"))
		headers.Send(selector("setObject:forKey:"), nsString(contentMIME(resp)), nsString("Content-Type"))
		urlResp := objcClass("NSHTTPURLResponse").Send(selector("alloc")).Send(
			selector("initWithURL:statusCode:HTTPVersion:headerFields:"),
			nsurl, 200, nsString("HTTP/1.1"), headers)
		urlResp.Send(selector("autorelease"))
		task.Send(selector("didReceiveResponse:"), urlResp)
		task.Send(selector("didReceiveData:"), data)
		task.Send(selector("didFinish"))
	})
}

func stopURLSchemeTask(self objc.ID, _cmd objc.SEL, webView objc.ID, task objc.ID) {}

func runOpenPanel(self objc.ID, _cmd objc.SEL, webView, parameters, frame, completionHandler objc.ID) {
	autorelease(func() {
		allowsMultiple := parameters.Send(selector("allowsMultipleSelection")) != 0
		allowsDirs := parameters.Send(selector("allowsDirectories")) != 0

		panel := objcClass("NSOpenPanel").Send(selector("openPanel"))
		configureOpenPanel(panel, true, allowsDirs, allowsMultiple, dialog.Options{})

		var urls objc.ID
		if int(panel.Send(selector("runModal"))) == nsModalResponseOK {
			urls = panel.Send(selector("URLs"))
		}
		invokeOpenPanelCompletion(completionHandler, urls)
	})
}

func configureOpenPanel(panel objc.ID, canFiles, canDirs, multiple bool, opts dialog.Options) {
	panel.Send(selector("setCanChooseFiles:"), canFiles)
	panel.Send(selector("setCanChooseDirectories:"), canDirs)
	panel.Send(selector("setAllowsMultipleSelection:"), multiple)
	if opts.Title != "" {
		panel.Send(selector("setMessage:"), nsString(opts.Title))
	}
	if opts.Directory != "" {
		url := objcClass("NSURL").Send(selector("fileURLWithPath:"), nsString(opts.Directory))
		panel.Send(selector("setDirectoryURL:"), url)
	}
	types := openPanelTypes(opts)
	if types != 0 {
		panel.Send(selector("setAllowedFileTypes:"), types)
	}
}

func openPanelTypes(opts dialog.Options) objc.ID {
	var exts []string
	for _, f := range opts.Filters {
		exts = append(exts, f.Extensions...)
	}
	exts = append(exts, opts.Extensions...)
	var clean []string
	for _, e := range exts {
		e = strings.TrimPrefix(e, ".")
		if e == "" || e == "*" {
			return 0
		}
		clean = append(clean, e)
	}
	if len(clean) == 0 {
		return 0
	}
	arr := objcClass("NSMutableArray").Send(selector("array"))
	for _, e := range clean {
		arr.Send(selector("addObject:"), nsString(e))
	}
	return arr
}

func invokeOpenPanelCompletion(completionHandler, urls objc.ID) {
	sig := objcClass("NSMethodSignature").Send(selector("signatureWithObjCTypes:"), "v@?@")
	inv := objcClass("NSInvocation").Send(selector("invocationWithMethodSignature:"), sig)
	inv.Send(selector("setTarget:"), completionHandler)
	inv.Send(selector("setArgument:atIndex:"), unsafe.Pointer(&urls), 1)
	inv.Send(selector("invoke"))
}

func mediaCapturePermission(self objc.ID, _cmd objc.SEL, webView, origin, frame objc.ID, captureType int, decisionHandler objc.ID) {
	autorelease(func() {
		invokeCaptureDecision(decisionHandler, wkPermissionDecisionGrant)
	})
}

func invokeCaptureDecision(decisionHandler objc.ID, decision int) {
	sig := objcClass("NSMethodSignature").Send(selector("signatureWithObjCTypes:"), "v@?q")
	inv := objcClass("NSInvocation").Send(selector("invocationWithMethodSignature:"), sig)
	inv.Send(selector("setTarget:"), decisionHandler)
	inv.Send(selector("setArgument:atIndex:"), unsafe.Pointer(&decision), 1)
	inv.Send(selector("invoke"))
}

func registerEngine(id objc.ID, w *webview) {
	regMu.Lock()
	registry[id] = w
	regMu.Unlock()
}

func unregisterEngine(id objc.ID) {
	regMu.Lock()
	delete(registry, id)
	regMu.Unlock()
}

func lookupEngine(id objc.ID) *webview {
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
	dispatchAsyncF(mainQueue, id, dispatchWork)
}

func onMainThread() bool {
	return objcClass("NSThread").Send(selector("isMainThread")) != 0
}

func performOnMain(f func()) {
	if onMainThread() || !uiIsMain {
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

func claimFirstInstance() bool {
	firstMu.Lock()
	defer firstMu.Unlock()
	if notFirst {
		return false
	}
	notFirst = true
	return true
}

func incWindowCount() { atomic.AddInt32(&windowCount, 1) }

func decWindowCount() int32 { return atomic.AddInt32(&windowCount, -1) }

func (w *webview) initWindow(window objc.ID) {
	autorelease(func() {
		if window != 0 {
			w.window = window
			w.ownsWindow = false
			return
		}
		if w.app.Send(selector("isRunning")) != 0 || !claimFirstInstance() {
			w.continueWindowInit()
			return
		}
		w.appDelegate = objc.ID(appDelegateClass).Send(selector("new"))
		registerEngine(w.appDelegate, w)
		w.app.Send(selector("setDelegate:"), w.appDelegate)
		w.app.Send(selector("run"))
	})
}

func (w *webview) appDidFinishLaunching(app objc.ID) {
	if w.ownsWindow {
		w.stopRunLoop()
	}
	if !isAppBundled() {
		app.Send(selector("setActivationPolicy:"), nsApplicationActivationPolicyRegular)
		app.Send(selector("activateIgnoringOtherApps:"), true)
	}
	reapplyAppIcon()
	w.continueWindowInit()
}

func (w *webview) continueWindowInit() {
	autorelease(func() {
		win := objc.ID(borderlessWindowClass).Send(selector("alloc"))
		win = win.Send(selector("initWithContentRect:styleMask:backing:defer:"),
			cgRect{cgPoint{0, 0}, cgSize{defaultWidth, defaultHeight}},
			uint(0), nsBackingStoreBuffered, false)
		w.window = win.Send(selector("retain"))
		w.windowDelegate = objc.ID(windowDelegateClass).Send(selector("new"))
		registerEngine(w.windowDelegate, w)
		w.window.Send(selector("setDelegate:"), w.windowDelegate)
		incWindowCount()
	})
}

func (w *webview) stopRunLoop() {
	autorelease(func() {
		w.app.Send(selector("stop:"), objc.ID(0))
		postWakeEvent(w.app)
	})
}

func postWakeEvent(app objc.ID) {
	event := objcClass("NSEvent").Send(
		selector("otherEventWithType:location:modifierFlags:timestamp:windowNumber:context:subtype:data1:data2:"),
		nsEventTypeApplicationDefined, cgPoint{0, 0}, uint(0), float64(0), 0, objc.ID(0), int16(0), 0, 0)
	app.Send(selector("postEvent:atStart:"), event, true)
}

func (w *webview) windowWillClose() {
	if w.ownsWindow {
		onAppWindowClosed()
	}
	w.widget = 0
	w.webView = 0
	w.window = 0
	w.closeOnce.Do(func() { close(w.closed) })
	dispatchMain(func() { w.windowDestroyed(false) })
}

func (w *webview) windowDestroyed(skipTermination bool) {
	if !skipTermination && w.windowDelegate != 0 {
		unregisterEngine(w.windowDelegate)
	}
	if decWindowCount() <= 0 && !skipTermination && appkitRunsLoop.Load() {
		w.Terminate()
	}
}

func isAppBundled() bool {
	bundle := objcClass("NSBundle").Send(selector("mainBundle"))
	if bundle == 0 {
		return false
	}
	path := bundle.Send(selector("bundlePath"))
	return path.Send(selector("hasSuffix:"), nsString(".app")) != 0
}

func (w *webview) Run() {
	if w.app.Send(selector("isRunning")) != 0 {
		if onMainThread() {
			w.pumpUntilClosed()
			return
		}
		<-w.closed
		return
	}
	appkitRunsLoop.Store(true)
	w.app.Send(selector("run"))
	appkitRunsLoop.Store(false)
}

func (w *webview) pumpUntilClosed() {
	for {
		select {
		case <-w.closed:
			return
		default:
		}
		autorelease(func() {
			deadline := objcClass("NSDate").Send(selector("dateWithTimeIntervalSinceNow:"), 0.05)
			ev := w.app.Send(selector("nextEventMatchingMask:untilDate:inMode:dequeue:"),
				nsEventMaskAny, deadline, nsString("kCFRunLoopDefaultMode"), true)
			if ev != 0 {
				w.app.Send(selector("sendEvent:"), ev)
			}
		})
	}
}

func (w *webview) Terminate() {
	if w.app.Send(selector("isRunning")) != 0 && !appkitRunsLoop.Load() {
		w.closeOnce.Do(func() { close(w.closed) })
		return
	}
	dispatchMain(w.stopRunLoop)
}

func (w *webview) Dispatch(f func()) { dispatchMain(f) }

func (w *webview) Window() unsafe.Pointer {
	id := w.window
	return *(*unsafe.Pointer)(unsafe.Pointer(&id))
}

func (w *webview) Focus() {
	if w.window == 0 || w.webView == 0 {
		return
	}
	performOnMain(func() {
		autorelease(func() { w.window.Send(selector("makeFirstResponder:"), w.webView) })
	})
}

func (w *webview) Raise() {
	if w.window == 0 {
		return
	}
	performOnMain(func() {
		autorelease(func() {
			w.app.Send(selector("activateIgnoringOtherApps:"), true)
			w.window.Send(selector("makeKeyAndOrderFront:"), objc.ID(0))
		})
	})
}

func (w *webview) Show() {
	if w.window == 0 {
		return
	}
	performOnMain(func() {
		autorelease(func() {
			if w.window.Send(selector("isMiniaturized")) != 0 {
				w.window.Send(selector("deminiaturize:"), objc.ID(0))
			}
			w.minimized = false
			w.app.Send(selector("activateIgnoringOtherApps:"), true)
			w.window.Send(selector("makeKeyAndOrderFront:"), objc.ID(0))
		})
	})
}

func (w *webview) Hide() {
	if w.window == 0 {
		return
	}
	performOnMain(func() {
		autorelease(func() { w.window.Send(selector("orderOut:"), objc.ID(0)) })
	})
}

func (w *webview) Maximize() {
	if w.window == 0 {
		return
	}
	performOnMain(func() {
		autorelease(func() {
			if w.maximized {
				w.unmaximizeFrameless()
				return
			}
			w.maximizeFrameless()
		})
	})
}

func (w *webview) maximizeFrameless() {
	if w.window == 0 {
		return
	}
	if w.minimized {
		w.unminimizeFrameless()
	}
	w.savedFrame = objc.Send[cgRect](w.window, selector("frame"))
	target := w.visibleFrame(w.savedFrame)
	w.window.Send(selector("setFrame:display:"), target, true)
	w.maximized = true
}

func (w *webview) visibleFrame(frame cgRect) cgRect {
	screensSel := selector("screens")
	scr := objcClass("NSScreen").Send(selector("mainScreen"))
	if screens := objcClass("NSScreen").Send(screensSel); screens != 0 {
		n := int(screens.Send(selector("count")))
		midX := frame.Origin.X + frame.Size.Width/2
		midY := frame.Origin.Y + frame.Size.Height/2
		for i := 0; i < n; i++ {
			s := screens.Send(selector("objectAtIndex:"), i)
			f := objc.Send[cgRect](s, selector("frame"))
			if midX >= f.Origin.X && midX < f.Origin.X+f.Size.Width &&
				midY >= f.Origin.Y && midY < f.Origin.Y+f.Size.Height {
				scr = s
				break
			}
		}
	}
	if scr == 0 {
		return frame
	}
	return objc.Send[cgRect](scr, selector("visibleFrame"))
}

func (w *webview) Unmaximize() {
	if w.window == 0 {
		return
	}
	performOnMain(func() {
		autorelease(func() {
			w.unmaximizeFrameless()
		})
	})
}

func (w *webview) Maximized() bool {
	if w.window == 0 {
		return false
	}
	max := false
	performOnMain(func() {
		autorelease(func() { max = w.maximized })
	})
	return max
}

func (w *webview) unmaximizeFrameless() {
	if w.window == 0 {
		return
	}
	if !w.maximized {
		return
	}
	w.window.Send(selector("setFrame:display:"), w.savedFrame, true)
	w.maximized = false
}

func (w *webview) Minimize() {
	if w.window == 0 {
		return
	}
	performOnMain(func() {
		autorelease(func() {
			if w.maximized {
				w.unmaximizeFrameless()
			}
			w.window.Send(selector("orderOut:"), objc.ID(0))
			w.minimized = true
		})
	})
}

func (w *webview) restoreOnReopen() {
	if w.window == 0 {
		return
	}
	autorelease(func() {
		w.Show()
	})
}

func (w *webview) Unminimize() {
	if w.window == 0 {
		return
	}
	performOnMain(func() {
		autorelease(func() {
			w.unminimizeFrameless()
		})
	})
}

func (w *webview) unminimizeFrameless() {
	if w.window == 0 {
		return
	}
	if !w.minimized {
		return
	}
	w.window.Send(selector("makeKeyAndOrderFront:"), objc.ID(0))
	w.minimized = false
}

func (w *webview) applyViewGeometry(v *View) {
	width, height := v.Width, v.Height
	if width == 0 && height == 0 {
		width, height = defaultWidth, defaultHeight
	}
	performOnMain(func() {
		autorelease(func() {
			w.applyWindowSize(width, height, v.State)
			w.window.Send(selector("center"))
		})
	})
	w.isSizeSet = true
}

func (w *webview) applyWindowSize(width, height int, state State) {
	var style uint
	if state != StateFixed {
		style |= nsWindowStyleMaskResizable
	}
	w.window.Send(selector("setStyleMask:"), style)
	size := cgSize{float64(width), float64(height)}
	switch state {
	case StateMin:
		w.window.Send(selector("setContentMinSize:"), size)
	case StateMax:
		w.window.Send(selector("setContentMaxSize:"), size)
	default:
		w.window.Send(selector("setContentSize:"), size)
		w.lastWidth, w.lastHeight = width, height
	}
}

func (w *webview) Navigate(url string) {
	if url == "" {
		url = "about:blank"
	}
	if w.contentBase != "" {
		if w.transient != nil && w.transient.isClosed() {
			w.transient = nil
			w.contentBase = ""
		} else {
			url = resolveAppURL(w.contentBase, url)
		}
	}
	performOnMain(func() {
		autorelease(func() {
			nsurl := objcClass("NSURL").Send(selector("URLWithString:"), nsString(url))
			req := objcClass("NSURLRequest").Send(selector("requestWithURL:"), nsurl)
			w.webView.Send(selector("loadRequest:"), req)
		})
	})
}

func (w *webview) loadHTML(html string) {
	performOnMain(func() {
		autorelease(func() {
			w.webView.Send(selector("loadHTMLString:baseURL:"), nsString(html), objc.ID(0))
		})
	})
}

func (w *webview) Init(js string) {
	performOnMain(func() {
		w.mu.Lock()
		defer w.mu.Unlock()
		w.pushUserScript(js)
	})
}

func (w *webview) Eval(js string) {
	if w.webView == 0 {
		return
	}
	performOnMain(func() {
		autorelease(func() {
			w.webView.Send(selector("evaluateJavaScript:completionHandler:"), nsString(js), objc.ID(0))
		})
	})
}

func (w *webview) BindBatch(batch []bindItem) error {
	prepared, live, err := stageBindings(batch)
	if err != nil {
		return err
	}
	performOnMain(func() {
		w.mu.Lock()
		defer w.mu.Unlock()
		for _, p := range prepared {
			replaceBindings(w.bindings, p.entries)
		}
		w.rebuildScriptsLocked()
	})
	w.Eval(buildLiveBindScript(live))
	return nil
}

func (w *webview) Unbind(name string) error {
	var unbindErr error
	performOnMain(func() {
		w.mu.Lock()
		defer w.mu.Unlock()
		_, exists := w.bindings[name]
		if !exists {
			unbindErr = errors.New("name not bound")
			return
		}
		for _, n := range []string{name, accessorGetterKey(name), accessorSetterKey(name)} {
			delete(w.bindings, n)
		}
		w.rebuildScriptsLocked()
	})
	if unbindErr != nil {
		return unbindErr
	}
	w.Eval(buildLiveUnbindScript(name))
	return nil
}

func (w *webview) Destroy() {
	w.dropLoopback()
	performOnMain(func() { w.destroyWebViewOnUI() })
}

func (w *webview) destroyWebViewOnUI() {
	autorelease(func() {
		if w.window != 0 {
			if w.webView != 0 {
				if w.uiDelegate != 0 {
					w.webView.Send(selector("setUIDelegate:"), objc.ID(0))
					w.webView.Send(selector("setNavigationDelegate:"), objc.ID(0))
					unregisterEngine(w.uiDelegate)
					w.uiDelegate.Send(selector("release"))
					w.uiDelegate = 0
				}
				w.webView.Send(selector("release"))
				w.webView = 0
			}
			if w.widget != 0 {
				if w.widget == w.window.Send(selector("contentView")) {
					w.window.Send(selector("setContentView:"), objc.ID(0))
				}
				w.widget.Send(selector("release"))
				w.widget = 0
			}
			if w.ownsWindow {
				w.window.Send(selector("setDelegate:"), objc.ID(0))
				w.window.Send(selector("close"))
				w.windowDestroyed(true)
			}
			w.window = 0
		}
		if w.windowDelegate != 0 {
			unregisterEngine(w.windowDelegate)
			w.windowDelegate.Send(selector("release"))
			w.windowDelegate = 0
		}
		if w.appDelegate != 0 {
			w.app.Send(selector("setDelegate:"), objc.ID(0))
			unregisterEngine(w.appDelegate)
			w.appDelegate.Send(selector("release"))
			w.appDelegate = 0
		}
		if w.scriptHandler != 0 {
			unregisterEngine(w.scriptHandler)
			w.scriptHandler = 0
		}
		for _, sh := range w.schemeHandlerObjs {
			unregisterEngine(sh)
		}
		w.schemeHandlerObjs = nil
	})
	w.closeOnce.Do(func() { close(w.closed) })
	if w.ownsWindow && !appkitRunsLoop.Load() && w.app.Send(selector("isRunning")) == 0 {
		w.drainRunLoopQueue()
	}
}

func (w *webview) runEventLoopWhile(cond func() bool) {
	for i := 0; i < 10000 && cond(); i++ {
		autorelease(func() {
			ev := w.app.Send(selector("nextEventMatchingMask:untilDate:inMode:dequeue:"),
				nsEventMaskAny, objc.ID(0), nsString("kCFRunLoopDefaultMode"), true)
			if ev != 0 {
				w.app.Send(selector("sendEvent:"), ev)
			}
		})
	}
}

func (w *webview) drainRunLoopQueue() {
	var done atomic.Bool
	dispatchMain(func() { done.Store(true) })
	w.runEventLoopWhile(func() bool { return !done.Load() })
}

func (w *webview) pushUserScript(src string) {
	w.userScriptSrcs = append(w.userScriptSrcs, src)
	w.rebuildScriptsLocked()
}

func (w *webview) rebuildScriptsLocked() {
	if w.manager == 0 {
		return
	}
	autorelease(func() {
		w.manager.Send(selector("removeAllUserScripts"))
		for _, src := range w.userScriptSrcs {
			addWKScript(w.manager, src)
		}
		addWKScript(w.manager, buildBindScript(w.bindingEntries()))
	})
}

func addWKScript(manager objc.ID, src string) {
	s := objcClass("WKUserScript").Send(selector("alloc"))
	s = s.Send(selector("initWithSource:injectionTime:forMainFrameOnly:"),
		nsString(src), wkInjectionTimeAtDocumentStart, true)
	manager.Send(selector("addUserScript:"), s)
	s.Send(selector("release"))
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
	if m.Method == methodWindowDrag {
		w.beginWindowMove(parseDragRequest(m.Params))
		return
	}
	if m.Method == methodWindowMaximize {
		w.Maximize()
		return
	}
	if m.Method == methodWindowCursor {
		w.setEdgeCursor(parseCursor(m.Params).Edge)
		return
	}
	if m.Method == methodBindError {
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

func (w *webview) beginWindowMove(p dragRequest) {
	if w.window == 0 {
		return
	}
	performOnMain(func() {
		autorelease(func() {
			ev := w.app.Send(selector("currentEvent"))
			if ev == 0 {
				winNum := w.window.Send(selector("windowNumber"))
				loc := cgPoint{X: p.ClientX, Y: float64(w.lastHeight) - p.ClientY}
				ev = objcClass("NSEvent").Send(
					selector("mouseEventWithType:location:modifierFlags:timestamp:windowNumber:context:eventNumber:clickCount:pressure:"),
					1, loc, uint(0), float64(0), winNum, objc.ID(0), 0, 1, float32(1))
			}
			if ev != 0 {
				w.window.Send(selector("performWindowDragWithEvent:"), ev)
			}
		})
	})
}

func (w *webview) setEdgeCursor(edge string) {
	if w.window == 0 {
		return
	}
	performOnMain(func() {
		autorelease(func() {
			var cursorClassSelector string
			switch edge {
			case "n", "s":
				cursorClassSelector = "resizeUpDownCursor"
			case "e", "w":
				cursorClassSelector = "resizeLeftRightCursor"
			default:
				return
			}
			cursor := objcClass("NSCursor").Send(selector(cursorClassSelector))
			if cursor != 0 {
				cursor.Send(selector("set"))
			}
		})
	})
}

func (w *webview) resolve(id string, status int, resultJSON string) {
	js := fmt.Sprintf("window.__webview__.onReply(%s, %d, %s)",
		jsonQuote(id), status, jsonQuote(resultJSON))
	dispatchMain(func() { autorelease(func() { w.Eval(js) }) })
}

const bridgePostFn = `function(message) {
  return window.webkit.messageHandlers.__webview__.postMessage(message);
}`

func pumpUI() {
	app := objcClass("NSApplication").Send(selector("sharedApplication"))
	if app.Send(selector("isRunning")) == 0 {
		appkitRunsLoop.Store(true)
		app.Send(selector("run"))
		appkitRunsLoop.Store(false)
		return
	}
	for !appExitRequested() {
		autorelease(func() {
			deadline := objcClass("NSDate").Send(selector("dateWithTimeIntervalSinceNow:"), 0.05)
			ev := app.Send(selector("nextEventMatchingMask:untilDate:inMode:dequeue:"),
				nsEventMaskAny, deadline, nsString("kCFRunLoopDefaultMode"), true)
			if ev != 0 {
				app.Send(selector("sendEvent:"), ev)
			}
		})
	}
}

func wakeUI() {
	app := objcClass("NSApplication").Send(selector("sharedApplication"))
	performOnMain(func() {
		app.Send(selector("stop:"), objc.ID(0))
		postWakeEvent(app)
	})
}

package appkit

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"unsafe"

	"github.com/malivvan/purego"
)

var errNoWindow = errors.New("webview2: failed to create window")

const bridgePostFn = `function(message) {
  return window.chrome.webview.postMessage(message);
}`

var debugEnabled = os.Getenv("WEBVIEW2_DEBUG") != ""

func dbg(format string, a ...any) {
	if debugEnabled {
		fmt.Fprintf(os.Stderr, "[webview2] "+format+"\n", a...)
	}
}

func asPointer(u uintptr) unsafe.Pointer { return *(*unsafe.Pointer)(unsafe.Pointer(&u)) }

type guid struct {
	Data1 uint32
	Data2 uint16
	Data3 uint16
	Data4 [8]byte
}

func guidEqual(a, b *guid) bool {
	return a.Data1 == b.Data1 && a.Data2 == b.Data2 && a.Data3 == b.Data3 && a.Data4 == b.Data4
}

var (
	iidIUnknown             = guid{0x00000000, 0x0000, 0x0000, [8]byte{0xC0, 0, 0, 0, 0, 0, 0, 0x46}}
	iidEnvironmentComplete  = guid{0x4E8A3389, 0xC9D8, 0x4BD2, [8]byte{0xB6, 0xB5, 0x12, 0x4F, 0xEE, 0x6C, 0xC1, 0x4D}}
	iidControllerComplete   = guid{0x6C4819F3, 0xC9B7, 0x4260, [8]byte{0x81, 0x27, 0xC9, 0xF5, 0xBD, 0xE7, 0xF6, 0x8C}}
	iidController2          = guid{0xF0EC8882, 0x7EC5, 0x4118, [8]byte{0xA7, 0xC8, 0x59, 0x54, 0x0C, 0xFF, 0x17, 0xED}}
	iidMessageReceived      = guid{0x57213F19, 0x00E6, 0x49FA, [8]byte{0x8E, 0x07, 0x89, 0x8E, 0xA0, 0x1E, 0xCB, 0xD2}}
	iidScriptAdded          = guid{0xB99369F3, 0x9B11, 0x47B5, [8]byte{0xBC, 0x6F, 0x8E, 0x78, 0x95, 0xFC, 0xEA, 0x17}}
	iidWebResourceRequested = guid{0xAB00B74C, 0x15F1, 0x4646, [8]byte{0x80, 0xE8, 0xE7, 0x63, 0x41, 0xD2, 0x5D, 0x71}}
	iidNavigationCompleted  = guid{0xD33A35BF, 0x1C49, 0x4F98, [8]byte{0x93, 0xAB, 0x00, 0x6E, 0x05, 0x33, 0xFE, 0x1C}}

	iidSettings3 = guid{0xFDB5AB74, 0xAF33, 0x4854, [8]byte{0x84, 0xF0, 0x0A, 0x63, 0x1D, 0xEB, 0x5E, 0xBA}}
	iidSettings4 = guid{0xCB56846C, 0x4168, 0x4D53, [8]byte{0xB0, 0x4F, 0x03, 0xB6, 0xD6, 0x79, 0x6F, 0xF2}}
	iidSettings5 = guid{0x183E7052, 0x1D03, 0x43A0, [8]byte{0xAB, 0x99, 0x98, 0xE0, 0x43, 0xB6, 0x6B, 0x39}}
	iidSettings6 = guid{0x11CB3ACD, 0x9BC8, 0x43B8, [8]byte{0x83, 0xBF, 0xF4, 0x07, 0x53, 0x71, 0x4F, 0x87}}
	iidSettings7 = guid{0x488DC902, 0x35EF, 0x42D2, [8]byte{0xBC, 0x7D, 0x94, 0xB6, 0x5C, 0x4B, 0xC4, 0x9C}}
	iidSettings8 = guid{0x9E6B0E8F, 0x86AD, 0x4E81, [8]byte{0x81, 0x47, 0xA9, 0xB5, 0xED, 0xB6, 0x86, 0x50}}
	iidSettings9 = guid{0x0528A73B, 0xE92D, 0x49F4, [8]byte{0x92, 0x7A, 0xE5, 0x47, 0xDD, 0xDA, 0xA3, 0x7D}}
)

type unknownVtbl struct {
	QueryInterface uintptr
	AddRef         uintptr
	Release        uintptr
}

type coreWebView2EnvironmentVtbl struct {
	unknownVtbl
	CreateCoreWebView2Controller  uintptr
	CreateWebResourceResponse     uintptr
	GetBrowserVersionString       uintptr
	AddNewBrowserVersionAvailable uintptr
	RemoveNewBrowserVersionAvail  uintptr
}

type coreWebView2ControllerVtbl struct {
	unknownVtbl
	GetIsVisible                   uintptr
	PutIsVisible                   uintptr
	GetBounds                      uintptr
	PutBounds                      uintptr
	GetZoomFactor                  uintptr
	PutZoomFactor                  uintptr
	AddZoomFactorChanged           uintptr
	RemoveZoomFactorChanged        uintptr
	SetBoundsAndZoomFactor         uintptr
	MoveFocus                      uintptr
	AddMoveFocusRequested          uintptr
	RemoveMoveFocusRequested       uintptr
	AddGotFocus                    uintptr
	RemoveGotFocus                 uintptr
	AddLostFocus                   uintptr
	RemoveLostFocus                uintptr
	AddAcceleratorKeyPressed       uintptr
	RemoveAcceleratorKeyPressed    uintptr
	GetParentWindow                uintptr
	PutParentWindow                uintptr
	NotifyParentWindowPositionChng uintptr
	Close                          uintptr
	GetCoreWebView2                uintptr
}

type coreWebView2Controller2Vtbl struct {
	coreWebView2ControllerVtbl
	GetDefaultBackgroundColor uintptr
	PutDefaultBackgroundColor uintptr
}

type coreWebView2Vtbl struct {
	unknownVtbl
	GetSettings                            uintptr
	GetSource                              uintptr
	Navigate                               uintptr
	NavigateToString                       uintptr
	AddNavigationStarting                  uintptr
	RemoveNavigationStarting               uintptr
	AddContentLoading                      uintptr
	RemoveContentLoading                   uintptr
	AddSourceChanged                       uintptr
	RemoveSourceChanged                    uintptr
	AddHistoryChanged                      uintptr
	RemoveHistoryChanged                   uintptr
	AddNavigationCompleted                 uintptr
	RemoveNavigationCompleted              uintptr
	AddFrameNavigationStarting             uintptr
	RemoveFrameNavigationStarting          uintptr
	AddFrameNavigationCompleted            uintptr
	RemoveFrameNavigationCompleted         uintptr
	AddScriptDialogOpening                 uintptr
	RemoveScriptDialogOpening              uintptr
	AddPermissionRequested                 uintptr
	RemovePermissionRequested              uintptr
	AddProcessFailed                       uintptr
	RemoveProcessFailed                    uintptr
	AddScriptToExecuteOnDocumentCreated    uintptr
	RemoveScriptToExecuteOnDocCreated      uintptr
	ExecuteScript                          uintptr
	CapturePreview                         uintptr
	Reload                                 uintptr
	PostWebMessageAsJSON                   uintptr
	PostWebMessageAsString                 uintptr
	AddWebMessageReceived                  uintptr
	RemoveWebMessageReceived               uintptr
	CallDevToolsProtocolMethod             uintptr
	GetBrowserProcessID                    uintptr
	GetCanGoBack                           uintptr
	GetCanGoForward                        uintptr
	GoBack                                 uintptr
	GoForward                              uintptr
	GetDevToolsProtocolEventReceiver       uintptr
	Stop                                   uintptr
	AddNewWindowRequested                  uintptr
	RemoveNewWindowRequested               uintptr
	AddDocumentTitleChanged                uintptr
	RemoveDocumentTitleChanged             uintptr
	GetDocumentTitle                       uintptr
	AddHostObjectToScript                  uintptr
	RemoveHostObjectFromScript             uintptr
	OpenDevToolsWindow                     uintptr
	AddContainsFullScreenElementChanged    uintptr
	RemoveContainsFullScreenElementChanged uintptr
	GetContainsFullScreenElement           uintptr
	AddWebResourceRequested                uintptr
	RemoveWebResourceRequested             uintptr
	AddWebResourceRequestedFilter          uintptr
	RemoveWebResourceRequestedFilter       uintptr
}

type coreWebView2SettingsVtbl struct {
	unknownVtbl
	GetIsScriptEnabled                uintptr
	PutIsScriptEnabled                uintptr
	GetIsWebMessageEnabled            uintptr
	PutIsWebMessageEnabled            uintptr
	GetAreDefaultScriptDialogsEnabled uintptr
	PutAreDefaultScriptDialogsEnabled uintptr
	GetIsStatusBarEnabled             uintptr
	PutIsStatusBarEnabled             uintptr
	GetDevTools                       uintptr
	PutDevTools                       uintptr
	GetAreDefaultContextMenusEnabled  uintptr
	PutAreDefaultContextMenusEnabled  uintptr
	GetAreHostObjectsAllowed          uintptr
	PutAreHostObjectsAllowed          uintptr
	GetIsZoomControlEnabled           uintptr
	PutIsZoomControlEnabled           uintptr
	GetIsBuiltInErrorPageEnabled      uintptr
	PutIsBuiltInErrorPageEnabled      uintptr
}

type coreWebView2Settings2Vtbl struct {
	coreWebView2SettingsVtbl
	GetUserAgent uintptr
	PutUserAgent uintptr
}
type coreWebView2Settings3Vtbl struct {
	coreWebView2Settings2Vtbl
	GetAreBrowserAcceleratorKeysEnabled uintptr
	PutAreBrowserAcceleratorKeysEnabled uintptr
}
type coreWebView2Settings4Vtbl struct {
	coreWebView2Settings3Vtbl
	GetIsPasswordAutosaveEnabled uintptr
	PutIsPasswordAutosaveEnabled uintptr
	GetIsGeneralAutofillEnabled  uintptr
	PutIsGeneralAutofillEnabled  uintptr
}
type coreWebView2Settings5Vtbl struct {
	coreWebView2Settings4Vtbl
	GetIsPinchZoomEnabled uintptr
	PutIsPinchZoomEnabled uintptr
}
type coreWebView2Settings6Vtbl struct {
	coreWebView2Settings5Vtbl
	GetIsSwipeNavigationEnabled uintptr
	PutIsSwipeNavigationEnabled uintptr
}
type coreWebView2Settings7Vtbl struct {
	coreWebView2Settings6Vtbl
	GetHiddenPdfToolbarItems uintptr
	PutHiddenPdfToolbarItems uintptr
}
type coreWebView2Settings8Vtbl struct {
	coreWebView2Settings7Vtbl
	GetIsReputationCheckingRequired uintptr
	PutIsReputationCheckingRequired uintptr
}
type coreWebView2Settings9Vtbl struct {
	coreWebView2Settings8Vtbl
	GetIsNonClientRegionSupportEnabled uintptr
	PutIsNonClientRegionSupportEnabled uintptr
}

type messageArgsVtbl struct {
	unknownVtbl
	GetSource             uintptr
	GetWebMessageAsJSON   uintptr
	TryGetWebMessageAsStr uintptr
}

type environment struct{ vtbl *coreWebView2EnvironmentVtbl }
type controller struct{ vtbl *coreWebView2ControllerVtbl }
type controller2 struct{ vtbl *coreWebView2Controller2Vtbl }
type coreWebView2 struct{ vtbl *coreWebView2Vtbl }
type settings struct{ vtbl *coreWebView2SettingsVtbl }
type settings3i struct{ vtbl *coreWebView2Settings3Vtbl }
type settings4i struct{ vtbl *coreWebView2Settings4Vtbl }
type settings5i struct{ vtbl *coreWebView2Settings5Vtbl }
type settings6i struct{ vtbl *coreWebView2Settings6Vtbl }
type settings7i struct{ vtbl *coreWebView2Settings7Vtbl }
type settings8i struct{ vtbl *coreWebView2Settings8Vtbl }
type settings9i struct{ vtbl *coreWebView2Settings9Vtbl }
type messageArgs struct {
	vtbl *messageArgsVtbl
}

type navigationCompletedArgsVtbl struct {
	unknownVtbl
	GetIsSuccess uintptr
}

type navigationCompletedArgs struct {
	vtbl *navigationCompletedArgsVtbl
}

func asNavigationCompletedArgs(p uintptr) *navigationCompletedArgs {
	return (*navigationCompletedArgs)(asPointer(p))
}

func (i *navigationCompletedArgs) IsSuccess() bool {
	var ok int32
	purego.SyscallN(i.vtbl.GetIsSuccess, uintptr(unsafe.Pointer(i)), uintptr(unsafe.Pointer(&ok)))
	return ok != 0
}

func asEnvironment(p uintptr) *environment { return (*environment)(asPointer(p)) }
func asController(p uintptr) *controller   { return (*controller)(asPointer(p)) }
func asController2(p uintptr) *controller2 { return (*controller2)(asPointer(p)) }
func asWebView2(p uintptr) *coreWebView2   { return (*coreWebView2)(asPointer(p)) }
func asSettings(p uintptr) *settings       { return (*settings)(asPointer(p)) }

func asSettingsSafeRelease(p uintptr) *settings { return asSettings(p) }
func asSettings3(p uintptr) *settings3i         { return (*settings3i)(asPointer(p)) }
func asSettings4(p uintptr) *settings4i         { return (*settings4i)(asPointer(p)) }
func asSettings5(p uintptr) *settings5i         { return (*settings5i)(asPointer(p)) }
func asSettings6(p uintptr) *settings6i         { return (*settings6i)(asPointer(p)) }
func asSettings7(p uintptr) *settings7i         { return (*settings7i)(asPointer(p)) }
func asSettings8(p uintptr) *settings8i         { return (*settings8i)(asPointer(p)) }
func asSettings9(p uintptr) *settings9i         { return (*settings9i)(asPointer(p)) }
func asMessageArgs(p uintptr) *messageArgs      { return (*messageArgs)(asPointer(p)) }

func (i *settings) settingsQI(iid *guid) uintptr {
	var out uintptr
	hr := i.QueryInterface(iid, &out)
	if out == 0 || int32(hr) < 0 {
		if uint32(hr) != 0x80004002 {
			dbg("settingsQI(iid=%08x) hr=0x%08x", iid.Data1, uint32(hr))
		}
		return 0
	}
	return out
}
func (i *settings) QueryInterface(riid *guid, out *uintptr) uintptr {
	r, _, _ := purego.SyscallN(i.vtbl.QueryInterface, uintptr(unsafe.Pointer(i)), uintptr(unsafe.Pointer(riid)), uintptr(unsafe.Pointer(out)))
	return r
}
func (i *settings) Release() { purego.SyscallN(i.vtbl.Release, uintptr(unsafe.Pointer(i))) }

func (i *environment) CreateController(hwnd, handler uintptr) uintptr {
	r, _, _ := purego.SyscallN(i.vtbl.CreateCoreWebView2Controller, uintptr(unsafe.Pointer(i)), hwnd, handler)
	return r
}

func (i *environment) AddRef()  { purego.SyscallN(i.vtbl.AddRef, uintptr(unsafe.Pointer(i))) }
func (i *environment) Release() { purego.SyscallN(i.vtbl.Release, uintptr(unsafe.Pointer(i))) }
func (i *controller) GetCoreWebView2(out *uintptr) uintptr {
	r, _, _ := purego.SyscallN(i.vtbl.GetCoreWebView2, uintptr(unsafe.Pointer(i)), uintptr(unsafe.Pointer(out)))
	return r
}
func (i *controller) QueryInterface(riid *guid, out *uintptr) uintptr {
	r, _, _ := purego.SyscallN(i.vtbl.QueryInterface, uintptr(unsafe.Pointer(i)), uintptr(unsafe.Pointer(riid)), uintptr(unsafe.Pointer(out)))
	return r
}
func (i *controller) PutIsVisible(v bool) {
	purego.SyscallN(i.vtbl.PutIsVisible, uintptr(unsafe.Pointer(i)), boolToUint(v))
}

const moveFocusReasonProgrammatic = 0

func (i *controller) MoveFocus(reason uintptr) {
	purego.SyscallN(i.vtbl.MoveFocus, uintptr(unsafe.Pointer(i)), reason)
}

func (i *controller) getBounds(r *rect) {
	purego.SyscallN(i.vtbl.GetBounds, uintptr(unsafe.Pointer(i)), uintptr(unsafe.Pointer(r)))
}

func (i *controller) AddRef()  { purego.SyscallN(i.vtbl.AddRef, uintptr(unsafe.Pointer(i))) }
func (i *controller) Close()   { purego.SyscallN(i.vtbl.Close, uintptr(unsafe.Pointer(i))) }
func (i *controller) Release() { purego.SyscallN(i.vtbl.Release, uintptr(unsafe.Pointer(i))) }

func (i *controller2) PutDefaultBackgroundColor(color uint32) uintptr {
	r, _, _ := purego.SyscallN(i.vtbl.PutDefaultBackgroundColor, uintptr(unsafe.Pointer(i)), uintptr(color))
	return r
}

func (i *controller2) Release() { purego.SyscallN(i.vtbl.Release, uintptr(unsafe.Pointer(i))) }

func (i *coreWebView2) GetSettings(out *uintptr) uintptr {
	r, _, _ := purego.SyscallN(i.vtbl.GetSettings, uintptr(unsafe.Pointer(i)), uintptr(unsafe.Pointer(out)))
	return r
}
func (i *coreWebView2) Navigate(url *uint16) {
	purego.SyscallN(i.vtbl.Navigate, uintptr(unsafe.Pointer(i)), uintptr(unsafe.Pointer(url)))
}
func (i *coreWebView2) NavigateToString(html *uint16) {
	purego.SyscallN(i.vtbl.NavigateToString, uintptr(unsafe.Pointer(i)), uintptr(unsafe.Pointer(html)))
}
func (i *coreWebView2) ExecuteScript(js *uint16, handler uintptr) {
	purego.SyscallN(i.vtbl.ExecuteScript, uintptr(unsafe.Pointer(i)), uintptr(unsafe.Pointer(js)), handler)
}
func (i *coreWebView2) AddScript(js *uint16, handler uintptr) {
	purego.SyscallN(i.vtbl.AddScriptToExecuteOnDocumentCreated, uintptr(unsafe.Pointer(i)), uintptr(unsafe.Pointer(js)), handler)
}
func (i *coreWebView2) RemoveScript(id *uint16) {
	purego.SyscallN(i.vtbl.RemoveScriptToExecuteOnDocCreated, uintptr(unsafe.Pointer(i)), uintptr(unsafe.Pointer(id)))
}
func (i *coreWebView2) Release() {
	purego.SyscallN(i.vtbl.Release, uintptr(unsafe.Pointer(i)))
}
func (i *coreWebView2) AddWebMessageReceived(handler uintptr, token *uint64) {
	purego.SyscallN(i.vtbl.AddWebMessageReceived, uintptr(unsafe.Pointer(i)), handler, uintptr(unsafe.Pointer(token)))
}
func (i *coreWebView2) AddNavigationCompleted(handler uintptr, token *uint64) {
	purego.SyscallN(i.vtbl.AddNavigationCompleted, uintptr(unsafe.Pointer(i)), handler, uintptr(unsafe.Pointer(token)))
}
func (i *coreWebView2) AddWebResourceRequested(handler uintptr, token *uint64) {
	purego.SyscallN(i.vtbl.AddWebResourceRequested, uintptr(unsafe.Pointer(i)), handler, uintptr(unsafe.Pointer(token)))
}
func (i *coreWebView2) AddWebResourceRequestedFilter(uri *uint16, ctx uint32) {
	purego.SyscallN(i.vtbl.AddWebResourceRequestedFilter, uintptr(unsafe.Pointer(i)), uintptr(unsafe.Pointer(uri)), uintptr(ctx))
}
func (i *coreWebView2) AddRef() { purego.SyscallN(i.vtbl.AddRef, uintptr(unsafe.Pointer(i))) }

func (i *settings) PutIsScriptEnabled(v bool) {
	purego.SyscallN(i.vtbl.PutIsScriptEnabled, uintptr(unsafe.Pointer(i)), boolToUint(v))
}
func (i *settings) PutIsWebMessageEnabled(v bool) {
	purego.SyscallN(i.vtbl.PutIsWebMessageEnabled, uintptr(unsafe.Pointer(i)), boolToUint(v))
}
func (i *settings) PutAreDefaultScriptDialogsEnabled(v bool) {
	purego.SyscallN(i.vtbl.PutAreDefaultScriptDialogsEnabled, uintptr(unsafe.Pointer(i)), boolToUint(v))
}
func (i *settings) PutIsStatusBarEnabled(v bool) {
	purego.SyscallN(i.vtbl.PutIsStatusBarEnabled, uintptr(unsafe.Pointer(i)), boolToUint(v))
}
func (i *settings) PutDevTools(v bool) {
	purego.SyscallN(i.vtbl.PutDevTools, uintptr(unsafe.Pointer(i)), boolToUint(v))
}
func (i *settings) PutAreDefaultContextMenusEnabled(v bool) {
	purego.SyscallN(i.vtbl.PutAreDefaultContextMenusEnabled, uintptr(unsafe.Pointer(i)), boolToUint(v))
}
func (i *settings) PutAreHostObjectsAllowed(v bool) {
	purego.SyscallN(i.vtbl.PutAreHostObjectsAllowed, uintptr(unsafe.Pointer(i)), boolToUint(v))
}
func (i *settings) PutIsZoomControlEnabled(v bool) {
	purego.SyscallN(i.vtbl.PutIsZoomControlEnabled, uintptr(unsafe.Pointer(i)), boolToUint(v))
}
func (i *settings) PutIsBuiltInErrorPageEnabled(v bool) {
	purego.SyscallN(i.vtbl.PutIsBuiltInErrorPageEnabled, uintptr(unsafe.Pointer(i)), boolToUint(v))
}

func (i *settings3i) PutAreBrowserAcceleratorKeysEnabled(v bool) {
	purego.SyscallN(i.vtbl.PutAreBrowserAcceleratorKeysEnabled, uintptr(unsafe.Pointer(i)), boolToUint(v))
}

func (i *settings4i) PutIsPasswordAutosaveEnabled(v bool) {
	purego.SyscallN(i.vtbl.PutIsPasswordAutosaveEnabled, uintptr(unsafe.Pointer(i)), boolToUint(v))
}
func (i *settings4i) PutIsGeneralAutofillEnabled(v bool) {
	purego.SyscallN(i.vtbl.PutIsGeneralAutofillEnabled, uintptr(unsafe.Pointer(i)), boolToUint(v))
}

func (i *settings5i) PutIsPinchZoomEnabled(v bool) {
	purego.SyscallN(i.vtbl.PutIsPinchZoomEnabled, uintptr(unsafe.Pointer(i)), boolToUint(v))
}

func (i *settings6i) PutIsSwipeNavigationEnabled(v bool) {
	purego.SyscallN(i.vtbl.PutIsSwipeNavigationEnabled, uintptr(unsafe.Pointer(i)), boolToUint(v))
}

func (i *settings7i) PutHiddenPdfToolbarItems(mask uint32) {
	purego.SyscallN(i.vtbl.PutHiddenPdfToolbarItems, uintptr(unsafe.Pointer(i)), uintptr(mask))
}

func (i *settings8i) PutIsReputationCheckingRequired(v bool) {
	purego.SyscallN(i.vtbl.PutIsReputationCheckingRequired, uintptr(unsafe.Pointer(i)), boolToUint(v))
}

func (i *settings9i) PutIsNonClientRegionSupportEnabled(v bool) {
	purego.SyscallN(i.vtbl.PutIsNonClientRegionSupportEnabled, uintptr(unsafe.Pointer(i)), boolToUint(v))
}
func (i *messageArgs) TryGetWebMessageAsString(out *uintptr) uintptr {
	r, _, _ := purego.SyscallN(i.vtbl.TryGetWebMessageAsStr, uintptr(unsafe.Pointer(i)), uintptr(unsafe.Pointer(out)))
	return r
}

func boolToUint(b bool) uintptr {
	if b {
		return 1
	}
	return 0
}

const (
	kindEnv = iota
	kindController
	kindMessage
	kindScript
	kindWebResourceRequested
	kindNavigationCompleted
)

type comHandlerVtbl struct {
	QueryInterface uintptr
	AddRef         uintptr
	Release        uintptr
	Invoke         uintptr
}

type comHandler struct {
	vtbl     *comHandlerVtbl
	iid      *guid
	engineID uintptr
	kind     int
	refCount int32
}

var (
	sharedHandlerVtbl *comHandlerVtbl
	handlerMu         sync.Mutex
	handlerKeepAlive  []*comHandler
)

func newCOMHandler(engineID uintptr, kind int, iid *guid) *comHandler {
	h := &comHandler{vtbl: sharedHandlerVtbl, iid: iid, engineID: engineID, kind: kind, refCount: 1}
	handlerMu.Lock()
	handlerKeepAlive = append(handlerKeepAlive, h)
	handlerMu.Unlock()
	return h
}

func handlerPtr(h *comHandler) uintptr     { return uintptr(unsafe.Pointer(h)) }
func handlerFrom(this uintptr) *comHandler { return (*comHandler)(asPointer(this)) }

func handlerQueryInterface(this, riid, ppv uintptr) uintptr {
	if ppv == 0 {
		return 0x80004003
	}
	h := handlerFrom(this)
	want := (*guid)(asPointer(riid))
	if guidEqual(want, h.iid) || guidEqual(want, &iidIUnknown) {
		*(*uintptr)(asPointer(ppv)) = this
		atomic.AddInt32(&h.refCount, 1)
		return 0
	}
	*(*uintptr)(asPointer(ppv)) = 0
	return 0x80004002
}

func handlerAddRef(this uintptr) uintptr {
	h := handlerFrom(this)
	return uintptr(atomic.AddInt32(&h.refCount, 1))
}

func handlerRelease(this uintptr) uintptr {
	h := handlerFrom(this)
	n := atomic.AddInt32(&h.refCount, -1)
	if n < 1 {
		n = 1
	}
	return uintptr(n)
}

func handlerInvoke(this, a, b uintptr) uintptr {
	h := handlerFrom(this)
	dbg("invoke kind=%d a=0x%x b=0x%x", h.kind, a, b)
	w := lookupEngine(h.engineID)
	if w == nil {
		return 0
	}
	switch h.kind {
	case kindEnv:
		if int32(a) >= 0 && b != 0 {
			asEnvironment(b).AddRef()
			w.environment = b
			if int32(asEnvironment(b).CreateController(w.window, handlerPtr(w.ctrlH))) < 0 {
				w.ready = true
			}
		} else {
			w.ready = true
		}
	case kindController:
		if int32(a) >= 0 && b != 0 {
			ctrl := asController(b)
			var wv uintptr
			ctrl.GetCoreWebView2(&wv)
			ctrl.AddRef()
			w.controller = b
			w.webview2 = wv
			if wv != 0 {
				cw := asWebView2(wv)
				cw.AddRef()
				var token uint64
				cw.AddWebMessageReceived(handlerPtr(w.msgH), &token)
				w.navH = newCOMHandler(w.id, kindNavigationCompleted, &iidNavigationCompleted)
				var navTok uint64
				cw.AddNavigationCompleted(handlerPtr(w.navH), &navTok)
				if w.serve != nil {
					w.wrrH = newCOMHandler(w.id, kindWebResourceRequested, &iidWebResourceRequested)
					var wrrTok uint64
					cw.AddWebResourceRequested(handlerPtr(w.wrrH), &wrrTok)
					cw.AddWebResourceRequestedFilter(utf16(schemeVirtualHost+"/*"), 0)
				}
			}
		}
		w.ready = true
	case kindMessage:
		if b != 0 {
			var pwstr uintptr
			if int32(asMessageArgs(b).TryGetWebMessageAsString(&pwstr)) >= 0 && pwstr != 0 {
				msg := wideToString(pwstr)
				freeTaskMem(pwstr)
				w.onMessage(msg)
			}
		}
	case kindNavigationCompleted:
		if b != 0 && asNavigationCompletedArgs(b).IsSuccess() {
			w.markReady()
		}
	case kindScript:
		if int32(a) >= 0 && b != 0 {
			w.lastScript = wideToString(b)
		}
		w.scriptDone = true
	case kindWebResourceRequested:
		if b != 0 {
			w.serveWindowsScheme(b)
		}
	}
	return 0
}

var (
	coInit      func(reserved uintptr, coinit uint32) int32
	freeTaskMem func(p uintptr)

	regOpenKey    func(key uintptr, subkey *uint16, opts, desired uint32, out *uintptr) int32
	regQueryValue func(key uintptr, name *uint16, reserved uintptr, typ *uint32, data *byte, dataLen *uint32) int32
	regClose      func(key uintptr) int32

	clientRectOf func(hwnd uintptr, r *rect) int32

	comInitOnce sync.Once
	comInitErr  error
)

type rect struct{ Left, Top, Right, Bottom int32 }

func ensureCOMInit() error {
	comInitOnce.Do(func() {
		err := initWin32()
		if err != nil {
			comInitErr = err
			return
		}
		ole32, err := syscall.LoadLibrary("ole32.dll")
		if err != nil {
			comInitErr = fmt.Errorf("load ole32.dll: %w", err)
			return
		}
		advapi32, err := syscall.LoadLibrary("advapi32.dll")
		if err != nil {
			comInitErr = fmt.Errorf("load advapi32.dll: %w", err)
			return
		}
		user32, err := syscall.LoadLibrary("user32.dll")
		if err != nil {
			comInitErr = fmt.Errorf("load user32.dll: %w", err)
			return
		}
		reg := func(fn any, dll syscall.Handle, name string) {
			if comInitErr != nil {
				return
			}
			addr, e := syscall.GetProcAddress(dll, name)
			if e != nil {
				comInitErr = fmt.Errorf("resolve %s: %w", name, e)
				return
			}
			purego.RegisterFunc(fn, addr)
		}
		reg(&coInit, ole32, "CoInitializeEx")
		reg(&freeTaskMem, ole32, "CoTaskMemFree")
		reg(&regOpenKey, advapi32, "RegOpenKeyExW")
		reg(&regQueryValue, advapi32, "RegQueryValueExW")
		reg(&regClose, advapi32, "RegCloseKey")
		reg(&clientRectOf, user32, "GetClientRect")
		if comInitErr != nil {
			return
		}
		sharedHandlerVtbl = &comHandlerVtbl{
			QueryInterface: purego.NewCallback(handlerQueryInterface),
			AddRef:         purego.NewCallback(handlerAddRef),
			Release:        purego.NewCallback(handlerRelease),
			Invoke:         purego.NewCallback(handlerInvoke),
		}
	})
	return comInitErr
}

const (
	hkeyLocalMachine = 0x80000002
	hkeyCurrentUser  = 0x80000001
	keyRead          = 0x20019
	keyWow6432Key    = 0x0200

	edgeClientStateKey = `SOFTWARE\Microsoft\EdgeUpdate\ClientState\{F3017226-FE2A-4295-8BDF-00C3A9A7E4C5}`
	minAPIVersion      = 1150
)

func findEmbeddedBrowser() (string, error) {
	for _, root := range []uintptr{hkeyLocalMachine, hkeyCurrentUser} {
		val, err := readRegistryString(root, edgeClientStateKey, "EBWebView")
		if err != nil || val == "" {
			continue
		}
		version := filepath.Base(val)
		if !buildAtLeast(version, minAPIVersion) {
			continue
		}
		dll := filepath.Join(val, "EBWebView", browserArch(), "EmbeddedBrowserWebView.dll")
		_, err = os.Stat(dll)
		if err == nil {
			return dll, nil
		}
	}
	return "", errors.New("webview2: Edge WebView2 Runtime not found (install it)")
}

func browserArch() string {
	switch runtime.GOARCH {
	case "amd64":
		return "x64"
	case "386":
		return "x86"
	case "arm64":
		return "arm64"
	}
	return "x64"
}

func buildAtLeast(v string, min int) bool {
	parts := splitVersion(v)
	if len(parts) < 3 {
		return false
	}
	return parseVersionPart(parts[2]) >= min
}

func readRegistryString(root uintptr, subkey, name string) (string, error) {
	var key uintptr
	if regOpenKey(root, utf16(subkey), 0, keyRead|keyWow6432Key, &key) != 0 {
		return "", errors.New("regOpenKeyExW failed")
	}
	defer regClose(key)
	namePtr := utf16(name)
	var size uint32
	if regQueryValue(key, namePtr, 0, nil, nil, &size) != 0 || size == 0 {
		return "", errors.New("regQueryValueExW size query failed")
	}
	buf := make([]byte, size)
	if regQueryValue(key, namePtr, 0, nil, &buf[0], &size) != 0 {
		return "", errors.New("regQueryValueExW read failed")
	}
	u16 := unsafe.Slice((*uint16)(unsafe.Pointer(&buf[0])), size/2)
	for len(u16) > 0 && u16[len(u16)-1] == 0 {
		u16 = u16[:len(u16)-1]
	}
	return string(utf16Decode(u16)), nil
}

func createWebViewEnvironment(userDataDir string, envHandler *comHandler) error {
	if os.Getenv("WEBVIEW2_DEFAULT_BACKGROUND_COLOR") == "" {
		if err := os.Setenv("WEBVIEW2_DEFAULT_BACKGROUND_COLOR", "00000000"); err != nil {
			return err
		}
	}
	dll, err := findEmbeddedBrowser()
	if err != nil {
		dbg("findEmbeddedBrowserDLL: %v", err)
		return err
	}
	dbg("runtime dll: %s", dll)
	mod, err := syscall.LoadLibrary(dll)
	if err != nil {
		return fmt.Errorf("load %s: %w", dll, err)
	}
	addr, err := syscall.GetProcAddress(mod, "CreateWebViewEnvironmentWithOptionsInternal")
	if err != nil {
		return fmt.Errorf("resolve CreateWebViewEnvironmentWithOptionsInternal (internal WebView2 loader export; installed Edge runtime may be incompatible): %w", err)
	}
	r, _, _ := purego.SyscallN(addr,
		1,
		0,
		uintptr(unsafe.Pointer(utf16(userDataDir))),
		0,
		handlerPtr(envHandler),
	)
	dbg("CreateWebViewEnvironmentWithOptionsInternal -> HRESULT 0x%08X", uint32(r))
	if int32(r) < 0 {
		return fmt.Errorf("CreateWebViewEnvironmentWithOptionsInternal: HRESULT 0x%08X", uint32(r))
	}
	return nil
}

func userDataFolder() string {
	base := os.Getenv("APPDATA")
	if base == "" {
		base = os.TempDir()
	}
	exe, _ := os.Executable()
	return filepath.Join(base, filepath.Base(exe))
}

const coinitApartmentThreaded = 0x2

func (w *webview) embed(v *View) error {
	err := ensureCOMInit()
	if err != nil {
		return err
	}
	coInit(0, coinitApartmentThreaded)

	w.envH = newCOMHandler(w.id, kindEnv, &iidEnvironmentComplete)
	w.ctrlH = newCOMHandler(w.id, kindController, &iidControllerComplete)
	w.msgH = newCOMHandler(w.id, kindMessage, &iidMessageReceived)
	w.scriptH = newCOMHandler(w.id, kindScript, &iidScriptAdded)

	dbg("embed: requesting environment (userDataFolder=%s)", userDataFolder())
	err = createWebViewEnvironment(userDataFolder(), w.envH)
	if err != nil {
		return err
	}

	dbg("embed: pumping until ready")
	var m msgStruct
	for !w.ready {
		r := getMessageW(&m, 0, 0, 0)
		if r <= 0 {
			break
		}
		if m.message == wmQuit {
			return errors.New("webview2: canceled before init")
		}
		translateMessage(&m)
		dispatchMessageW(&m)
	}
	dbg("embed: ready=%v controller=0x%x webview2=0x%x", w.ready, w.controller, w.webview2)
	if w.controller == 0 || w.webview2 == 0 {
		return errors.New("webview2: environment/controller creation failed")
	}

	devTools := v.Debug

	var base uintptr
	if int32(asWebView2(w.webview2).GetSettings(&base)) < 0 || base == 0 {
		dbg("embed: could not obtain ICoreWebView2Settings")
	} else {
		b := asSettings(base)
		b.PutIsScriptEnabled(true)
		b.PutIsWebMessageEnabled(true)
		b.PutAreDefaultScriptDialogsEnabled(true)
		b.PutIsStatusBarEnabled(false)
		b.PutDevTools(devTools)
		b.PutAreDefaultContextMenusEnabled(true)
		b.PutAreHostObjectsAllowed(true)
		b.PutIsZoomControlEnabled(true)
		b.PutIsBuiltInErrorPageEnabled(true)

		applySettingsExtension := func(iid *guid, apply func(p uintptr)) {
			if p := b.settingsQI(iid); p != 0 {
				defer asSettingsSafeRelease(p)
				apply(p)
			}
		}
		applySettingsExtension(&iidSettings3, func(p uintptr) {
			asSettings3(p).PutAreBrowserAcceleratorKeysEnabled(true)
		})
		applySettingsExtension(&iidSettings4, func(p uintptr) {
			asSettings4(p).PutIsPasswordAutosaveEnabled(false)
			asSettings4(p).PutIsGeneralAutofillEnabled(true)
		})
		applySettingsExtension(&iidSettings5, func(p uintptr) { asSettings5(p).PutIsPinchZoomEnabled(true) })
		applySettingsExtension(&iidSettings6, func(p uintptr) { asSettings6(p).PutIsSwipeNavigationEnabled(true) })
		applySettingsExtension(&iidSettings7, func(p uintptr) { asSettings7(p).PutHiddenPdfToolbarItems(0) })
		applySettingsExtension(&iidSettings8, func(p uintptr) { asSettings8(p).PutIsReputationCheckingRequired(true) })
		applySettingsExtension(&iidSettings9, func(p uintptr) { asSettings9(p).PutIsNonClientRegionSupportEnabled(false) })

		b.Release()
	}

	w.applyWebView2BackgroundColor()

	w.pushUserScript(buildInitScript(bridgePostFn))

	w.resizeWebView()
	asController(w.controller).PutIsVisible(true)
	if w.ownsWindow {
		applyWindowAppIcon(w.window)
	}
	showWindow(w.window, swShow)
	updateWindow(w.window)
	w.applyWebView2BackgroundColor()
	asController(w.controller).MoveFocus(moveFocusReasonProgrammatic)

	return nil
}

func (w *webview) resizeWebView() {
	if w.controller == 0 || w.window == 0 {
		return
	}
	var r rect
	if clientRectOf(w.window, &r) != 0 {
		asController(w.controller).putBounds(r)
	}
}

func (w *webview) applyWebView2BackgroundColor() {
	if w.controller == 0 {
		return
	}
	var ctrl2 uintptr
	hr := asController(w.controller).QueryInterface(&iidController2, &ctrl2)
	if ctrl2 == 0 {
		dbg("applyDefaultBackgroundColor: no Controller2 (hr=0x%x ctrl2=0x%x); background color unavailable", uint32(hr), ctrl2)
		return
	}
	defer asController2(ctrl2).Release()
	// COREWEBVIEW2_COLOR is a {A, R, G, B} byte struct passed by value; on the
	// little-endian calling convention that is A in the low byte. A fully
	// transparent background (alpha 0) with the WS_EX_NOREDIRECTIONBITMAP
	// window style lets the desktop show through the page's transparent pixels;
	// a fully opaque one (alpha 0xFF) paints the usual white behind the page. A
	// framed window is opaque white; there is no per-window
	// opaque-background option.
	var color uint32
	if w.frameless {
		color = 0 // A=0 -> fully transparent
	} else {
		color = 0x000000FF // A=0xFF -> opaque white
	}
	hr = asController2(ctrl2).PutDefaultBackgroundColor(color)
	dbg("applyDefaultBackgroundColor: color=0x%08x hr=0x%x", color, uint32(hr))
}

func (w *webview) Focus() {
	if w.controller == 0 {
		return
	}
	asController(w.controller).MoveFocus(moveFocusReasonProgrammatic)
}

func (w *webview) Raise() {
	if w.window == 0 {
		return
	}
	showWindow(w.window, swRestore)
	setForegroundWin(w.window)
}

func (w *webview) Show() {
	if w.window == 0 {
		return
	}
	w.Dispatch(func() {
		showWindow(w.window, swRestore)
		showWindow(w.window, swShow)
		setForegroundWin(w.window)
	})
}

func (w *webview) Hide() {
	if w.window == 0 {
		return
	}
	w.Dispatch(func() { showWindow(w.window, swHide) })
}

func (w *webview) Maximize() {
	if w.window == 0 {
		return
	}
	w.Dispatch(func() { showWindow(w.window, swMaximize) })
}

func (w *webview) Minimize() {
	if w.window == 0 {
		return
	}
	w.Dispatch(func() { showWindow(w.window, swMinimize) })
}

func (w *webview) Unminimize() {
	if w.window == 0 {
		return
	}
	w.Dispatch(func() { showWindow(w.window, swRestore) })
}

func (w *webview) Unmaximize() {
	if w.window == 0 {
		return
	}
	w.Dispatch(func() { showWindow(w.window, swRestore) })
}

func (w *webview) toggleMaximize() {
	if w.window == 0 {
		return
	}
	w.Dispatch(func() {
		if isZoomed(w.window) != 0 {
			showWindow(w.window, swRestore)
		} else {
			showWindow(w.window, swMaximize)
		}
	})
}

func (w *webview) Maximized() bool {
	if w.window == 0 {
		return false
	}
	return isZoomed(w.window) != 0
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
	url = w.resolveURL(url)
	url = w.rewriteSchemeURL(url)
	if w.webview2 == 0 {
		return
	}
	if url == "" {
		url = "about:blank"
	}
	asWebView2(w.webview2).Navigate(utf16(url))
}

func (w *webview) loadHTML(html string) {
	if w.webview2 != 0 {
		asWebView2(w.webview2).NavigateToString(utf16(html))
	}
}

func (w *webview) Eval(js string) {
	if w.webview2 != 0 {
		asWebView2(w.webview2).ExecuteScript(utf16(js), 0)
	}
}

func (w *webview) Init(js string) { w.pushUserScript(js) }

func (w *webview) addDocumentScript(src string) {
	if w.webview2 == 0 {
		return
	}
	w.scriptDone = false
	w.lastScript = ""
	asWebView2(w.webview2).AddScript(utf16(src), handlerPtr(w.scriptH))
	var m msgStruct
	for !w.scriptDone {
		r := getMessageW(&m, 0, 0, 0)
		if r <= 0 {
			break
		}
		if m.message == wmQuit {
			postQuitMessage(0)
			break
		}
		translateMessage(&m)
		dispatchMessageW(&m)
	}
	if w.lastScript != "" {
		w.installedScriptIDs = append(w.installedScriptIDs, w.lastScript)
	}
}

func (w *webview) pushUserScript(src string) {
	w.mu.Lock()
	w.userScriptSrcs = append(w.userScriptSrcs, src)
	w.mu.Unlock()
	w.addDocumentScript(src)
}

func (w *webview) clearDocumentScripts() {
	if w.webview2 != 0 {
		cw := asWebView2(w.webview2)
		for _, id := range w.installedScriptIDs {
			cw.RemoveScript(utf16(id))
		}
	}
	w.installedScriptIDs = nil
}

func (w *webview) rebuildUserScripts() {
	w.clearDocumentScripts()
	w.mu.Lock()
	srcs := append([]string(nil), w.userScriptSrcs...)
	entries := w.bindingEntries()
	w.mu.Unlock()
	for _, src := range srcs {
		w.addDocumentScript(src)
	}
	w.addDocumentScript(buildBindScript(entries))
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
	w.mu.Unlock()
	w.rebuildUserScripts()
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
	w.mu.Unlock()
	w.rebuildUserScripts()
	w.Eval(buildLiveUnbindScript(name))
	return nil
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
	if m.Method == methodAppRegions {
		w.setRegions(parseAppRegionSet(m.Params))
		return
	}
	if m.Method == methodWindowDrag {
		w.beginWindowMove(parseDragRequest(m.Params))
		return
	}
	if m.Method == methodWindowResize {
		w.beginWindowResize(parseDragRequest(m.Params))
		return
	}
	if m.Method == methodWindowMaximize {
		w.toggleMaximize()
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

func (w *webview) resolve(id string, status int, resultJSON string) {
	js := fmt.Sprintf("window.__webview__.onReply(%s, %d, %s)", jsonQuote(id), status, jsonQuote(resultJSON))
	w.Dispatch(func() { w.Eval(js) })
}

func wideToString(p uintptr) string {
	if p == 0 {
		return ""
	}
	base := asPointer(p)
	var n int
	for *(*uint16)(unsafe.Add(base, uintptr(n)*2)) != 0 {
		n++
	}
	return string(utf16Decode(unsafe.Slice((*uint16)(base), n)))
}

func utf16Decode(u []uint16) []rune {
	out := make([]rune, 0, len(u))
	for i := 0; i < len(u); i++ {
		c := u[i]
		switch {
		case c >= 0xD800 && c < 0xDC00 && i+1 < len(u):
			lo := u[i+1]
			out = append(out, (rune(c-0xD800)<<10|rune(lo-0xDC00))+0x10000)
			i++
		default:
			out = append(out, rune(c))
		}
	}
	return out
}

func splitVersion(s string) []string {
	var parts []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '.' {
			parts = append(parts, s[start:i])
			start = i + 1
		}
	}
	parts = append(parts, s[start:])
	return parts
}

func parseVersionPart(s string) int {
	n := 0
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return n
		}
		n = n*10 + int(s[i]-'0')
	}
	return n
}

const schemeVirtualHost = "https://" + schemeName + ".localhost"

type webResourceRequestedArgsVtbl struct {
	unknownVtbl
	GetRequest  uintptr
	GetResponse uintptr
	PutResponse uintptr
}
type webResourceRequestedArgs struct{ vtbl *webResourceRequestedArgsVtbl }

func asWebResourceRequestedArgs(p uintptr) *webResourceRequestedArgs {
	return (*webResourceRequestedArgs)(asPointer(p))
}

func (i *webResourceRequestedArgs) GetRequest(out *uintptr) uintptr {
	r, _, _ := purego.SyscallN(i.vtbl.GetRequest, uintptr(unsafe.Pointer(i)), uintptr(unsafe.Pointer(out)))
	return r
}
func (i *webResourceRequestedArgs) PutResponse(resp uintptr) uintptr {
	r, _, _ := purego.SyscallN(i.vtbl.PutResponse, uintptr(unsafe.Pointer(i)), resp)
	return r
}

type webResourceRequestVtbl struct {
	unknownVtbl
	GetUri uintptr
}
type webResourceRequest struct{ vtbl *webResourceRequestVtbl }

func asWebResourceRequest(p uintptr) *webResourceRequest { return (*webResourceRequest)(asPointer(p)) }

type unknown struct{ vtbl *unknownVtbl }

func releaseUnknown(p uintptr) {
	if p == 0 {
		return
	}
	u := (*unknown)(asPointer(p))
	purego.SyscallN(u.vtbl.Release, p)
}

func (i *webResourceRequest) GetUri(out *uintptr) uintptr {
	r, _, _ := purego.SyscallN(i.vtbl.GetUri, uintptr(unsafe.Pointer(i)), uintptr(unsafe.Pointer(out)))
	return r
}

func (i *environment) CreateWebResourceResponse(stream uintptr, status int, reason, headers *uint16, out *uintptr) uintptr {
	r, _, _ := purego.SyscallN(i.vtbl.CreateWebResourceResponse,
		uintptr(unsafe.Pointer(i)), stream, uintptr(status),
		uintptr(unsafe.Pointer(reason)), uintptr(unsafe.Pointer(headers)), uintptr(unsafe.Pointer(out)))
	return r
}

var (
	memStreamOnce sync.Once
	memStreamProc uintptr
)

func createMemStream(data []byte) uintptr {
	memStreamOnce.Do(func() {
		mod, err := syscall.LoadLibrary("shlwapi.dll")
		if err != nil {
			return
		}
		addr, err := syscall.GetProcAddress(mod, "SHCreateMemStream")
		if err != nil {
			return
		}
		memStreamProc = addr
	})
	if memStreamProc == 0 {
		return 0
	}
	var p *byte
	if len(data) > 0 {
		p = &data[0]
	}
	r, _, _ := purego.SyscallN(memStreamProc, uintptr(unsafe.Pointer(p)), uintptr(uint32(len(data))))
	return r
}

func (w *webview) serveWindowsScheme(args uintptr) {
	a := asWebResourceRequestedArgs(args)
	var reqPtr uintptr
	if int32(a.GetRequest(&reqPtr)) < 0 || reqPtr == 0 {
		return
	}
	defer releaseUnknown(reqPtr)
	var uriPtr uintptr
	if int32(asWebResourceRequest(reqPtr).GetUri(&uriPtr)) < 0 || uriPtr == 0 {
		return
	}
	uri := wideToString(uriPtr)
	freeTaskMem(uriPtr)

	if w.serve == nil || !strings.HasPrefix(uri, schemeVirtualHost+"/") {
		return
	}
	resp := invokeContentFunc(w.serve, &contentRequest{URL: w.canonicalAppURL(uri)})
	if resp == nil || w.environment == 0 {
		return
	}

	stream := createMemStream(resp.Body)
	if stream == 0 {
		return
	}
	defer releaseUnknown(stream)
	headers := "Content-Type: " + contentMIME(resp) +
		"\r\n" + headerCOOP + ": " + valSameOrigin +
		"\r\n" + headerCOEP + ": " + valRequireCorp +
		"\r\n" + headerCORP + ": " + valSameOrigin
	var respObj uintptr
	if int32(asEnvironment(w.environment).CreateWebResourceResponse(stream, 200, utf16("OK"), utf16(headers), &respObj)) < 0 || respObj == 0 {
		return
	}
	defer releaseUnknown(respObj)
	a.PutResponse(respObj)
}

func (w *webview) rewriteSchemeURL(raw string) string {
	if w.serve == nil {
		return raw
	}
	u, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	if u.Scheme != schemeName {
		return raw
	}
	if u.Host != "" {
		w.mu.Lock()
		w.schemeAuthority = u.Host
		w.mu.Unlock()
	}
	out := schemeVirtualHost + u.Path
	if u.RawQuery != "" {
		out += "?" + u.RawQuery
	}
	if u.Fragment != "" {
		out += "#" + u.EscapedFragment()
	}
	return out
}

func (w *webview) canonicalAppURL(vhostURL string) string {
	u, err := url.Parse(vhostURL)
	if err != nil {
		return vhostURL
	}
	w.mu.Lock()
	authority := w.schemeAuthority
	w.mu.Unlock()
	if authority == "" {
		authority = schemeName
	}
	out := schemeName + "://" + authority + u.Path
	if u.RawQuery != "" {
		out += "?" + u.RawQuery
	}
	return out
}

const (
	cwUseDefault = ^int32(0x7fffffff)
	swHide       = 0
	swMaximize   = 3
	swShow       = 5
	swMinimize   = 6
	swRestore    = 9

	wsOverlappedWindow      = 0x00CF0000
	wsThickFrame            = 0x00040000
	wsMaximizeBox           = 0x00010000
	wsMinimizeBox           = 0x00020000
	wsPopup                 = 0x80000000
	wsExNoRedirectionBitmap = 0x00200000

	gwlpUserData = -21
	gwlStyle     = -16
	gwlWndProc   = -4

	wmNCCreate      = 0x0081
	wmDestroy       = 0x0002
	wmSize          = 0x0005
	wmSetFocus      = 0x0007
	wmClose         = 0x0010
	wmGetMinMaxInfo = 0x0024
	wmNCCalcSize    = 0x0083
	wmNCHitTest     = 0x0084
	wmNCLButtonDown = 0x00A1
	wmApp           = 0x8000
	wmQuit          = 0x0012
	wmSetIcon       = 0x0080

	iconSmall = 0
	iconBig   = 1

	swpNoZOrder   = 0x0004
	swpNoActivate = 0x0010
	swpNoMove     = 0x0002

	htCaption     = 2
	htLeft        = 10
	htRight       = 11
	htTop         = 12
	htTopLeft     = 13
	htTopRight    = 14
	htBottom      = 15
	htBottomLeft  = 16
	htBottomRight = 17
)

const (
	defaultWidth  = 640
	defaultHeight = 480
)

var (
	getModuleHandleW         func(name uintptr) uintptr
	registerClassExW         func(wc *wndClassExW) uint16
	createWindowExW          func(exStyle uint32, objcClass, name *uint16, style uint32, x, y, w, h int32, parent, menu, inst, param uintptr) uintptr
	defWindowProcW           func(hwnd uintptr, msg uint32, wp, lp uintptr) uintptr
	callWindowProcW          func(prev uintptr, hwnd uintptr, msg uint32, wp, lp uintptr) uintptr
	getMessageW              func(m *msgStruct, hwnd uintptr, min, max uint32) int32
	translateMessage         func(m *msgStruct) int32
	dispatchMessageW         func(m *msgStruct) uintptr
	postQuitMessage          func(code int32)
	postThreadMessageW       func(thread uint32, msg uint32, wp, lp uintptr) int32
	getCurrentThreadID       func() uint32
	postMessageW             func(hwnd uintptr, msg uint32, wp, lp uintptr) int32
	sendMessageW             func(hwnd uintptr, msg uint32, wp, lp uintptr) uintptr
	createIconFromResourceEx func(resource *byte, bytes uint32, isIcon int32, ver uint32, cx, cy int32, flags uint32) uintptr
	showWindow               func(hwnd uintptr, cmd int32) int32
	updateWindow             func(hwnd uintptr) int32
	destroyWindow            func(hwnd uintptr) int32
	setWindowLongPtrW        func(hwnd uintptr, index int32, val uintptr) uintptr
	getWindowLongPtrW        func(hwnd uintptr, index int32) uintptr
	setWindowPos             func(hwnd, after uintptr, x, y, w, h int32, flags uint32) int32
	screenToClient           func(hwnd uintptr, pt *point) int32
	setForegroundWin         func(hwnd uintptr) int32
	releaseCapture           func() int32
	getSystemMetrics         func(index int32) int32
	isZoomed                 func(hwnd uintptr) int32
)

type wndClassExW struct {
	cbSize        uint32
	_             uint32
	lpfnWndProc   uintptr
	_             int32
	_             int32
	hInstance     uintptr
	_             uintptr
	_             uintptr
	_             uintptr
	_             *uint16
	lpszClassName *uint16
	_             uintptr
}

type point struct{ X, Y int32 }

type minMaxInfo struct {
	ptReserved     point
	ptMaxSize      point
	ptMaxPosition  point
	ptMinTrackSize point
	ptMaxTrackSize point
}

type msgStruct struct {
	_       uintptr
	message uint32
	_       uint32
	_       uintptr
	_       uintptr
	_       uint32
	_       point
	_       uint32
}

var (
	winInitOnce sync.Once
	winInitErr  error

	wndProcCB  uintptr
	hostProcCB uintptr
)

func initWin32() error {
	winInitOnce.Do(func() {
		user32, err := syscall.LoadLibrary("user32.dll")
		if err != nil {
			winInitErr = fmt.Errorf("webview: load user32.dll: %w", err)
			return
		}
		kernel32, err := syscall.LoadLibrary("kernel32.dll")
		if err != nil {
			winInitErr = fmt.Errorf("webview: load kernel32.dll: %w", err)
			return
		}
		reg := func(fn any, dll syscall.Handle, name string) {
			if winInitErr != nil {
				return
			}
			addr, e := syscall.GetProcAddress(dll, name)
			if e != nil {
				winInitErr = fmt.Errorf("webview: resolve %s: %w", name, e)
				return
			}
			purego.RegisterFunc(fn, addr)
		}
		reg(&getModuleHandleW, kernel32, "GetModuleHandleW")
		reg(&registerClassExW, user32, "RegisterClassExW")
		reg(&createWindowExW, user32, "CreateWindowExW")
		reg(&defWindowProcW, user32, "DefWindowProcW")
		reg(&callWindowProcW, user32, "CallWindowProcW")
		reg(&getMessageW, user32, "GetMessageW")
		reg(&translateMessage, user32, "TranslateMessage")
		reg(&dispatchMessageW, user32, "DispatchMessageW")
		reg(&postQuitMessage, user32, "PostQuitMessage")
		reg(&postMessageW, user32, "PostMessageW")
		reg(&postThreadMessageW, user32, "PostThreadMessageW")
		reg(&getCurrentThreadID, kernel32, "GetCurrentThreadId")
		reg(&sendMessageW, user32, "SendMessageW")
		reg(&createIconFromResourceEx, user32, "CreateIconFromResourceEx")
		reg(&showWindow, user32, "ShowWindow")
		reg(&updateWindow, user32, "UpdateWindow")
		reg(&destroyWindow, user32, "DestroyWindow")
		setLongPtr, getLongPtr := "SetWindowLongPtrW", "GetWindowLongPtrW"
		if unsafe.Sizeof(uintptr(0)) == 4 {
			setLongPtr, getLongPtr = "SetWindowLongW", "GetWindowLongW"
		}
		reg(&setWindowLongPtrW, user32, setLongPtr)
		reg(&getWindowLongPtrW, user32, getLongPtr)
		reg(&setWindowPos, user32, "SetWindowPos")
		reg(&screenToClient, user32, "ScreenToClient")
		reg(&setForegroundWin, user32, "SetForegroundWindow")
		reg(&releaseCapture, user32, "ReleaseCapture")
		reg(&getSystemMetrics, user32, "GetSystemMetrics")
		reg(&isZoomed, user32, "IsZoomed")
		if winInitErr != nil {
			return
		}
		wndProcCB = purego.NewCallback(wndProc)
		hostProcCB = purego.NewCallback(windowHostProc)
	})
	return winInitErr
}

func utf16(s string) *uint16 {
	u := make([]uint16, 0, len(s)+1)
	for _, r := range s {
		if r < 0x10000 {
			u = append(u, uint16(r))
		} else {
			r -= 0x10000
			u = append(u, uint16(0xD800+(r>>10)), uint16(0xDC00+(r&0x3FF)))
		}
	}
	u = append(u, 0)
	return &u[0]
}

func pointFromLParam(lp uintptr) point {
	return point{
		X: int32(int16(lp & 0xffff)),
		Y: int32(int16((lp >> 16) & 0xffff)),
	}
}

func packPoint(x, y int32) uintptr {
	return uintptr(uint32(uint16(x)) | uint32(uint16(y))<<16)
}

var (
	regMu     sync.Mutex
	registry  = map[uintptr]*webview{}
	engineSeq uintptr

	uiThreadOnce sync.Once

	windowCount int32
)

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

type webview struct {
	id         uintptr
	hinst      uintptr
	window     uintptr
	ownsWindow bool

	controller  uintptr
	webview2    uintptr
	environment uintptr
	envH        *comHandler
	ctrlH       *comHandler
	msgH        *comHandler
	scriptH     *comHandler
	wrrH        *comHandler
	navH        *comHandler
	ready       bool
	scriptDone  bool
	lastScript  string

	serve           contentFunc
	schemeAuthority string

	contentBase string
	transient   *localServer

	minWidth, minHeight int32
	maxWidth, maxHeight int32

	// frameless windows drop the OS frame; the page's -app-region
	// boxes then drive the WM_NCHITTEST-based move drag, and the edge bands
	// drive the matching WM_NCLBUTTONDOWN resize.
	frameless bool

	// fixed records that the window was created un-resizable (State ==
	// StateFixed): a frameless fixed window drops WS_THICKFRAME so neither the
	// OS nor the page edge bands can resize it.
	fixed bool

	regions appRegionSet

	hostOrig uintptr

	mu                 sync.Mutex
	bindings           map[string]binding
	userScriptSrcs     []string
	installedScriptIDs []string
	events             *events
	calls              workQueue

	eventsGlobal string

	onReady      func()
	onReadyFired bool

	uiThread uint32

	dispatchMu  sync.Mutex
	dispatchMap map[uintptr]func()
	dispatchSeq uintptr
}

var classNamePtr = utf16("appkit_webview")

func newView(v *View, serve contentFunc) (*webview, error) {
	err := initWin32()
	if err != nil {
		return nil, err
	}
	uiThreadOnce.Do(runtime.LockOSThread)

	w := &webview{
		ownsWindow:  v.window == nil,
		frameless:   !v.Frame,
		fixed:       !v.Frame && v.State == StateFixed,
		bindings:    map[string]binding{},
		dispatchMap: map[uintptr]func(){},
		serve:       serve,
	}
	w.id = registerEngine(w)
	w.hinst = getModuleHandleW(0)

	if w.ownsWindow {
		style := uint32(wsOverlappedWindow)
		if w.frameless {
			// A frameless window is a WS_POPUP (no OS caption/decorations).
			// WS_MINIMIZEBOX is kept even though nothing draws it: the taskbar
			// minimizes the foreground window with WM_SYSCOMMAND SC_MINIMIZE,
			// which DefWindowProc only honours when this style is set - without
			// it, clicking the taskbar button of a visible window does nothing
			// (only the restore half of the taskbar toggle works).
			// WS_THICKFRAME is still added when the window may be resized: it
			// is what makes DefWindowProc honour the WM_NCLBUTTONDOWN(HTCAPTION
			// / HT<edge>) that beginWindowMove/beginWindowResize send to start
			// the modal move/resize loop, and it does not paint a visible frame
			// on a borderless popup. StateFixed drops it so the window cannot
			// be resized.
			style = wsPopup | wsMinimizeBox
			if !w.fixed {
				style |= wsThickFrame
			}
		}
		wc := wndClassExW{
			lpfnWndProc:   wndProcCB,
			hInstance:     w.hinst,
			lpszClassName: classNamePtr,
		}
		wc.cbSize = uint32(unsafe.Sizeof(wc))
		registerClassExW(&wc)

		w.window = createWindowExW(
			wsExNoRedirectionBitmap, classNamePtr, utf16(""), style,
			cwUseDefault, cwUseDefault, defaultWidth, defaultHeight,
			0, 0, w.hinst, w.id,
		)
		if w.window == 0 {
			unregisterEngine(w.id)
			return nil, errNoWindow
		}
		atomic.AddInt32(&windowCount, 1)
	} else {
		w.window = uintptr(v.window)
		w.hostOrig = setWindowLongPtrW(w.window, gwlWndProc, hostProcCB)
		setWindowLongPtrW(w.window, gwlpUserData, w.id)
	}

	w.uiThread = getCurrentThreadID()

	if err := w.embed(v); err != nil {
		w.Destroy()
		return nil, err
	}
	if w.frameless && w.ownsWindow {
		w.pushUserScript(buildRegionScript(v.State != StateFixed, true, "windows"))
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

func wndProc(hwnd uintptr, msg uint32, wp, lp uintptr) uintptr {
	var id uintptr
	if msg == wmNCCreate {
		cs := *(*unsafe.Pointer)(unsafe.Pointer(&lp))
		id = *(*uintptr)(cs)
		setWindowLongPtrW(hwnd, gwlpUserData, id)
	} else {
		id = getWindowLongPtrW(hwnd, gwlpUserData)
	}
	w := lookupEngine(id)
	if w == nil {
		return defWindowProcW(hwnd, msg, wp, lp)
	}
	if res, handled := w.engineMsg(hwnd, msg, wp, lp); handled {
		return res
	}
	return defWindowProcW(hwnd, msg, wp, lp)
}

func windowHostProc(hwnd uintptr, msg uint32, wp, lp uintptr) uintptr {
	id := getWindowLongPtrW(hwnd, gwlpUserData)
	w := lookupEngine(id)
	if w == nil {
		return defWindowProcW(hwnd, msg, wp, lp)
	}
	switch msg {
	case wmApp:
		w.runDispatchLoop(lp)
		return 0
	case wmSize:
		w.resizeWebView()
	}
	if w.hostOrig != 0 {
		return callWindowProcW(w.hostOrig, hwnd, msg, wp, lp)
	}
	return defWindowProcW(hwnd, msg, wp, lp)
}

func (w *webview) engineMsg(hwnd uintptr, msg uint32, wp, lp uintptr) (uintptr, bool) {
	switch msg {
	case wmApp:
		w.runDispatchLoop(lp)
		return 0, true
	case wmSize:
		w.resizeWebView()
		return 0, true
	case wmNCCalcSize:
		// Frameless windows carry WS_THICKFRAME only so DefWindowProc will run
		// the modal resize (SC_SIZE) - the frame itself is not wanted. Return 0
		// so the whole window rect is the client area (no invisible resize
		// border / white inset the frame would otherwise keep), like the
		// reference does. When maximized we leave the system to size the frame
		// so the window still snaps inside the monitor work area.
		if w.frameless && w.ownsWindow && isZoomed(w.window) == 0 {
			return 0, true
		}
		return 0, false
	case wmSetFocus:
		if w.controller != 0 {
			asController(w.controller).MoveFocus(moveFocusReasonProgrammatic)
		}
		return 0, true
	case wmGetMinMaxInfo:
		mmi := (*minMaxInfo)(asPointer(lp))
		if w.maxWidth > 0 && w.maxHeight > 0 {
			mmi.ptMaxSize = point{w.maxWidth, w.maxHeight}
			mmi.ptMaxTrackSize = point{w.maxWidth, w.maxHeight}
		}
		if w.minWidth > 0 && w.minHeight > 0 {
			mmi.ptMinTrackSize = point{w.minWidth, w.minHeight}
		}
		return 0, true
	case wmNCHitTest:
		if w.frameless && !w.regions.empty() {
			if pt, ok := w.clientPointAt(hwnd, lp); ok && w.regions.isDrag(float64(pt.X), float64(pt.Y)) {
				return uintptr(int32(htCaption)), true
			}
		}
		return 0, false
	case wmClose:
		destroyWindow(hwnd)
		if w.ownsWindow && atomic.LoadInt32(&windowCount) <= 0 {
			postQuitMessage(0)
		}
		return 0, true
	case wmDestroy:
		unregisterEngine(w.id)
		if w.ownsWindow {
			atomic.AddInt32(&windowCount, -1)
			onAppWindowClosed()
		}
		w.window = 0
		setWindowLongPtrW(hwnd, gwlpUserData, 0)
		return 0, true
	}
	return 0, false
}

func (w *webview) clientPointAt(hwnd uintptr, lp uintptr) (point, bool) {
	pt := pointFromLParam(lp)
	if screenToClient(hwnd, &pt) == 0 {
		return point{}, false
	}
	return pt, true
}

func (w *webview) runDispatchLoop(lp uintptr) {
	w.dispatchMu.Lock()
	f := w.dispatchMap[lp]
	delete(w.dispatchMap, lp)
	w.dispatchMu.Unlock()
	if f != nil {
		f()
	}
}

func (w *webview) Run() {
	var m msgStruct
	for getMessageW(&m, 0, 0, 0) > 0 {
		translateMessage(&m)
		dispatchMessageW(&m)
	}
}

func (w *webview) Terminate() {
	w.Dispatch(func() { postQuitMessage(0) })
}
func (w *webview) Dispatch(f func()) {
	w.dispatchMu.Lock()
	w.dispatchSeq++
	id := w.dispatchSeq
	w.dispatchMap[id] = f
	w.dispatchMu.Unlock()
	if postMessageW(w.window, wmApp, 0, id) == 0 {
		w.dispatchMu.Lock()
		delete(w.dispatchMap, id)
		w.dispatchMu.Unlock()
	}
}

func (w *webview) Window() unsafe.Pointer {
	p := w.window
	return *(*unsafe.Pointer)(unsafe.Pointer(&p))
}

func (w *webview) applyViewGeometry(v *View) {
	width, height := v.Width, v.Height
	if width == 0 && height == 0 {
		width, height = defaultWidth, defaultHeight
	}
	w.applyWindowSize(width, height, v.State)
}

func (w *webview) applyWindowSize(width, height int, state State) {
	switch state {
	case StateMin:
		w.minWidth, w.minHeight = int32(width), int32(height)
		return
	case StateMax:
		w.maxWidth, w.maxHeight = int32(width), int32(height)
		return
	}
	if !w.frameless {
		// Frame windows toggle the resize frame and maximize box. Frameless
		// windows resize from the page's edge bands instead (see
		// beginWindowResize), so their style is left alone here.
		style := getWindowLongPtrW(w.window, gwlStyle)
		if state == StateFixed {
			style &^= uintptr(wsThickFrame | wsMaximizeBox)
		} else {
			style |= uintptr(wsThickFrame | wsMaximizeBox)
		}
		setWindowLongPtrW(w.window, gwlStyle, style)
	}
	setWindowPos(w.window, 0, 0, 0, int32(width), int32(height),
		swpNoZOrder|swpNoActivate|swpNoMove)
	if w.ownsWindow {
		showWindow(w.window, swShow)
		updateWindow(w.window)
	}
}

func (w *webview) Destroy() {
	w.dropLoopback()
	if w.uiThread != 0 && getCurrentThreadID() != w.uiThread && w.window != 0 {
		w.Dispatch(func() { w.destroyWebViewOnUI() })
		return
	}
	w.destroyWebViewOnUI()
}

func (w *webview) destroyWebViewOnUI() {
	if w.controller != 0 {
		asController(w.controller).Close()
		if w.webview2 != 0 {
			asWebView2(w.webview2).Release()
			w.webview2 = 0
		}
		asController(w.controller).Release()
		w.controller = 0
	}
	if w.window != 0 && w.ownsWindow {
		destroyWindow(w.window)
		w.window = 0
	}
	if w.environment != 0 {
		asEnvironment(w.environment).Release()
		w.environment = 0
	}
	w.dispatchMu.Lock()
	w.dispatchMap = map[uintptr]func(){}
	w.dispatchMu.Unlock()
	unregisterEngine(w.id)
}

func (w *webview) setRegions(rs appRegionSet) { w.regions = rs }

func (w *webview) beginWindowMove(p dragRequest) {
	if w.window == 0 || !w.frameless || !w.ownsWindow {
		return
	}
	releaseCapture()
	sendMessageW(w.window, wmNCLButtonDown, uintptr(int32(htCaption)), packPoint(p.ScreenX, p.ScreenY))
}

func hitTestCode(direction string) int {
	switch direction {
	case "nw":
		return htTopLeft
	case "n":
		return htTop
	case "ne":
		return htTopRight
	case "w":
		return htLeft
	case "e":
		return htRight
	case "sw":
		return htBottomLeft
	case "s":
		return htBottom
	case "se":
		return htBottomRight
	}
	return -1
}

func (w *webview) beginWindowResize(p dragRequest) {
	if w.window == 0 || !w.frameless || !w.ownsWindow {
		return
	}
	if getWindowLongPtrW(w.window, gwlStyle)&uintptr(wsThickFrame) == 0 {
		return
	}
	code := hitTestCode(p.Direction)
	if code < 0 {
		return
	}
	releaseCapture()
	sendMessageW(w.window, wmNCLButtonDown, uintptr(int32(code)), packPoint(p.ScreenX, p.ScreenY))
}

func platformBackend() string { return "WebView2" }

var (
	waitThreadOnce sync.Once
	uiThreadIDv    uint32
)

func uiThread() uint32 {
	waitThreadOnce.Do(func() {
		uiThreadIDv = getCurrentThreadID()
	})
	return uiThreadIDv
}

func pumpUI() {
	uiThread()
	var m msgStruct
	for getMessageW(&m, 0, 0, 0) > 0 {
		translateMessage(&m)
		dispatchMessageW(&m)
	}
}

func wakeUI() {
	postThreadMessageW(uiThread(), wmQuit, 0, 0)
}

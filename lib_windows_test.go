package appkit

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"runtime"
	"sync/atomic"
	"testing"
	"time"
	"unsafe"
)

var (
	resWinBridge      atomic.Value
	resWinErrorUnbind atomic.Value
	resWinRichTypes   atomic.Value
	resWinEmbed       atomic.Value
	resWinClose       atomic.Value
)

func guiAvailable() bool {
	if ensureCOMInit() != nil {
		return false
	}
	_, err := findEmbeddedBrowser()
	return err == nil
}

func requireGUI(t *testing.T, got string) {
	t.Helper()
	if got == "" {
		t.Skip("Edge WebView2 Runtime not available")
	}
}

func TestMain(m *testing.M) {
	flag.Parse()
	runtime.LockOSThread()
	if !testing.Short() && guiAvailable() {
		resWinBridge.Store(winBridgeScenario())
		resWinErrorUnbind.Store(winErrorUnbindScenario())
		resWinRichTypes.Store(winRichTypesScenario())
		resWinEmbed.Store(winEmbedScenario())
		resWinClose.Store(winCloseViaUIScenario())
	}
	os.Exit(m.Run())
}

func winCloseViaUIScenario() string {
	w := &View{}
	if err := testApp().Show(w); err != nil {
		return "new error: " + err.Error()
	}
	defer w.Close()
	hwnd := w.w.window

	var watchdogFired atomic.Bool
	time.AfterFunc(2*time.Second, func() { postMessageW(hwnd, wmClose, 0, 0) })
	time.AfterFunc(40*time.Second, func() { watchdogFired.Store(true); w.Close() })

	w.w.loadHTML(`<!DOCTYPE html><html><body>close test</body></html>`)
	w.w.Run()

	if watchdogFired.Load() {
		return "hung (WM_CLOSE did not end Run)"
	}
	return "closed"
}

func winEmbedScenario() string {
	err := initWin32()
	if err != nil {
		return "init error: " + err.Error()
	}
	host := createWindowExW(0, utf16("STATIC"), utf16("host"), 0x00CF0000,
		cwUseDefault, cwUseDefault, 320, 240, 0, 0, getModuleHandleW(0), 0)
	if host == 0 {
		return "host window nil"
	}
	hostPtr := *(*unsafe.Pointer)(unsafe.Pointer(&host))
	w := &View{window: hostPtr}
	if err := testApp().Show(w); err != nil {
		return "new error: " + err.Error()
	}
	owns := w.w.ownsWindow

	setWindowPos(host, 0, 0, 0, 500, 400, swpNoZOrder|swpNoActivate|swpNoMove)
	var want, got rect
	clientRectOf(host, &want)
	asController(w.w.controller).getBounds(&got)
	if got != want {
		w.Close()
		destroyWindow(host)
		return fmt.Sprintf("bounds after host resize = %+v, want %+v (WM_SIZE not routed)", got, want)
	}

	ran := false
	w.w.Dispatch(func() { ran = true })
	for range 8 {
		postMessageW(host, wmApp+1, 0, 0)
	}
	var m msgStruct
	for i := 0; i < 9 && !ran && getMessageW(&m, 0, 0, 0) > 0; i++ {
		translateMessage(&m)
		dispatchMessageW(&m)
	}
	if !ran {
		w.Close()
		destroyWindow(host)
		return "Dispatch closure never ran in embed mode (WM_APP not routed)"
	}

	w.Close()
	alive := getWindowLongPtrW(host, gwlStyle) != 0
	destroyWindow(host)
	if owns {
		return "owns=true (BUG: should not own external window)"
	}
	if !alive {
		return "host destroyed (BUG)"
	}
	return "embed-ok"
}

func TestEmbedExternalWindow(t *testing.T) {
	got, _ := resWinEmbed.Load().(string)
	requireGUI(t, got)
	if got != "embed-ok" {
		t.Fatalf("embed external window = %q, want %q", got, "embed-ok")
	}
}

func TestCloseViaUI(t *testing.T) {
	got, _ := resWinClose.Load().(string)
	requireGUI(t, got)
	if got != "closed" {
		t.Fatalf("close via UI = %q, want %q", got, "closed")
	}
}

func winBridgeScenario() string {
	w := &View{Debug: true, Width: 700, Height: 500}
	if err := testApp().Show(w); err != nil {
		return "new error: " + err.Error()
	}
	defer w.Close()

	done := make(chan string, 1)
	_ = w.w.Bind("add", func(a, b float64) float64 { return a + b })
	_ = w.w.Bind("hello", func(s string) string { return "hi " + s })
	_ = w.w.Bind("done", func(s string) {
		select {
		case done <- s:
		default:
		}
		w.Close()
	})
	time.AfterFunc(40*time.Second, func() { w.Close() })

	w.w.loadHTML(`<!DOCTYPE html><html><body><script>
window.addEventListener('load', async function(){
  try {
    var s = await window.add(20, 22);
    var h = await window.hello("x");
    window.done(s + "|" + h);
  } catch(e) { window.done("ERR:" + e); }
});
</script></body></html>`)
	w.w.Run()

	select {
	case r := <-done:
		return r
	default:
		return "no report"
	}
}

func winErrorUnbindScenario() string {
	w := &View{}
	if err := testApp().Show(w); err != nil {
		return "new error: " + err.Error()
	}
	defer w.Close()

	done := make(chan string, 1)
	_ = w.w.Bind("report", func(s string) {
		select {
		case done <- s:
		default:
		}
		w.Close()
	})
	_ = w.w.Bind("boom", func() (string, error) { return "", errors.New("kaboom") })
	_ = w.w.Bind("temp", func() string { return "x" })
	_ = w.w.Unbind("temp")
	time.AfterFunc(40*time.Second, func() { w.Close() })

	w.w.loadHTML(`<!DOCTYPE html><html><body><script>
window.addEventListener('load', async function(){
  var msg = 'temp=' + (typeof window.temp);
  try { await window.boom(); msg += ' boom=nope'; }
  catch(e){ msg += ' boom=' + e; }
  window.report(msg);
});
</script></body></html>`)
	w.w.Run()

	select {
	case r := <-done:
		return r
	default:
		return "no report"
	}
}

type xy struct{ X, Y int }

func winRichTypesScenario() string {
	w := &View{}
	if err := testApp().Show(w); err != nil {
		return "new error: " + err.Error()
	}
	defer w.Close()

	done := make(chan string, 1)
	_ = w.w.Bind("report", func(s string) {
		select {
		case done <- s:
		default:
		}
		w.Close()
	})
	_ = w.w.Bind("echoPoint", func(p xy) xy { return xy{p.X + 1, p.Y + 1} })
	_ = w.w.Bind("sum", func(xs []int) int {
		t := 0
		for _, x := range xs {
			t += x
		}
		return t
	})
	time.AfterFunc(40*time.Second, func() { w.Close() })

	w.w.loadHTML(`<!DOCTYPE html><html><body><script>
window.addEventListener('load', async function(){
  try {
    var p = await window.echoPoint({X:1, Y:2});
    var s = await window.sum([1,2,3,4]);
    window.report('p=' + p.X + ',' + p.Y + ' s=' + s);
  } catch(e) { window.report('ERR:' + e); }
});
</script></body></html>`)
	w.w.Run()

	select {
	case r := <-done:
		return r
	default:
		return "no report"
	}
}

func TestBridge(t *testing.T) {
	got, _ := resWinBridge.Load().(string)
	requireGUI(t, got)
	if got != "42|hi x" {
		t.Fatalf("JS<->Go bridge = %q, want %q", got, "42|hi x")
	}
}

func TestErrorAndUnbind(t *testing.T) {
	const want = "temp=undefined boom=kaboom"
	got, _ := resWinErrorUnbind.Load().(string)
	requireGUI(t, got)
	if got != want {
		t.Fatalf("error/unbind = %q, want %q", got, want)
	}
}

func TestRichBindingTypes(t *testing.T) {
	const want = "p=2,3 s=10"
	got, _ := resWinRichTypes.Load().(string)
	requireGUI(t, got)
	if got != want {
		t.Fatalf("rich types = %q, want %q", got, want)
	}
}

func serveDummy() contentFunc { return func(*contentRequest) *contentResponse { return nil } }

func TestRewriteSchemeURL(t *testing.T) {
	w := &webview{serve: serveDummy()}

	cases := []struct {
		name, in, want string
	}{
		{"registered scheme -> vhost", "app://home/index.html", "https://app.localhost/index.html"},
		{"keeps query", "app://home/x?y=1", "https://app.localhost/x?y=1"},
		{"keeps fragment", "app://home/index.html#/route", "https://app.localhost/index.html#/route"},
		{"keeps query and fragment", "app://home/x?y=1#/r", "https://app.localhost/x?y=1#/r"},
		{"root path", "app://home/", "https://app.localhost/"},
		{"unregistered scheme passes through", "other://z/a", "other://z/a"},
		{"https passes through", "https://example.com/a", "https://example.com/a"},
	}
	for _, c := range cases {
		got := w.rewriteSchemeURL(c.in)
		if got != c.want {
			t.Errorf("%s: rewriteSchemeURL(%q) = %q, want %q", c.name, c.in, got, c.want)
		}
	}
}

func TestRewriteSchemeURLNoContent(t *testing.T) {
	w := &webview{}
	in := "app://home/index.html"
	got := w.rewriteSchemeURL(in)
	if got != in {
		t.Errorf("rewriteSchemeURL(%q) with no resolver = %q, want unchanged", in, got)
	}
}

func TestCanonicalSchemeURL(t *testing.T) {
	w := &webview{
		serve:           serveDummy(),
		schemeAuthority: "home",
	}
	cases := []struct {
		name, in, want string
	}{
		{"restores authority", "https://app.localhost/index.html", "app://home/index.html"},
		{"restores authority + query", "https://app.localhost/x?y=1", "app://home/x?y=1"},
		{"root", "https://app.localhost/", "app://home/"},
	}
	for _, c := range cases {
		got := w.canonicalAppURL(c.in)
		if got != c.want {
			t.Errorf("%s: canonicalSchemeURL(%q) = %q, want %q", c.name, c.in, got, c.want)
		}
	}
}

func TestCanonicalSchemeURLFallback(t *testing.T) {
	w := &webview{serve: serveDummy()}
	got := w.canonicalAppURL("https://app.localhost/index.html")
	want := "app://app/index.html"
	if got != want {
		t.Errorf("canonicalSchemeURL fallback = %q, want %q", got, want)
	}
}

func TestSchemeURLRoundTrip(t *testing.T) {
	w := &webview{serve: serveDummy()}
	w.rewriteSchemeURL("app://home/index.html")
	got := w.canonicalAppURL("https://app.localhost/assets/app.js")
	want := "app://home/assets/app.js"
	if got != want {
		t.Errorf("round-trip = %q, want %q", got, want)
	}
}

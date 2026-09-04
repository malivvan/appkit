package appkit

import (
	"errors"
	"flag"
	"os"
	"runtime"
	"strconv"
	"sync/atomic"
	"testing"
	"time"
	"unsafe"

	"github.com/ebitengine/purego/objc"

	"github.com/malivvan/appkit/dialog"
)

var (
	resBridge       atomic.Value
	resErrorUnbind  atomic.Value
	resRichTypes    atomic.Value
	resMultiWindow  atomic.Value
	resEmbed        atomic.Value
	resOpenPanel    atomic.Value
	resDialogCfg    atomic.Value
	resFirstMouse   atomic.Value
	resHitTest      atomic.Value
	resRaise        atomic.Value
	resExternalLoop atomic.Value
	resWindowState  atomic.Value
)

func TestMain(m *testing.M) {
	flag.Parse()
	if !testing.Short() {
		runtime.LockOSThread()
		resBridge.Store(bridgeScenario())
		resErrorUnbind.Store(errorUnbindScenario())
		resRichTypes.Store(richTypesScenario())
		resMultiWindow.Store(multiWindowScenario())
		resEmbed.Store(embedScenario())
		resOpenPanel.Store(openPanelCompletionScenario())
		resDialogCfg.Store(dialogConfigScenario())
		resFirstMouse.Store(firstMouseScenario())
		resHitTest.Store(hitTestFirstMouseScenario())
		resRaise.Store(raiseScenario())
		resWindowState.Store(windowStateScenario())
		resExternalLoop.Store(externalLoopScenario())
	}
	os.Exit(m.Run())
}

func requireGUI(t *testing.T, got string) {
	t.Helper()
	if got == "" {
		t.Skip("GUI scenarios skipped (-short)")
	}
}

func openPanelCompletionScenario() string {
	done := make(chan objc.ID, 1)
	block := objc.NewBlock(func(_ objc.Block, urls objc.ID) {
		select {
		case done <- urls:
		default:
		}
	})
	invokeOpenPanelCompletion(objc.ID(uintptr(block)), 0)
	select {
	case urls := <-done:
		if urls != 0 {
			return "urls=nonnil (want nil for cancel)"
		}
		return "panel-ok"
	case <-time.After(2 * time.Second):
		return "completion handler not invoked"
	}
}

func multiWindowScenario() string {
	start := atomic.LoadInt32(&windowCount)
	w1 := &View{}
	if err := testApp().Show(w1); err != nil {
		return "w1 error: " + err.Error()
	}
	w2 := &View{window: nil}
	if err := testApp().Show(w2); err != nil {
		return "w2 error: " + err.Error()
	}
	peak := atomic.LoadInt32(&windowCount)
	w1.Close()
	w2.Close()
	end := atomic.LoadInt32(&windowCount)
	return strconv.Itoa(int(start)) + "->" + strconv.Itoa(int(peak)) + "->" + strconv.Itoa(int(end))
}

func embedScenario() string {
	host := objcClass("NSWindow").Send(selector("alloc"))
	host = host.Send(selector("initWithContentRect:styleMask:backing:defer:"),
		cgRect{cgPoint{0, 0}, cgSize{400, 300}},
		uint(1), nsBackingStoreBuffered, false)
	host = host.Send(selector("retain"))

	hostPtr := *(*unsafe.Pointer)(unsafe.Pointer(&host))
	w := &View{window: hostPtr}
	if err := testApp().Show(w); err != nil {
		return "new error: " + err.Error()
	}
	owns := w.w.ownsWindow
	w.Close()

	host.Send(selector("setTitle:"), nsString("still alive"))
	host.Send(selector("close"))
	host.Send(selector("release"))

	if owns {
		return "owns=true (BUG: should not own external window)"
	}
	return "embed-ok"
}

func TestMultiWindowRefCount(t *testing.T) {
	const want = "0->2->0"
	got, _ := resMultiWindow.Load().(string)
	requireGUI(t, got)
	if got != want {
		t.Fatalf("window ref-count = %q, want %q", got, want)
	}
}

func TestEmbedExternalWindow(t *testing.T) {
	got, _ := resEmbed.Load().(string)
	requireGUI(t, got)
	if got != "embed-ok" {
		t.Fatalf("embed external window = %q, want %q", got, "embed-ok")
	}
}

func TestOpenPanelCompletion(t *testing.T) {
	got, _ := resOpenPanel.Load().(string)
	requireGUI(t, got)
	if got != "panel-ok" {
		t.Fatalf("open-panel completion = %q, want %q", got, "panel-ok")
	}
}

func dialogConfigScenario() string {
	res := "dialog-config-ok"
	autorelease(func() {
		p := objcClass("NSOpenPanel").Send(selector("openPanel"))
		configureOpenPanel(p, true, false, true, dialog.Options{
			Title:   "Pick a file",
			Filters: []dialog.FileFilter{{Name: "Images", Extensions: []string{"png", ".jpg"}}},
		})
		switch {
		case p.Send(selector("canChooseFiles")) == 0:
			res = "file: canChooseFiles=false"
		case p.Send(selector("canChooseDirectories")) != 0:
			res = "file: canChooseDirectories=true (want false)"
		case p.Send(selector("allowsMultipleSelection")) == 0:
			res = "file: allowsMultipleSelection=false"
		case int(p.Send(selector("allowedFileTypes")).Send(selector("count"))) != 2:
			res = "file: allowedFileTypes count != 2"
		}
		if res != "dialog-config-ok" {
			return
		}
		d := objcClass("NSOpenPanel").Send(selector("openPanel"))
		configureOpenPanel(d, false, true, false, dialog.Options{
			Filters: []dialog.FileFilter{{Extensions: []string{"*"}}},
		})
		switch {
		case d.Send(selector("canChooseFiles")) != 0:
			res = "dir: canChooseFiles=true (want false)"
		case d.Send(selector("canChooseDirectories")) == 0:
			res = "dir: canChooseDirectories=false"
		case d.Send(selector("allowedFileTypes")) != 0:
			res = "dir: allowedFileTypes set (want nil for wildcard)"
		}
	})
	return res
}

func TestDialogConfig(t *testing.T) {
	got, _ := resDialogCfg.Load().(string)
	requireGUI(t, got)
	if got != "dialog-config-ok" {
		t.Fatalf("dialog config = %q, want %q", got, "dialog-config-ok")
	}
}

func bridgeScenario() string {
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
	time.AfterFunc(15*time.Second, func() { w.Close() })

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

func errorUnbindScenario() string {
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
	time.AfterFunc(15*time.Second, func() { w.Close() })

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

type point struct{ X, Y int }

func richTypesScenario() string {
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
	_ = w.w.Bind("echoPoint", func(p point) point { return point{p.X + 1, p.Y + 1} })
	_ = w.w.Bind("sum", func(xs []int) int {
		t := 0
		for _, x := range xs {
			t += x
		}
		return t
	})
	time.AfterFunc(15*time.Second, func() { w.Close() })

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
	got, _ := resBridge.Load().(string)
	requireGUI(t, got)
	if got != "42|hi x" {
		t.Fatalf("JS<->Go bridge = %q, want %q", got, "42|hi x")
	}
}

func TestErrorAndUnbind(t *testing.T) {
	const want = "temp=undefined boom=kaboom"
	got, _ := resErrorUnbind.Load().(string)
	requireGUI(t, got)
	if got != want {
		t.Fatalf("error/unbind = %q, want %q", got, want)
	}
}

func TestRichBindingTypes(t *testing.T) {
	const want = "p=2,3 s=10"
	got, _ := resRichTypes.Load().(string)
	requireGUI(t, got)
	if got != want {
		t.Fatalf("rich types = %q, want %q", got, want)
	}
}

func firstMouseScenario() string {
	ask := func(v *View) (string, bool) {
		if err := testApp().Show(v); err != nil {
			return "new error: " + err.Error(), false
		}
		defer v.Close()
		view := v.w.webView
		if view == 0 {
			return "no web view was created", false
		}
		if view.Send(selector("respondsToSelector:"), selector("acceptsFirstMouse:")) == 0 {
			return "the view does not respond to acceptsFirstMouse:", false
		}
		return "", bool(view.Send(selector("acceptsFirstMouse:"), objc.ID(0)) != 0)
	}

	msg, on := ask(&View{FirstMouse: true})
	if msg != "" {
		return msg
	}
	if !on {
		return "opted in, but the view still refuses the first mouse"
	}
	msg, off := ask(&View{})
	if msg != "" {
		return msg
	}
	if off {
		return "not opted in, but the view accepts the first mouse (the default must stay AppKit's)"
	}
	return "first-mouse-ok"
}

func TestFirstMouseIsOptIn(t *testing.T) {
	const want = "first-mouse-ok"
	got, _ := resFirstMouse.Load().(string)
	requireGUI(t, got)
	if got != want {
		t.Fatalf("acceptsFirstMouse: got %q, want %q", got, want)
	}
}

func hitTestFirstMouseScenario() string {
	w := &View{FirstMouse: true}
	if err := testApp().Show(w); err != nil {
		return "new error: " + err.Error()
	}
	defer w.Close()
	wv := w.w.webView
	win := w.w.window
	content := win.Send(selector("contentView"))
	hit := content.Send(selector("hitTest:"), cgPoint{200, 200})

	name := func(id objc.ID) string {
		if id == 0 {
			return "<nil>"
		}
		return cString(id.Send(selector("className")).Send(selector("UTF8String")))
	}
	accepts := func(id objc.ID) string {
		if id == 0 {
			return "-"
		}
		if id.Send(selector("respondsToSelector:"), selector("acceptsFirstMouse:")) == 0 {
			return "no-selector"
		}
		if id.Send(selector("acceptsFirstMouse:"), objc.ID(0)) != 0 {
			return "YES"
		}
		return "NO"
	}
	if accepts(hit) != "YES" {
		return "the view under the cursor refuses the first mouse: hit=" +
			name(hit) + "/" + accepts(hit) + " webView=" + name(wv) + "/" + accepts(wv)
	}
	_ = content
	return "hit-test-ok"
}

func TestTheViewUnderTheCursorAcceptsTheFirstMouse(t *testing.T) {
	const want = "hit-test-ok"
	got, _ := resHitTest.Load().(string)
	requireGUI(t, got)
	if got != want {
		t.Fatalf("hit-test first mouse: got %q, want %q", got, want)
	}
}

func raiseScenario() string {
	w := &View{}
	if err := testApp().Show(w); err != nil {
		return "new error: " + err.Error()
	}
	defer w.Close()
	win := w.w.window

	win.Send(selector("orderOut:"), objc.ID(0))
	if win.Send(selector("isKeyWindow")) != 0 {
		return "the window is still key after orderOut: the fixture proves nothing"
	}

	w.Focus(true)
	if win.Send(selector("isKeyWindow")) == 0 {
		return "Raise left the window not key"
	}
	return "raise-ok"
}

func TestRaiseMakesTheWindowKey(t *testing.T) {
	const want = "raise-ok"
	got, _ := resRaise.Load().(string)
	requireGUI(t, got)
	if got != want {
		t.Fatalf("Raise: got %q, want %q", got, want)
	}
}

func windowStateScenario() string {
	w := &View{}
	if err := testApp().Show(w); err != nil {
		return "new error: " + err.Error()
	}
	defer w.Close()
	wv := w.w
	win := wv.window
	wv.runEventLoopWhile(func() bool { return win.Send(selector("isVisible")) == 0 })
	fr := func() cgRect { return objc.Send[cgRect](win, selector("frame")) }

	before := fr()
	wv.maximized = false
	w.Maximize()
	wv.runEventLoopWhile(func() bool { return !wv.maximized })
	mid := fr()
	w.Unmaximize()
	wv.runEventLoopWhile(func() bool { return wv.maximized })
	after := fr()

	if !(mid.Size.Width > before.Size.Width && mid.Size.Height > before.Size.Height) {
		return "maximize did not enlarge the frameless window (" + ws(before) + " -> " + ws(mid) + ")"
	}
	if !(rEq(after.Origin.X, before.Origin.X) && rEq(after.Origin.Y, before.Origin.Y) &&
		rEq(after.Size.Width, before.Size.Width) && rEq(after.Size.Height, before.Size.Height)) {
		return "unmaximize did not restore the frameless frame (" + ws(before) + " -> " + ws(after) + ")"
	}

	wv.minimized = false
	w.Minimize()
	wv.runEventLoopWhile(func() bool { return !wv.minimized })
	hidden := win.Send(selector("isVisible")) == 0
	w.Unminimize()
	wv.runEventLoopWhile(func() bool { return wv.minimized })
	shown := win.Send(selector("isVisible")) != 0
	if !hidden {
		return "frameless minimize did not hide the window"
	}
	if !shown {
		return "frameless unminimize did not restore the window"
	}
	return "window-state-ok"
}

func ws(r cgRect) string {
	return strconv.FormatFloat(r.Size.Width, 'f', 0, 64) + "x" +
		strconv.FormatFloat(r.Size.Height, 'f', 0, 64) + "@" +
		strconv.FormatFloat(r.Origin.X, 'f', 0, 64) + "," +
		strconv.FormatFloat(r.Origin.Y, 'f', 0, 64)
}

func rEq(a, b float64) bool {
	d := a - b
	if d < 0 {
		d = -d
	}
	return d < 1
}

func TestFramelessWindowState(t *testing.T) {
	got, _ := resWindowState.Load().(string)
	requireGUI(t, got)
	const want = "window-state-ok"
	if got != want {
		t.Fatalf("frameless window state: got %q, want %q", got, want)
	}
}

func externalLoopScenario() string {
	app := objcClass("NSApplication").Send(selector("sharedApplication"))
	res := make(chan string, 1)

	go func() {
		verdict := func() string {
			for i := 0; app.Send(selector("isRunning")) == 0; i++ {
				if i > 500 {
					return "host loop never started"
				}
				time.Sleep(10 * time.Millisecond)
			}
			done := make(chan string, 1)
			go func() {
				w := &View{}
				if err := testApp().Show(w); err != nil {
					done <- "new error: " + err.Error()
					return
				}
				defer w.Close()
				w.w.loadHTML("<html><body>external loop</body></html>")
				go func() {
					time.Sleep(500 * time.Millisecond)
					w.Close()
				}()
				w.w.Run()
				done <- "external-loop-ok"
			}()
			select {
			case s := <-done:
				if s != "external-loop-ok" {
					return s
				}
			case <-time.After(15 * time.Second):
				return "timeout: New or Run blocked under a running loop (deadlock)"
			}

			syncRes := make(chan string, 1)
			dispatchMain(func() {
				w := &View{}
				if err := testApp().Show(w); err != nil {
					syncRes <- "sync new error: " + err.Error()
					return
				}
				defer w.Close()
				w.w.loadHTML("<html><body>external loop, sync shape</body></html>")
				go func() {
					time.Sleep(300 * time.Millisecond)
					w.Close()
				}()
				w.w.Run()
				syncRes <- "external-loop-ok"
			})
			select {
			case s := <-syncRes:
				return s
			case <-time.After(15 * time.Second):
				return "timeout: sync (OnClick-shaped) lifecycle hung"
			}
		}()
		if app.Send(selector("isRunning")) == 0 {
			verdict += " (webview close stopped the host loop)"
		}
		res <- verdict
		dispatchMain(func() {
			autorelease(func() {
				app.Send(selector("stop:"), objc.ID(0))
				postWakeEvent(app)
			})
		})
	}()

	app.Send(selector("run"))
	return <-res
}

func TestNewUnderAnExternalRunLoop(t *testing.T) {
	const want = "external-loop-ok"
	got, _ := resExternalLoop.Load().(string)
	requireGUI(t, got)
	if got != want {
		t.Fatalf("external run loop: got %q, want %q", got, want)
	}
}

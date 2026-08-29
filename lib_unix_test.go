//go:build linux || freebsd || netbsd

package appkit

import (
	"errors"
	"flag"
	"os"
	"os/exec"
	"runtime"
	"sync/atomic"
	"testing"
	"time"
	"unsafe"
)

var (
	resBridge      atomic.Value
	resErrorUnbind atomic.Value
	resRichTypes   atomic.Value
	resEmbed       atomic.Value
	resWaitClose   atomic.Value
	resGeometry    atomic.Value
	resIcon        atomic.Value
)

func hasDisplay() bool {
	return os.Getenv("DISPLAY") != "" || os.Getenv("WAYLAND_DISPLAY") != ""
}

func webkitRunnable() bool {
	bwrap, err := exec.LookPath("bwrap")
	if err == nil {
		if exec.Command(bwrap, "--unshare-user", "--", "/bin/true").Run() != nil {
			return false
		}
	}
	if d := os.Getenv("XDG_RUNTIME_DIR"); d != "" && !isDirWritable(d) {
		return false
	}
	return true
}

func guiAvailable() bool {
	return hasDisplay() && webkitRunnable() && ensureInit() == nil
}

func TestMain(m *testing.M) {
	flag.Parse()
	runtime.LockOSThread()
	if !testing.Short() && guiAvailable() {
		resBridge.Store(bridgeScenario())
		resErrorUnbind.Store(errorUnbindScenario())
		resRichTypes.Store(richTypesScenario())
		resEmbed.Store(embedScenario())
		resWaitClose.Store(waitCloseScenario())
	}
	os.Exit(m.Run())
}

func embedScenario() string {
	err := ensureInit()
	if err != nil {
		return "init error: " + err.Error()
	}
	if !gtkInit() {
		return "gtk_init failed"
	}
	host := gtkNewWindow()
	if host == 0 {
		return "host window nil"
	}
	hostPtr := *(*unsafe.Pointer)(unsafe.Pointer(&host))
	w := &View{window: hostPtr}
	if err := testApp().Show(w); err != nil {
		return "new error: " + err.Error()
	}
	owns := w.w.ownsWindow
	w.Close()
	gtkWindowResize(host, 300, 200)
	gtkWindowClose(host)
	if owns {
		return "owns=true (BUG: should not own external window)"
	}
	return "embed-ok"
}

func TestEmbedExternalWindow(t *testing.T) {
	got, _ := resEmbed.Load().(string)
	requireGUI(t, got)
	if got != "embed-ok" {
		t.Fatalf("embed external window = %q, want %q", got, "embed-ok")
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

func requireGUI(t *testing.T, got string) {
	t.Helper()
	if got == "" {
		t.Skip("WebKitGTK/display not available; install libwebkit2gtk and run under xvfb-run")
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

func TestLinuxBackendOverride(t *testing.T) {
	cases := []struct {
		env  string
		want int
	}{
		{"", backendAuto},
		{envBackendGTK4, backendGTK4},
		{envBackendGTK3, backendGTK3},
		{"gtk4", backendAuto},
		{"WEBKITGTK-6.0", backendAuto},
	}
	for _, c := range cases {
		t.Setenv("APPKIT_BACKEND", c.env)
		if got := linuxBackendOverride(); got != c.want {
			t.Errorf("APPKIT_BACKEND=%q: got %d, want %d", c.env, got, c.want)
		}
	}
}

func waitCloseScenario() string {
	app := &App{Exit: true}
	w := &View{Width: 400, Height: 300}
	if err := app.Show(w); err != nil {
		return "view error: " + err.Error()
	}
	time.AfterFunc(300*time.Millisecond, func() {
		w.Maximize()
		w.Unmaximize()
		w.Minimize()
	})
	time.AfterFunc(900*time.Millisecond, func() {
		w.Show()
	})
	time.AfterFunc(1200*time.Millisecond, func() {
		dispatchMain(func() { w.Close() })
	})
	if err := app.Wait(); err != nil {
		return "wait error: " + err.Error()
	}
	return "wait-ok"
}

func TestWaitReturnsAfterLastWindowCloses(t *testing.T) {
	got, _ := resWaitClose.Load().(string)
	requireGUI(t, got)
	if got != "wait-ok" {
		t.Fatalf("wait/close scenario = %q, want %q", got, "wait-ok")
	}
}

//go:build linux || freebsd || netbsd

package appkit

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"
	"unsafe"

	"github.com/ebitengine/purego"
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
	if !testing.Short() && geometryAvailable() {
		resGeometry.Store(geometryScenario())
		resIcon.Store(iconScenario())
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

const (
	geomWidth  = 320
	geomHeight = 240
)

func geometryAvailable() bool {
	return hasDisplay() && ensureInit() == nil
}

func geometryScenario() string {
	p := newGeometryProbe()
	if p == nil {
		return "ERROR: geometry probe could not bind the toolkit readback"
	}
	var sb strings.Builder
	for _, state := range []State{StateNone, StateFixed} {
		name := map[State]string{StateNone: "none", StateFixed: "fixed"}[state]
		v := &View{Width: geomWidth, Height: geomHeight, State: state}
		if err := testApp().Show(v); err != nil {
			return "ERROR: " + name + ": show: " + err.Error()
		}
		for i := 0; i < 40; i++ {
			gMainContextIteration(0, false)
			time.Sleep(5 * time.Millisecond)
		}
		w, h := p.size(v.w.window)
		fmt.Fprintf(&sb, "%s=%dx%d", name, w, h)
		sb.WriteString(";")
		v.Close()
	}
	return sb.String()
}

type geometryProbe struct {
	size func(window uintptr) (w, h int)
}

func newGeometryProbe() *geometryProbe {
	stack := "libgtk-3.so.0"
	if gtk4 {
		stack = "libgtk-4.so.1"
	}
	gtk, err := purego.Dlopen(stack, 2)
	if err != nil {
		return nil
	}
	var (
		gtkWindowGetSize   func(window uintptr, w, h *int32)
		gtkWidgetGetWidth  func(widget uintptr) int32
		gtkWidgetGetHeight func(widget uintptr) int32
	)
	p := &geometryProbe{}
	if gtk4 {
		purego.RegisterLibFunc(&gtkWidgetGetWidth, gtk, "gtk_widget_get_width")
		purego.RegisterLibFunc(&gtkWidgetGetHeight, gtk, "gtk_widget_get_height")
		p.size = func(window uintptr) (int, int) {
			return int(gtkWidgetGetWidth(window)), int(gtkWidgetGetHeight(window))
		}
	} else {
		purego.RegisterLibFunc(&gtkWindowGetSize, gtk, "gtk_window_get_size")
		p.size = func(window uintptr) (int, int) {
			var w, h int32
			gtkWindowGetSize(window, &w, &h)
			return int(w), int(h)
		}
	}
	return p
}

func TestCreationTimeGeometry(t *testing.T) {
	got, _ := resGeometry.Load().(string)
	if got == "" {
		t.Skip("GTK/display not available; install the GTK libraries and run under a display")
	}
	if strings.HasPrefix(got, "ERROR: ") {
		t.Fatalf("geometry scenario: %s", strings.TrimPrefix(got, "ERROR: "))
	}
	entries := strings.Split(strings.TrimSuffix(got, ";"), ";")
	if len(entries) == 0 {
		t.Fatalf("geometry scenario reported nothing: %q", got)
	}
	for _, entry := range entries {
		name, rest, ok := strings.Cut(entry, "=")
		if !ok {
			t.Fatalf("malformed geometry report entry %q (in %q)", entry, got)
		}
		size := rest
		if want := fmt.Sprintf("%dx%d", geomWidth, geomHeight); size != want {
			t.Errorf("State%s window size = %s, want %s", name, size, want)
		}
	}
}

func iconScenario() string {
	if !gtk4 || !haveX11Icons {
		return ""
	}
	if err := setAppIcon(embeddedIcon, "appkit-icon-probe"); err != nil {
		return "ERROR: setAppIcon: " + err.Error()
	}
	if len(appIconARGB) == 0 {
		return "ERROR: setAppIcon built no _NET_WM_ICON payload"
	}
	v := &View{Width: 320, Height: 240}
	if err := testApp().Show(v); err != nil {
		return "ERROR: show: " + err.Error()
	}
	defer v.Close()
	if !x11Display() {
		return ""
	}
	for i := 0; i < 40; i++ {
		gMainContextIteration(0, false)
		time.Sleep(5 * time.Millisecond)
	}
	got, ok := readNETWMICON(v.w.window)
	if !ok {
		return "ERROR: _NET_WM_ICON is not set on the X11 window"
	}
	want := appIconARGB
	if len(got) != len(want) {
		return fmt.Sprintf("ERROR: _NET_WM_ICON has %d CARDINALs, want %d", len(got), len(want))
	}
	for i := range want {
		if uint32(got[i]) != uint32(want[i]) {
			return fmt.Sprintf("ERROR: _NET_WM_ICON[%d] = %#x, want %#x", i, uint32(got[i]), uint32(want[i]))
		}
	}
	return fmt.Sprintf("icon-ok=%dx%d", uint32(got[0]), uint32(got[1]))
}

func readNETWMICON(window uintptr) ([]uintptr, bool) {
	if !haveX11Icons || gdkX11SurfaceGetXid == nil || gdkX11DisplayGetXdisplay == nil || xInternAtom == nil {
		return nil, false
	}
	xlib, err := purego.Dlopen("libX11.so.6", 2)
	if err != nil {
		return nil, false
	}
	var (
		xGetWindowProperty func(display, window, property uintptr, longOffset, longLength int64, del int32, reqType uintptr,
			actualType, actualFormat, nitems, bytesAfter *uintptr, prop *unsafe.Pointer) int32
		xFree func(data uintptr) int32
	)
	purego.RegisterLibFunc(&xGetWindowProperty, xlib, "XGetWindowProperty")
	purego.RegisterLibFunc(&xFree, xlib, "XFree")

	surface := gtkNativeGetSurface(window)
	if surface == 0 {
		return nil, false
	}
	xid := gdkX11SurfaceGetXid(surface)
	display := gdkX11DisplayGetXdisplay(gdkDisplayGetDefault())
	if xid == 0 || display == 0 {
		return nil, false
	}
	atom := xInternAtom(display, "_NET_WM_ICON", 1)
	if atom == 0 {
		return nil, false
	}
	var actualType, actualFormat, nitems, bytesAfter uintptr
	var prop unsafe.Pointer
	if xGetWindowProperty(display, xid, atom, 0, 1<<24, 0, 0,
		&actualType, &actualFormat, &nitems, &bytesAfter, &prop) != 0 || prop == nil {
		return nil, false
	}
	defer func() { _ = xFree(uintptr(prop)) }()
	if actualType == 0 || actualFormat != 32 || nitems == 0 {
		return nil, false
	}
	out := make([]uintptr, nitems)
	copy(out, unsafe.Slice((*uintptr)(prop), nitems))
	return out, true
}

func TestX11WindowIcon(t *testing.T) {
	got, _ := resIcon.Load().(string)
	if got == "" {
		t.Skip("GTK4 X11 icon path not available (no display, or a Wayland/GTK3 backend)")
	}
	if strings.HasPrefix(got, "ERROR: ") {
		t.Fatalf("icon scenario: %s", strings.TrimPrefix(got, "ERROR: "))
	}
}

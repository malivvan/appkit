package appkit

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"slices"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"
	"unsafe"

	"github.com/malivvan/appkit/dialog"
	"github.com/malivvan/appkit/tray"
)

func uniqueID(name string) string {
	return fmt.Sprintf("native-instance-test-%s-%d", name, os.Getpid())
}

func TestSnapshotConfigCarriesTray(t *testing.T) {
	cfg := &tray.Config{Tooltip: "snapshot-test"}
	got := snapshotSetup(&App{Name: "snapshot-test", Tray: cfg})
	if got.Tray != cfg {
		t.Fatalf("snapshot Tray = %v, want the committed config pointer", got.Tray)
	}
	if snapshotSetup(&App{}).Tray != nil {
		t.Fatal("snapshot Tray should be nil when App.Tray is unset")
	}
}

const (
	testAppID   = "com.example.app"
	testAppName = "My App"
	testIndex   = "index.html"
	testCSSBody = "body{}"
	testHTML    = "<h1>hi</h1>"
)

func TestSnapshotConfigCarriesExitIDAndExec(t *testing.T) {
	call := func([]string) {}
	got := snapshotSetup(&App{Exit: true, ID: testAppID, Exec: call})
	if !got.Exit {
		t.Fatal("snapshot Exit = false, want the committed true")
	}
	if got.ID != testAppID {
		t.Fatalf("snapshot ID = %q, want the committed id", got.ID)
	}
	if got.Exec == nil {
		t.Fatal("snapshot Exec = nil, want the committed callback")
	}
	zero := snapshotSetup(&App{})
	if zero.Exit {
		t.Fatal("snapshot Exit should default to false")
	}
	if zero.ID != "" {
		t.Fatalf("snapshot ID should default to empty, got %q", zero.ID)
	}
	if zero.Exec != nil {
		t.Fatal("snapshot Exec should default to nil (single-instance mode off)")
	}
}

func TestSingleInstanceDisabledWithoutExec(t *testing.T) {
	for _, cfg := range []*appSetup{
		{},
		{ID: testAppID},
	} {
		release, err := claimPrimaryInstance(cfg)
		if err != nil {
			t.Fatalf("claimPrimaryInstance(%+v): unexpected error %v", cfg, err)
		}
		if release == nil {
			t.Fatalf("claimPrimaryInstance(%+v): nil release", cfg)
		}
		release()
	}
}

func TestSingleInstanceExecRequiresID(t *testing.T) {
	release, err := claimPrimaryInstance(&appSetup{Exec: func([]string) {}})
	if err == nil {
		t.Fatal("claimPrimaryInstance with Exec set and empty ID: expected error")
	}
	if !strings.Contains(err.Error(), "App.ID is required") {
		t.Fatalf("error = %v, want the ID-required message", err)
	}
	if release != nil {
		t.Fatal("ID-required failure must not return a release")
	}
}

func TestSingleInstanceEnabledByExec(t *testing.T) {
	id := uniqueID("callmode")
	release, err := claimPrimaryInstance(&appSetup{ID: id, Exec: func([]string) {}})
	if err != nil {
		t.Fatalf("claimPrimaryInstance with Exec set: %v", err)
	}
	if release == nil {
		t.Fatal("claimPrimaryInstance with Exec set: nil release")
	}
	defer release()
	if _, err := acquireGuard(id, nil); !errors.Is(err, errInstanceRunning) {
		t.Fatalf("second acquire under active Exec mode = %v, want errInstanceRunning", err)
	}
}

func TestAcquireSendRoundTrip(t *testing.T) {
	id := uniqueID("roundtrip")
	got := make(chan []string, 1)

	inst, err := acquireGuard(id, func(args []string) {
		select {
		case got <- args:
		default:
		}
	})
	if err != nil {
		t.Fatalf("acquireGuard (primary): %v", err)
	}
	defer func() { _ = inst.Release() }()

	second, err := acquireGuard(id, nil)
	if !errors.Is(err, errInstanceRunning) {
		if second != nil {
			_ = second.Release()
		}
		t.Fatalf("second acquireGuard = %v, want errInstanceRunning", err)
	}

	want := []string{"open", "/tmp/a b.txt", "café ✓"}
	if err := signalPeerInstance(id, want); err != nil {
		t.Fatalf("signalPeerInstance: %v", err)
	}
	select {
	case args := <-got:
		if !slices.Equal(args, want) {
			t.Fatalf("forwarded args = %v, want %v", args, want)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("timed out waiting for forwarded args")
	}
}

func TestReleaseAllowsReacquire(t *testing.T) {
	id := uniqueID("reacquire")
	inst, err := acquireGuard(id, nil)
	if err != nil {
		t.Fatalf("first acquireGuard: %v", err)
	}
	if err := inst.Release(); err != nil {
		t.Fatalf("Release: %v", err)
	}
	again, err := acquireGuard(id, nil)
	if err != nil {
		t.Fatalf("re-acquire after Release: %v", err)
	}
	_ = again.Release()
}

func TestSendWithoutInstance(t *testing.T) {
	id := uniqueID("noinstance")
	if err := signalPeerInstance(id, []string{"x"}); err == nil {
		t.Fatal("signalPeerInstance with no running instance should fail")
	}
}

type eventsFakeWV struct {
	*bindMethodsWebViewStub

	mu     sync.Mutex
	initJS []string
	evalJS []string
	bound  map[string]any
	ev     *events
}

func newEventsFakeWV() *eventsFakeWV {
	return &eventsFakeWV{
		bindMethodsWebViewStub: &bindMethodsWebViewStub{},
		bound:                  map[string]any{},
	}
}

func (f *eventsFakeWV) Init(js string) {
	f.mu.Lock()
	f.initJS = append(f.initJS, js)
	f.mu.Unlock()
}

func (f *eventsFakeWV) Eval(js string) {
	f.mu.Lock()
	f.evalJS = append(f.evalJS, js)
	f.mu.Unlock()
}

func (f *eventsFakeWV) Dispatch(fn func()) { fn() }

func (f *eventsFakeWV) Bind(name string, vals ...any) error {
	f.mu.Lock()
	f.bound[name] = vals[0]
	f.mu.Unlock()
	return nil
}

func (f *eventsFakeWV) lastEval() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.evalJS) == 0 {
		return ""
	}
	return f.evalJS[len(f.evalJS)-1]
}

func (f *eventsFakeWV) On(name string, handler func(args ...json.RawMessage)) func() {
	if f.ev == nil {
		return func() {}
	}
	return f.ev.On(name, handler)
}

func (f *eventsFakeWV) Off(name string) {
	if f.ev != nil {
		f.ev.Off(name)
	}
}

func (f *eventsFakeWV) Emit(name string, data ...any) error {
	if f.ev == nil {
		return errors.New("events bridge not installed")
	}
	return f.ev.Emit(name, data...)
}

func wireEvents(t *testing.T, f *eventsFakeWV) *events {
	t.Helper()
	e, err := installEvents(f, "")
	if err != nil {
		t.Fatalf("install events: %v", err)
	}
	f.ev = e
	return e
}

func TestInstallEventsWiresBridge(t *testing.T) {
	f := newEventsFakeWV()
	wireEvents(t, f)
	if len(f.initJS) != 1 || !strings.Contains(f.initJS[0], "window.events") {
		t.Fatalf("events JS not injected via Init: %q", f.initJS)
	}
	_, ok := f.bound[eventsBindName]
	if !ok {
		t.Fatalf("bridge %q not bound; bound names: %v", eventsBindName, keysOf(f.bound))
	}
}

func TestEventsAPICustomGlobal(t *testing.T) {
	f := newEventsFakeWV()
	e, err := installEvents(f, "acme")
	if err != nil {
		t.Fatalf("install events: %v", err)
	}
	f.ev = e
	if len(f.initJS) != 1 {
		t.Fatalf("init scripts = %d, want 1", len(f.initJS))
	}
	js := f.initJS[0]
	if !strings.Contains(js, "window.acme") {
		t.Fatalf("events JS does not install window.acme: %q", js)
	}
	if strings.Contains(js, ".events") {
		t.Fatalf("events JS still nests a .events member: %q", js)
	}
	if err := f.Emit("ping", "pong"); err != nil {
		t.Fatalf("Emit: %v", err)
	}
	ev := f.lastEval()
	if !strings.Contains(ev, "window.acme;if(g&&g._dispatch){g._dispatch(\"ping\"") {
		t.Fatalf("Eval does not reach the acme global's _dispatch: %q", ev)
	}
}

func TestEmitFiresGoHandlerAndEvalsJS(t *testing.T) {
	f := newEventsFakeWV()
	wireEvents(t, f)

	var got []json.RawMessage
	f.On("greet", func(args ...json.RawMessage) { got = args })

	err := f.Emit("greet", map[string]any{"name": "crg"}, 42)
	if err != nil {
		t.Fatalf("Emit: %v", err)
	}

	if len(got) != 2 {
		t.Fatalf("handler received %d args, want 2", len(got))
	}
	var payload struct {
		Name string `json:"name"`
	}
	err = json.Unmarshal(got[0], &payload)
	if err != nil || payload.Name != "crg" {
		t.Fatalf("arg 0 = %s (err %v), want {name:crg}", got[0], err)
	}
	if string(got[1]) != "42" {
		t.Fatalf("arg 1 = %s, want 42", got[1])
	}

	js := f.lastEval()
	if !strings.Contains(js, `_dispatch("greet"`) {
		t.Fatalf("Eval did not dispatch the event: %q", js)
	}
	if !strings.Contains(js, `"name":"crg"`) || !strings.Contains(js, "42") {
		t.Fatalf("Eval missing the payload: %q", js)
	}
}

func TestReceiveFromJSDispatchesToGo(t *testing.T) {
	f := newEventsFakeWV()
	wireEvents(t, f)

	var got []json.RawMessage
	f.On("ui:click", func(args ...json.RawMessage) { got = args })

	bridge, ok := f.bound[eventsBindName].(func(string, []json.RawMessage))
	if !ok {
		t.Fatalf("bound bridge has unexpected type %T", f.bound[eventsBindName])
	}
	bridge("ui:click", []json.RawMessage{json.RawMessage(`"save"`)})

	if len(got) != 1 || string(got[0]) != `"save"` {
		t.Fatalf("Go handler received %v, want [\"save\"]", got)
	}
}

func TestOnCancelStopsHandler(t *testing.T) {
	f := newEventsFakeWV()
	wireEvents(t, f)

	n := 0
	cancel := f.On("tick", func(args ...json.RawMessage) { n++ })

	_ = f.Emit("tick")
	cancel()
	_ = f.Emit("tick")

	if n != 1 {
		t.Fatalf("handler fired %d times, want 1 (cancelled after first emit)", n)
	}
}

func TestOffRemovesAllHandlers(t *testing.T) {
	f := newEventsFakeWV()
	wireEvents(t, f)

	n := 0
	f.On("x", func(args ...json.RawMessage) { n++ })
	f.On("x", func(args ...json.RawMessage) { n++ })

	_ = f.Emit("x")
	if n != 2 {
		t.Fatalf("both handlers should fire: got %d, want 2", n)
	}

	f.Off("x")
	_ = f.Emit("x")
	if n != 2 {
		t.Fatalf("no handler should fire after Off: got %d, want 2", n)
	}
}

func TestEmitRejectsUnencodableData(t *testing.T) {
	f := newEventsFakeWV()
	wireEvents(t, f)

	fired := false
	f.On("bad", func(args ...json.RawMessage) { fired = true })

	err := f.Emit("bad", make(chan int))
	if err == nil {
		t.Fatal("Emit should fail to encode a channel")
	}
	if fired {
		t.Fatal("no handler should fire when encoding fails")
	}
}

func keysOf(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

type bindMethodsWebViewStub struct {
	bound      map[string]any
	failOn     string
	bindCalls  int
	bindOrder  []string
	unbindCall []string
}

func (s *bindMethodsWebViewStub) Run() {}

func (s *bindMethodsWebViewStub) Terminate() {}

func (s *bindMethodsWebViewStub) Dispatch(_ func()) {}

func (s *bindMethodsWebViewStub) Destroy() {}

func (s *bindMethodsWebViewStub) Window() unsafe.Pointer { return nil }

func (s *bindMethodsWebViewStub) Navigate(_ string) {}

func (s *bindMethodsWebViewStub) Init(_ string) {}

func (s *bindMethodsWebViewStub) Eval(_ string) {}

func (s *bindMethodsWebViewStub) Focus() {}

func (s *bindMethodsWebViewStub) Raise() {}

func (s *bindMethodsWebViewStub) Show() {}

func (s *bindMethodsWebViewStub) Hide() {}

func (s *bindMethodsWebViewStub) Maximize() {}

func (s *bindMethodsWebViewStub) Minimize() {}

func (s *bindMethodsWebViewStub) Unminimize() {}

func (s *bindMethodsWebViewStub) Unmaximize() {}

func (s *bindMethodsWebViewStub) Bind(name string, vals ...any) error {
	s.bindCalls++
	s.bindOrder = append(s.bindOrder, name)
	if name == s.failOn {
		return errors.New("bind failure")
	}
	if s.bound == nil {
		s.bound = make(map[string]any)
	}
	s.bound[name] = vals[0]
	return nil
}

func (s *bindMethodsWebViewStub) Unbind(name string) error {
	s.unbindCall = append(s.unbindCall, name)
	delete(s.bound, name)
	return nil
}

func (s *bindMethodsWebViewStub) On(string, func(...json.RawMessage)) func() { return func() {} }

func (s *bindMethodsWebViewStub) Off(string) {}

func (s *bindMethodsWebViewStub) Emit(string, ...any) error { return nil }

func (s *bindMethodsWebViewStub) Dialog(_ dialog.Options) ([]string, error) { return nil, nil }

type bindMethodsService struct{}

func (bindMethodsService) GetUserByID(_ int) int { return 1 }

func (bindMethodsService) Ping() {}

func TestBindFuncAndConstantBindOneName(t *testing.T) {
	w := &bindMethodsWebViewStub{}
	fn := func() {}
	names, err := bindSingle(w, "app.someAPI.call", fn)
	if err != nil {
		t.Fatalf("bindEntry(func): %v", err)
	}
	if len(names) != 1 || names[0] != "app.someAPI.call" {
		t.Fatalf("bindEntry(func) names = %v, want [app.someAPI.call]", names)
	}
	if _, ok := w.bound["app.someAPI.call"]; !ok {
		t.Fatalf("missing binding for app.someAPI.call; bound: %v", keysOf(w.bound))
	}

	svc := bindMethodsService{}
	cw := &bindMethodsWebViewStub{}
	names, err = bindSingle(cw, "app.someAPI", svc)
	if err != nil {
		t.Fatalf("bindEntry(constant): %v", err)
	}
	if len(names) != 1 || names[0] != "app.someAPI" {
		t.Fatalf("bindEntry(constant) names = %v, want [app.someAPI] (no method expansion)", names)
	}
	if len(cw.bound) != 1 {
		t.Fatalf("bindEntry(constant) bound %d names, want exactly 1; got %v", len(cw.bound), keysOf(cw.bound))
	}
	if got := cw.bound["app.someAPI"]; got != svc {
		t.Fatal("bindEntry(constant) must record the value under its exact name")
	}
}

func TestBindNilWebView(t *testing.T) {
	_, err := bindSingle(nil, "api", func() {})
	if err == nil {
		t.Fatal("Bind() expected error for nil View")
	}
}

func TestBindNilValue(t *testing.T) {
	w := &bindMethodsWebViewStub{}
	_, err := bindSingle(w, "api", nil)
	if err == nil {
		t.Fatal("Bind() expected error for nil value")
	}
}

func TestBindBindError(t *testing.T) {
	w := &bindMethodsWebViewStub{failOn: "sum"}
	names, err := bindSingle(w, "sum", func(a, b int) int { return a + b })
	if err == nil {
		t.Fatal("Bind() expected bind error")
	}
	if len(names) != 0 {
		t.Fatalf("Bind() names len = %d, want 0 (nothing bound on error)", len(names))
	}
	if len(w.bound) != 0 {
		t.Fatalf("Bind() bound %d names on error, want none", len(w.bound))
	}
}

func TestValidateSchemeAllows(t *testing.T) {
	allowed := []string{
		"http://example.com",
		"https://example.com/a?b=c#d",
		"HTTPS://EXAMPLE.COM",
		"mailto:someone@example.com",
		"file:///tmp/report.pdf",
	}
	for _, u := range allowed {
		err := checkURLScheme(u)
		if err != nil {
			t.Errorf("validateScheme(%q) = %v, want nil", u, err)
		}
	}
}

func TestValidateSchemeRejects(t *testing.T) {
	rejected := []string{
		"",
		"example.com",
		"/etc/passwd",
		"ftp://example.com",
		"javascript:alert(1)",
		"vbscript:msgbox(1)",
		"data:text/html,<h1>x",
		"smb://host/share",
	}
	for _, u := range rejected {
		err := checkURLScheme(u)
		if !errors.Is(err, ErrScheme) {
			t.Errorf("validateScheme(%q) = %v, want ErrScheme", u, err)
		}
	}
}

func TestOpenRejectsBadScheme(t *testing.T) {
	app := testApp()
	err := app.Open("javascript:alert(1)")
	if !errors.Is(err, ErrScheme) {
		t.Fatalf("testApp().Open(javascript:) = %v, want ErrScheme", err)
	}
	if app.scope != nil {
		t.Fatal("Open opened the app scope for a disallowed scheme")
	}
}

func testApp() *App { return &App{} }

func fsys(files map[string]string) fs.FS {
	m := fstest.MapFS{}
	for name, content := range files {
		m[name] = &fstest.MapFile{Data: []byte(content)}
	}
	return m
}

func TestServeAppFSNil(t *testing.T) {
	if got := fsContentFunc(nil, nil); got != nil {
		t.Fatalf("serveAppFS(nil) = %v, want nil", got)
	}
	if got := fsContentFunc(fsys(map[string]string{testIndex: "x"}), nil); got == nil {
		t.Fatal("serveAppFS with content = nil, want a resolver")
	}
}

func TestServeAppFSMapping(t *testing.T) {
	serve := fsContentFunc(fsys(map[string]string{
		testIndex: testHTML,
		"app.css": testCSSBody,
	}), nil)
	if resp := serve(&contentRequest{URL: "app://app/app.css"}); resp == nil || string(resp.Body) != testCSSBody || resp.MIME != "text/css; charset=utf-8" {
		t.Fatalf("app.css served as %+v, want body{} css", resp)
	}
	if resp := serve(&contentRequest{URL: "http://localhost:4123/"}); resp == nil || string(resp.Body) != testHTML {
		t.Fatalf("root served as %+v, want the index.html fallback", resp)
	}
	if resp := serve(&contentRequest{URL: "app://app/missing.txt"}); resp != nil {
		t.Fatalf("missing file answered %+v, want nil", resp)
	}
	for _, evil := range []string{"app://app/../secret", "app://app/a/../../secret", "/etc/passwd"} {
		if resp := serve(&contentRequest{URL: evil}); resp != nil {
			t.Fatalf("traversal %q answered %+v, want nil", evil, resp)
		}
	}
}

func TestServeAppFSRendersHTMLTemplates(t *testing.T) {
	app := &App{Name: "templated", Exit: true}
	view := &View{URL: "app://app/page.html", Width: 800, Height: 600}
	view.app = app
	if view.App() != app {
		t.Fatalf("View.App() = %v, want the managing app", view.App())
	}
	if got := (&View{}).App(); got != nil {
		t.Fatalf("unshown View.App() = %v, want nil", got)
	}
	if got := (*View)(nil).App(); got != nil {
		t.Fatalf("nil View.App() = %v, want nil", got)
	}
	const rawHTML = `<script>const t = "{{literal}}";</script>`
	serve := fsContentFunc(fsys(map[string]string{
		"page.html": `<h1>{{.App.Name}}</h1><p>{{.URL}} {{.Width}}x{{.Height}} {{.App.Exit}}</p>`,
		"raw.html":  rawHTML,
		"note.txt":  "{{.App.Name}}",
	}), view)
	req := &contentRequest{URL: "app://app/page.html"}
	resp := serve(req)
	if resp == nil {
		t.Fatal("page.html served nothing")
	}
	if want := "<h1>templated</h1><p>app://app/page.html 800x600 true</p>"; string(resp.Body) != want {
		t.Fatalf("rendered page.html = %q, want %q", resp.Body, want)
	}
	if resp.MIME != "text/html; charset=utf-8" {
		t.Fatalf("rendered page.html MIME = %q, want text/html", resp.MIME)
	}
	if req.View != view {
		t.Fatalf("request.View = %v, want the served view", req.View)
	}
	if resp := serve(&contentRequest{URL: "app://app/raw.html"}); resp == nil || string(resp.Body) != rawHTML {
		t.Fatalf("raw.html = %+v, want it served verbatim", resp)
	}
	if resp := serve(&contentRequest{URL: "app://app/note.txt"}); resp == nil || string(resp.Body) != "{{.App.Name}}" {
		t.Fatalf("note.txt = %+v, want it served verbatim (not HTML)", resp)
	}
	// html/template must escape View data: a name carrying markup can never be
	// injected into the served page (guards against a text/template regression).
	app.Name = `<b>&"x"</b>`
	esc := serve(&contentRequest{URL: "app://app/page.html"})
	if esc == nil || strings.Contains(string(esc.Body), "<b>") || !strings.Contains(string(esc.Body), "&lt;b&gt;") {
		t.Fatalf("rendered page.html = %q, want App.Name HTML-escaped", esc.Body)
	}
}

func TestEmitWithoutBridge(t *testing.T) {
	w := &webview{}
	if err := w.Emit("x"); err == nil {
		t.Fatal("Emit on a view without the events bridge should error")
	}
	if cancel := w.On("x", func(args ...json.RawMessage) {}); cancel == nil {
		t.Fatal("On without the bridge should still return a cancel func")
	}
	w.Off("x")
}

func TestApplyBindsDeterministicOrderAndOverride(t *testing.T) {
	appBinds := map[string]any{
		"zeta":  func() {},
		"alpha": func() {},
		"ghost": nil,
		"mid":   func() {},
	}
	viewBinds := map[string]any{
		"beta":  func() {},
		"mid":   nil,
		"alpha": func() {},
	}
	s := &bindMethodsWebViewStub{}
	if err := applyBindings(s, appBinds, viewBinds); err != nil {
		t.Fatalf("applyBinds: %v", err)
	}
	wantOrder := []string{"alpha", "mid", "zeta", "alpha", "beta"}
	if len(s.bindOrder) != len(wantOrder) {
		t.Fatalf("bind order = %v, want %v", s.bindOrder, wantOrder)
	}
	for i, name := range wantOrder {
		if s.bindOrder[i] != name {
			t.Fatalf("bind order = %v, want %v", s.bindOrder, wantOrder)
		}
	}
	if len(s.unbindCall) != 1 || s.unbindCall[0] != "mid" {
		t.Fatalf("unbind calls = %v, want [mid]", s.unbindCall)
	}
	wantBound := []string{"alpha", "beta", "zeta"}
	got := keysOf(s.bound)
	if len(got) != len(wantBound) {
		t.Fatalf("bound names = %v, want %v", got, wantBound)
	}
	for _, n := range wantBound {
		if _, ok := s.bound[n]; !ok {
			t.Errorf("name %q should be bound; bound: %v", n, got)
		}
	}
	if _, ok := s.bound["mid"]; ok {
		t.Error(`"mid" must be unbound by the nil view entry`)
	}
	if _, ok := s.bound["ghost"]; ok {
		t.Error(`"ghost" (nil app entry) must not be bound`)
	}
}

func TestApplyBindsNilViewEntryWithoutAppBindingIsNoop(t *testing.T) {
	s := &bindMethodsWebViewStub{}
	if err := applyBindings(s, map[string]any{"a": func() {}}, map[string]any{"missing": nil}); err != nil {
		t.Fatalf("applyBinds: %v", err)
	}
	if len(s.unbindCall) != 0 {
		t.Fatalf("unbind calls = %v, want none", s.unbindCall)
	}
}

func TestSortedMapKeys(t *testing.T) {
	m := map[string]int{"b": 2, "a": 1, "c": 3}
	if got, want := sortedKeys(m), []string{"a", "b", "c"}; len(got) != 3 || got[0] != want[0] || got[1] != want[1] || got[2] != want[2] {
		t.Fatalf("sortedMapKeys = %v, want %v", got, want)
	}
}

func TestAutostartSlug(t *testing.T) {
	cases := map[string]string{
		"My App":      "my-app",
		"My  App":     "my--app",
		"my.app_v1-x": "my.app_v1-x",
		"Über-App":    "ber-app",
		"  App  ":     "app",
		"???":         defaultAutostartSlug,
		"UPPER":       "upper",
		"":            defaultAutostartSlug,
	}
	for in, want := range cases {
		if got := slugifyAutostart(in); got != want {
			t.Errorf("autostartSlug(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestValidateAutostartIdentifier(t *testing.T) {
	good := []string{testAppID, "my-app_1.0", "x"}
	for _, id := range good {
		if err := validateAutostartLabel(id); err != nil {
			t.Errorf("validateAutostartIdentifier(%q) = %v, want nil", id, err)
		}
	}
	bad := []string{"has space", "has/slash", "emoji😀", strings.Repeat("a", 201)}
	for _, id := range bad {
		if err := validateAutostartLabel(id); err == nil {
			t.Errorf("validateAutostartIdentifier(%q) = nil, want error", id)
		}
	}
}

func TestAutostartIdentifier(t *testing.T) {
	cases := []struct {
		name string
		cfg  appSetup
		want string
	}{
		{"id wins", appSetup{ID: testAppID, Name: testAppName}, testAppID},
		{"name slug", appSetup{Name: testAppName}, "my-app"},
		{"name slug strips non-ascii", appSetup{Name: "Über App"}, "ber-app"},
	}
	for _, tc := range cases {
		got, err := autostartLabel(tc.cfg)
		if err != nil || got != tc.want {
			t.Errorf("%s: autostartIdentifier(%+v) = %q, %v; want %q", tc.name, tc.cfg, got, err, tc.want)
		}
	}
	if _, err := autostartLabel(appSetup{ID: "bad id"}); err == nil {
		t.Error("autostartIdentifier with an invalid App.ID = nil error, want error")
	}
	id, err := autostartLabel(appSetup{})
	if err != nil || id == "" {
		t.Fatalf("autostartIdentifier(empty cfg) = %q, %v; want a non-empty slug", id, err)
	}
}

func TestAutostartNilSafety(t *testing.T) {
	var a *Autostart
	if a.Enabled() {
		t.Error("nil Autostart: Enabled = true")
	}
	if a.Path() != "" || a.Backend() != "" {
		t.Errorf("nil Autostart: Path = %q, Backend = %q, want empty", a.Path(), a.Backend())
	}
	if err := a.Enable("--flag"); err == nil {
		t.Error("nil Autostart: Enable = nil, want ErrAutostartNotSupported")
	}
	if err := a.Disable(); err == nil {
		t.Error("nil Autostart: Disable = nil, want ErrAutostartNotSupported")
	}
}

func TestValidateTopLevelRejectsReservedAndDenylisted(t *testing.T) {
	bad := []string{
		"close", "open", "name", "fetch", "document", "location",
		"__webview__",
		"__appkit_event__",
		"__appkitWindowDrag",
		"events",
		"close.thing", "name.x",
	}
	for _, name := range bad {
		if err := checkBindTarget(name, "events"); err == nil {
			t.Errorf("validateTopLevel(%q) = nil, want error", name)
		}
	}
	good := []string{
		"demo.close", "demo.theme", "appApi", "closeWindow", "eventsBus",
	}
	for _, name := range good {
		if err := checkBindTarget(name, "events"); err != nil {
			t.Errorf("validateTopLevel(%q) = %v, want nil", name, err)
		}
	}
	if err := checkBindTarget("bus", "bus"); err == nil {
		t.Error(`validateTopLevel("bus", "bus") must reject a binding on the events global`)
	}
	if err := checkBindTarget("events", "bus"); err != nil {
		t.Errorf(`validateTopLevel("events", "bus") = %v, want nil (no clash after rename)`, err)
	}
}

func TestCheckDottedPrefixes(t *testing.T) {
	if err := checkNestedBindNames(map[string]bool{"api": true, "api.id": true}); err == nil {
		t.Fatal("api + api.id must collide")
	}
	if err := checkNestedBindNames(map[string]bool{"app.x": true, "app.x.y": true, "app.x.y.z": true}); err == nil {
		t.Fatal("nested namespace chain must collide")
	}
	if err := checkNestedBindNames(map[string]bool{"api": true}); err != nil {
		t.Fatalf("single name: %v", err)
	}
	if err := checkNestedBindNames(map[string]bool{"a.b": true, "a.c": true, "b": true}); err != nil {
		t.Fatalf("sibling names must not collide: %v", err)
	}
	if err := checkNestedBindNames(map[string]bool{"b": true, "b.a": true}); err == nil {
		t.Fatal("b + b.a must collide")
	}
	if err := checkNestedBindNames(map[string]bool{"api": true, "apix": true}); err != nil {
		t.Fatalf("apix is a different leaf, not a nested name: %v", err)
	}
}

func TestApplyBindsRejectsInvalidPlans(t *testing.T) {
	w := &bindMethodsWebViewStub{}
	cases := []struct {
		name      string
		app, view map[string]any
	}{
		{"prefix collision across maps", map[string]any{"api": func() {}}, map[string]any{"api.id": func() {}}},
		{"prefix collision within one map", nil, map[string]any{"demo": func() {}, "demo.theme": func() {}}},
		{"reserved name", nil, map[string]any{"__appkit_event__": func() {}}},
		{"denylisted top level", nil, map[string]any{"name": func() {}}},
		{"events global", nil, map[string]any{"events": func() {}}},
		{"bad segments", nil, map[string]any{"a..b": func() {}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := applyBindings(w, tc.app, tc.view); err == nil {
				t.Fatal("applyBinds must reject the plan")
			}
			if len(w.bindOrder) != 0 {
				t.Fatalf("nothing must be bound on a rejected plan, bound: %v", w.bindOrder)
			}
		})
	}
	if err := applyBindings(w, map[string]any{"api": func() {}}, map[string]any{"api": func() {}}); err != nil {
		t.Fatalf("same-name override must be allowed: %v", err)
	}
}

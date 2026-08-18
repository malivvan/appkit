package appkit

import (
	"encoding/json"
	"os/exec"
	"strings"
	"testing"
	"unsafe"
)

func TestAppRegionsIsDrag(t *testing.T) {
	rs := appRegionSet{
		Drag:   []appRegion{{X: 0, Y: 0, W: 400, H: 40}},
		NoDrag: []appRegion{{X: 350, Y: 5, W: 40, H: 30}},
	}
	tests := []struct {
		name string
		x, y float64
		want bool
	}{
		{name: "inside drag box", x: 10, y: 10, want: true},
		{name: "inside no-drag box", x: 360, y: 10, want: false},
		{name: "outside both", x: 10, y: 100, want: false},
		{name: "drag boundary left edge", x: 0, y: 10, want: true},
		{name: "drag boundary right edge", x: 400, y: 10, want: false},
		{name: "no-drag precedence at corner", x: 350, y: 5, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := rs.isDrag(tt.x, tt.y); got != tt.want {
				t.Fatalf("isDrag(%v, %v) = %v, want %v", tt.x, tt.y, got, tt.want)
			}
		})
	}
	if (appRegionSet{}).isDrag(1, 1) {
		t.Fatal("empty region set must not be draggable")
	}
}

func TestParseAppRegions(t *testing.T) {
	raw := json.RawMessage(`[{"drag":[{"x":1,"y":2,"w":3,"h":4}],"noDrag":[{"x":5,"y":6,"w":7,"h":8}]}]`)
	rs := parseAppRegionSet(raw)
	if len(rs.Drag) != 1 || rs.Drag[0].X != 1 || rs.Drag[0].Y != 2 || rs.Drag[0].W != 3 || rs.Drag[0].H != 4 {
		t.Fatalf("drag regions = %+v", rs.Drag)
	}
	if len(rs.NoDrag) != 1 || rs.NoDrag[0].X != 5 {
		t.Fatalf("no-drag regions = %+v", rs.NoDrag)
	}

	for _, bad := range []json.RawMessage{json.RawMessage(`{}`), json.RawMessage(`[]`), json.RawMessage(`not json`)} {
		if rs := parseAppRegionSet(bad); !rs.empty() {
			t.Fatalf("parseAppRegionSet(%s) = %+v, want empty", bad, rs)
		}
	}
}

func TestParseDragRequest(t *testing.T) {
	raw := json.RawMessage(`[{"direction":"se","button":0,"screenX":100,"screenY":200,"clientX":10.5,"clientY":20.5,"time":1234}]`)
	p := parseDragRequest(raw)
	if p.Direction != "se" || p.Button != 0 || p.ScreenX != 100 || p.ScreenY != 200 ||
		p.ClientX != 10.5 || p.ClientY != 20.5 || p.Time != 1234 {
		t.Fatalf("parseDragRequest = %+v", p)
	}
	if p := parseDragRequest(json.RawMessage(`[]`)); p.Direction != "" {
		t.Fatalf("parseDragRequest([]) = %+v, want zero", p)
	}
}

func TestGdkEdgeFor(t *testing.T) {
	tests := map[string]int32{
		"nw": gdkEdgeNorthWest, "n": gdkEdgeNorth, "ne": gdkEdgeNorthEast,
		"w": gdkEdgeWest, "e": gdkEdgeEast, "sw": gdkEdgeSouthWest,
		"s": gdkEdgeSouth, "se": gdkEdgeSouthEast,
	}
	for dir, want := range tests {
		if got := gdkEdgeFor(dir); got != want {
			t.Fatalf("gdkEdgeFor(%q) = %d, want %d", dir, got, want)
		}
	}
	if got := gdkEdgeFor("bogus"); got != -1 {
		t.Fatalf("gdkEdgeFor(bogus) = %d, want -1", got)
	}
}

func TestCreateAppRegionScriptContent(t *testing.T) {
	script := buildRegionScript(true, true, "windows")
	for _, want := range []string{
		"-app-region", "-webview-app-region", "-webkit-app-region", "drag", "no-drag",
		`"windows"`, methodAppRegions, methodWindowDrag, methodWindowResize,
		methodWindowMaximize,
	} {
		if !strings.Contains(script, want) {
			t.Fatalf("script does not contain %q", want)
		}
	}
	if !strings.Contains(script, "var RESIZABLE = true") {
		t.Fatal("script should bake in RESIZABLE = true")
	}
	if !strings.Contains(script, "var POST_REGIONS = true") {
		t.Fatal("script should bake in POST_REGIONS = true")
	}
	fixed := buildRegionScript(false, false, "darwin")
	if !strings.Contains(fixed, "var RESIZABLE = false") || !strings.Contains(fixed, "var POST_REGIONS = false") {
		t.Fatal("script should bake in RESIZABLE/POST_REGIONS = false")
	}
}

func TestAppRegionScriptBehavior(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not available")
	}
	script := buildRegionScript(true, false, "unix")

	harness := `
const listeners = {};
const posts = [];
const styleEl = {
  textContent: '.titlebar { -user-select: none; -app-region: drag; }\n' +
               '.titlebar-button { -app-region: no-drag; }'
};
const titlebarEl = { getBoundingClientRect: function() {
  return { left: 0, top: 0, width: 400, height: 40, right: 400, bottom: 40 };
} };
const buttonEl = { getBoundingClientRect: function() {
  return { left: 350, top: 5, width: 40, height: 30, right: 390, bottom: 35 };
} };
global.window = {
  __webview__: { post: function(msg) { posts.push(JSON.parse(msg)); } },
  innerWidth: 400, innerHeight: 300, devicePixelRatio: 1,
  addEventListener: function(type, fn) {
    (listeners[type] = listeners[type] || []).push(fn);
  },
  requestAnimationFrame: function(fn) { if (frames++ < 5) { fn(); } },
  setInterval: function() { return 0; },
};
let frames = 0;
global.document = {
  documentElement: { contains: function() { return true; } },
  head: { appendChild: function() {} },
  createElement: function() { return { id: '', textContent: '' }; },
  querySelectorAll: function(sel) {
    if (sel === 'style') { return [styleEl]; }
    if (sel === 'link[rel~="stylesheet"]') { return []; }
    if (sel === '[style]') { return []; }
    if (sel === '.titlebar') { return [titlebarEl]; }
    if (sel === '.titlebar-button') { return [buttonEl]; }
    return [];
  },
  addEventListener: function(type, fn) {
    (listeners[type] = listeners[type] || []).push(fn);
  },
  fonts: null,
};

` + script + `

function mouse(x, y, t, button) {
  return {
    clientX: x, clientY: y, screenX: x + 50, screenY: y + 60,
    button: (button === undefined ? 0 : button),
    timeStamp: (t === undefined ? 123 : t),
    preventDefault: function() {}, stopPropagation: function() {}
  };
}
function assert(cond, msg) {
  if (!cond) { console.error('ASSERT: ' + msg); process.exit(1); }
}
listeners['mousedown'][0](mouse(10, 10));            // inside .titlebar (drag)
assert(posts.length === 1, 'drag mousedown posted once, got ' + posts.length);
assert(posts[0].method === '__appkitWindowDrag', 'drag method, got ' + posts[0].method);
assert(posts[0].params[0].button === 0, 'drag button 0');

listeners['mousedown'][0](mouse(360, 10));           // inside .titlebar-button (no-drag)
assert(posts.length === 1, 'no-drag mousedown must not post, got ' + posts.length);

listeners['mousedown'][0](mouse(2, 150));            // left edge of resizable window
assert(posts.length === 2, 'edge mousedown posted, got ' + posts.length);
assert(posts[1].method === '__appkitWindowResize', 'resize method, got ' + posts[1].method);
assert(posts[1].params[0].direction === 'w', 'resize direction w, got ' + posts[1].params[0].direction);

listeners['mousedown'][0](mouse(100, 100));          // plain content
assert(posts.length === 2, 'content mousedown must not post, got ' + posts.length);

// Double-click on a drag box: the first click drags, the second (close in
// time and space) posts the maximize toggle and NOT a second drag request.
listeners['mousedown'][0](mouse(150, 20, 1000));
assert(posts.length === 3, 'double-click first mousedown posted, got ' + posts.length);
assert(posts[2].method === '__appkitWindowDrag', 'first click of a pair drags, got ' + posts[2].method);
listeners['mousedown'][0](mouse(150, 20, 1250));     // same spot, 250 ms later
assert(posts.length === 4, 'double-click second mousedown posted, got ' + posts.length);
assert(posts[3].method === '__appkitWindowToggleMaximize', 'second click toggles, got ' + posts[3].method);
assert(posts[3].params === undefined, 'toggle carries no params payload');

// The pending pair was consumed, so a fast third click drags instead of
// firing a second toggle.
listeners['mousedown'][0](mouse(151, 20, 1300));
assert(posts.length === 5 && posts[4].method === '__appkitWindowDrag',
  'fast third click drags (pair consumed), got ' + posts[4].method);

// A slow second click (600 ms later) is a plain drag, not a toggle.
listeners['mousedown'][0](mouse(200, 20, 2000));
listeners['mousedown'][0](mouse(200, 20, 2600));
assert(posts.length === 7 && posts[6].method === '__appkitWindowDrag',
  'slow second click drags, got ' + posts[6].method);

// A far second click (100 ms later but 30 px away) is a plain drag too.
listeners['mousedown'][0](mouse(250, 20, 3000));
listeners['mousedown'][0](mouse(280, 20, 3100));
assert(posts.length === 9 && posts[8].method === '__appkitWindowDrag',
  'far second click drags, got ' + posts[8].method);
console.log('PASS');
`

	cmd := exec.Command(node, "-")
	cmd.Stdin = strings.NewReader(harness)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("node harness failed: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "PASS") {
		t.Fatalf("unexpected harness output: %s", out)
	}
}

func TestAppRegionScriptPlatformIdentifiers(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not available")
	}
	tests := []struct {
		platform string
		edge     string
		hover    string
	}{
		{"unix", methodWindowResize, ""},
		{"linux", methodWindowResize, ""},
		{"freebsd", methodWindowResize, ""},
		{"netbsd", methodWindowResize, ""},
		{"windows", methodWindowResize, ""},
		{"darwin", "", methodWindowCursor},
	}
	for _, tc := range tests {
		t.Run(tc.platform, func(t *testing.T) {
			harness := `
const listeners = {};
const posts = [];
global.window = {
  __webview__: { post: function(msg) { posts.push(JSON.parse(msg)); } },
  innerWidth: 400, innerHeight: 300, devicePixelRatio: 1,
  addEventListener: function(type, fn) {
    (listeners[type] = listeners[type] || []).push(fn);
  },
  requestAnimationFrame: function() {},
  setInterval: function() { return 0; },
};
global.document = {
  documentElement: { contains: function() { return true; } },
  head: { appendChild: function() {} },
  createElement: function() { return { id: '', textContent: '' }; },
  querySelectorAll: function() { return []; },
  addEventListener: function(type, fn) {
    (listeners[type] = listeners[type] || []).push(fn);
  },
};

` + buildRegionScript(true, false, tc.platform) + `

function mouse(x, y) {
  return {
    clientX: x, clientY: y, screenX: x + 50, screenY: y + 60, button: 0,
    timeStamp: 123, preventDefault: function() {}, stopPropagation: function() {}
  };
}
function method(list) { return list.length ? list[0].method : ''; }
listeners['mousedown'][0](mouse(2, 150));            // left edge band of a resizable window
const edge = method(posts);
posts.length = 0;
// The hover listener must be wired for every platform here (all are
// resizable), and only macOS answers a hover with a native cursor.
if (!listeners['mousemove'] || listeners['mousemove'].length === 0) {
  console.error('ASSERT: no mousemove listener registered');
  process.exit(1);
}
listeners['mousemove'][0](mouse(2, 150));
console.log('edge=' + edge + ' hover=' + method(posts));
`
			cmd := exec.Command(node, "-")
			cmd.Stdin = strings.NewReader(harness)
			out, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("node harness failed: %v\n%s", err, out)
			}
			want := "edge=" + tc.edge + " hover=" + tc.hover
			if got := strings.TrimSpace(string(out)); got != want {
				t.Fatalf("tracker for platform %q: got %q, want %q", tc.platform, got, want)
			}
		})
	}
}

func TestBridgeScriptsBehavior(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not available")
	}
	eventsJS := buildEventsScript("events")
	acmeJS := buildEventsScript("acme")
	bindJS := buildInitScript("function(m) { posts.push(JSON.parse(m)); }")

	harness := `
const posts = [];
const goEvents = [];
global.window = {
  crypto: { getRandomValues: function(a) { for (let i = 0; i < a.length; i++) { a[i] = i + 1; } return a; } },
  __appkit_event__: function(name, args) {
    goEvents.push([name, args]);
    return Promise.resolve();
  },
};
function assert(cond, msg) {
  if (!cond) { console.error('ASSERT: ' + msg); process.exit(1); }
}

` + eventsJS + `

// The events API IS the named global: on/off/emit live on window.events, no
// nested .events member remains.
assert(typeof window.events === 'object' && typeof window.events.on === 'function',
  'window.events.on installed');
assert(window.events.events === undefined, 'no nested .events member');
assert(typeof window.events.emit === 'function' && typeof window.events.off === 'function',
  'events on/off/emit present');

// Re-running the script is a no-op (idempotence guard), so a listener
// registered between runs survives.
let got = null;
window.events.on('greet', function(msg) { got = msg; });
` + eventsJS + `
window.events.emit('greet', 'hi');
assert(got === 'hi', 'local listener fired across re-install, got ' + got);
assert(goEvents.length === 1 && goEvents[0][0] === 'greet' && goEvents[0][1][0] === 'hi',
  'JS emit forwarded to Go, got ' + JSON.stringify(goEvents));

// A second configured name installs its own global without touching the first.
` + acmeJS + `
assert(typeof window.acme.on === 'function' && typeof window.events.on === 'function',
  'custom global window.acme coexists with window.events');

` + bindJS + `

// Flat names bind at the top level as before.
window.__webview__.onBind('demoAdd');
assert(typeof window.demoAdd === 'function', 'flat name bound at window.demoAdd');

// Dotted names nest under window.
window.__webview__.onBind('app.someAPI.call');
assert(typeof window.app === 'object' && typeof window.app.someAPI === 'object',
  'dotted path created intermediate objects');
assert(typeof window.app.someAPI.call === 'function', 'dotted name bound at window.app.someAPI.call');

window.app.someAPI.call(2, 3);
assert(posts.length === 1 && posts[0].method === 'app.someAPI.call' &&
  posts[0].params[0] === 2 && posts[0].params[1] === 3,
  'calling the nested function posts the dotted method name, got ' + JSON.stringify(posts));

// Unbind removes exactly the leaf.
window.__webview__.onUnbind('app.someAPI.call');
assert(window.app.someAPI.call === undefined, 'leaf removed by onUnbind');
assert(typeof window.app.someAPI === 'object', 'parent namespace survives onUnbind');

// Unbinding a name that is not bound throws (parity with the flat case).
let threw = false;
try { window.__webview__.onUnbind('app.someAPI.call'); } catch (e) { threw = true; }
assert(threw, 'second onUnbind throws');
try { window.__webview__.onUnbind('demoAdd'); } catch (e) { threw = true; }
assert(window.demoAdd === undefined && threw, 'flat unbind removes the global and re-unbind throws');

// Constants bind through onBindValue as ONE value installed from its raw
// JSON at the (possibly dotted) name.
window.__webview__.onBindValue('cfg.levels', JSON.stringify([{ name: 'one' }, { name: 'two' }]));
assert(typeof window.cfg === 'object' && Array.isArray(window.cfg.levels) &&
  window.cfg.levels[1].name === 'two', 'constant bound at dotted path from raw JSON');
window.__webview__.onBindValue('flatConst', JSON.stringify(42));
assert(window.flatConst === 42, 'scalar constant bound at the top level');

(async function() {
  // An accessor pair binds as a readable/writable property: reading runs the
  // Go getter over the bridge and yields a Promise, assigning runs the
  // setter with the assigned value.
  window.__webview__.onBindAccessor('app.size', 'app.size\x00get', 'app.size\x00set');
  const getDesc = Object.getOwnPropertyDescriptor(window.app, 'size');
  assert(typeof getDesc.get === 'function' && typeof getDesc.set === 'function',
    'accessor property installed on the namespace');

  const read = getDesc.get();
  assert(read && typeof read.then === 'function', 'property read returns a promise');
  let m = posts[posts.length - 1];
  assert(m.method === 'app.size\x00get' && m.params.length === 0, 'getter dispatched with no arguments');
  window.__webview__.onReply(m.id, 0, JSON.stringify('7px'));
  assert((await read) === '7px', 'awaiting a property read fetches the getter value');

  getDesc.set('9px');
  m = posts[posts.length - 1];
  assert(m.method === 'app.size\x00set' && m.params[0] === '9px', 'assignment dispatches the setter');
  window.__webview__.onReply(m.id, 0, JSON.stringify(null));

  // A plain bound function doubles as a getter: await window.name calls it
  // with no arguments and resolves to its result; calling it still works.
  window.__webview__.onBind('clock');
  const tick = (async () => await window.clock)(); // .then runs on a microtask
  setTimeout(function() {
    const c = posts[posts.length - 1];
    window.__webview__.onReply(c.id, 0, JSON.stringify(1234));
  }, 0);
  assert((await tick) === 1234, 'await window.clock resolves the no-arg call result');
  assert(typeof window.clock === 'function', 'the bound name stays callable');

  // freezeBinds ends the binding process: every object the batch created -
  // namespace containers, callable wrappers and the whole constant tree - is
  // frozen, while the global object itself is never frozen. Accessor
  // properties stay live on their (frozen) namespace.
  window.__webview__.freezeBinds();
  assert(Object.isFrozen(window.clock), 'function wrapper frozen');
  assert(Object.isFrozen(window.app) && Object.isFrozen(window.app.someAPI), 'namespace objects frozen');
  assert(Object.isFrozen(window.cfg) && Object.isFrozen(window.cfg.levels) &&
    Object.isFrozen(window.cfg.levels[0]) && Object.isFrozen(window.cfg.levels[1]),
    'constant tree frozen');
  window.cfg.levels[1].name = 'mutated'; // silent no-op outside strict mode
  assert(window.cfg.levels[1].name === 'two', 'frozen constant ignores mutation');
  assert(!Object.isFrozen(window), 'window/globalThis itself is never frozen');
  let sealed = false;
  try { window.__webview__.onBind('app.someAPI.extra'); } catch (e) { sealed = true; }
  assert(sealed, 'adding a name under an already-sealed namespace throws');

  const after = getDesc.get();
  const am = posts[posts.length - 1];
  window.__webview__.onReply(am.id, 0, JSON.stringify('frozen-yet-live'));
  assert((await after) === 'frozen-yet-live', 'accessor reads keep working after the namespace froze');

  // One-sided accessors: an empty key means the side is not exposed. A
  // getter-only accessor has a getter but no setter; a setter-only accessor
  // has a setter but no getter (reads yield undefined). They live on window
  // itself (never frozen) - namespaces are sealed by now.
  window.__webview__.onBindAccessor('roProp', 'roProp\x00get', '');
  const roDesc = Object.getOwnPropertyDescriptor(window, 'roProp');
  assert(typeof roDesc.get === 'function' && roDesc.set === undefined,
    'getter-only accessor exposes get without set');
  window.__webview__.onBindAccessor('woProp', '', 'woProp\x00set');
  const woDesc = Object.getOwnPropertyDescriptor(window, 'woProp');
  assert(woDesc.get === undefined && typeof woDesc.set === 'function',
    'setter-only accessor exposes set without get');
  let woRead;
  try { woRead = window.woProp; } catch (e) { woRead = 'threw'; }
  assert(woRead === undefined, 'reading a setter-only accessor yields undefined, got ' + woRead);

  // Callable setter: a one-argument function is both a function and a
  // writable variable - reading yields the callable, assigning runs it with
  // the assigned value and resolves to the result.
  window.__webview__.onBindSetter('logger');
  const logDesc = Object.getOwnPropertyDescriptor(window, 'logger');
  assert(typeof logDesc.get === 'function' && typeof logDesc.set === 'function',
    'callable setter installed with get+set');
  const called = logDesc.get()('via call');
  let lm = posts[posts.length - 1];
  assert(lm.method === 'logger' && lm.params[0] === 'via call', 'calling the setter dispatches it');
  window.__webview__.onReply(lm.id, 0, JSON.stringify('called:via call'));
  assert((await called) === 'called:via call', 'call result resolved');
  const assigned = logDesc.set('via assign'); // what window.logger = v runs
  lm = posts[posts.length - 1];
  assert(lm.method === 'logger' && lm.params[0] === 'via assign', 'assignment dispatches the setter');
  window.__webview__.onReply(lm.id, 0, JSON.stringify('assigned:via assign'));
  assert((await assigned) === 'assigned:via assign', 'assignment resolves to the setter result');
  assert(typeof window.logger === 'function', 'the name still reads as a callable function');

  // A REAL assignment expression yields the ASSIGNED VALUE (ECMAScript
  // discards the setter's result - the demo selftest asserts exactly this),
  // while the Go call still fires.
  const exprVal = (window.logger = 'expr');
  assert(exprVal === 'expr', 'assignment expression = ' + exprVal);
  lm = posts[posts.length - 1];
  assert(lm.method === 'logger' && lm.params[0] === 'expr', 'assignment still dispatched the setter');

  console.log('PASS');
})().catch(function(e) {
  console.error('ASSERT: ' + (e && e.message ? e.message : e));
  process.exit(1);
});
`
	cmd := exec.Command(node, "-")
	cmd.Stdin = strings.NewReader(harness)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("node harness failed: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "PASS") {
		t.Fatalf("unexpected harness output: %s", out)
	}
}

func TestViewNotSpawnedGuards(t *testing.T) {
	v := &View{}
	if err := v.Emit("x"); err == nil {
		t.Fatal("Emit before Show should return a not-shown error")
	}
	cancel := v.On("x", nil)
	if cancel == nil {
		t.Fatal("On before Show should return a no-op cancel, not nil")
	}
	cancel()
	v.Off("x")
	v.Close()
	if v.w != nil {
		t.Fatal("Close before Show must leave the engine handle nil")
	}
	if v.window != nil {
		t.Fatal("window handle before Show must stay nil")
	}
	func() {
		defer func() {
			if recover() == nil {
				t.Fatal("void View method before Show should panic with a clear message")
			}
		}()
		v.Navigate("about:blank")
	}()
	func() {
		defer func() {
			if recover() == nil {
				t.Fatal("View.Window before Show should panic with a clear message")
			}
		}()
		v.Window(func(unsafe.Pointer) {})
	}()
}

func TestViewShowAgainReveals(t *testing.T) {
	app := &App{}
	v := &View{w: &webview{}}
	if err := app.Show(v); err != nil {
		t.Fatalf("Show on a live window should reveal it, got error: %v", err)
	}
	if err := app.Show(nil); err == nil {
		t.Fatal("Show(nil) should be rejected")
	}
}

func TestViewMaximizedUnshown(t *testing.T) {
	var nilView *View
	if nilView.Maximized() {
		t.Error("nil View.Maximized() = true, want false")
	}
	if (&View{}).Maximized() {
		t.Error("unshown View.Maximized() = true, want false")
	}
}

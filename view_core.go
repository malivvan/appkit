package appkit

import (
	"encoding/json"
	"errors"
	"fmt"
	"sync/atomic"
	"unsafe"

	"github.com/malivvan/appkit/dialog"
)

const (
	methodAppRegions     = "__appkitAppRegions"
	methodWindowDrag     = "__appkitWindowDrag"
	methodWindowResize   = "__appkitWindowResize"
	methodWindowCursor   = "__appkitWindowCursor"
	methodWindowMaximize = "__appkitWindowToggleMaximize"
	methodBindError      = "__appkitBindError"
)

const (
	gdkEdgeNorthWest = 0
	gdkEdgeNorth     = 1
	gdkEdgeNorthEast = 2
	gdkEdgeWest      = 3
	gdkEdgeEast      = 4
	gdkEdgeSouthWest = 5
	gdkEdgeSouth     = 6
	gdkEdgeSouthEast = 7
)

type appRegion struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
	W float64 `json:"w"`
	H float64 `json:"h"`
}

type appRegionSet struct {
	Drag   []appRegion `json:"drag"`
	NoDrag []appRegion `json:"noDrag"`
}

func (r appRegion) contains(x, y float64) bool {
	return x >= r.X && x < r.X+r.W && y >= r.Y && y < r.Y+r.H
}

func (rs appRegionSet) empty() bool { return len(rs.Drag) == 0 && len(rs.NoDrag) == 0 }

func (rs appRegionSet) isDrag(x, y float64) bool {
	for _, r := range rs.NoDrag {
		if r.contains(x, y) {
			return false
		}
	}
	for _, r := range rs.Drag {
		if r.contains(x, y) {
			return true
		}
	}
	return false
}

type dragRequest struct {
	Direction string  `json:"direction"`
	Button    int32   `json:"button"`
	ScreenX   int32   `json:"screenX"`
	ScreenY   int32   `json:"screenY"`
	ClientX   float64 `json:"clientX"`
	ClientY   float64 `json:"clientY"`
	Time      uint32  `json:"time"`
}

func decodeFirstParam(params json.RawMessage, dst any) bool {
	var arr []json.RawMessage
	if err := json.Unmarshal(params, &arr); err != nil || len(arr) == 0 {
		return false
	}
	return json.Unmarshal(arr[0], dst) == nil
}

func parseAppRegionSet(params json.RawMessage) appRegionSet {
	var rs appRegionSet
	decodeFirstParam(params, &rs)
	return rs
}

func parseDragRequest(params json.RawMessage) dragRequest {
	var p dragRequest
	decodeFirstParam(params, &p)
	return p
}

func gdkEdgeFor(direction string) int32 {
	switch direction {
	case "nw":
		return gdkEdgeNorthWest
	case "n":
		return gdkEdgeNorth
	case "ne":
		return gdkEdgeNorthEast
	case "w":
		return gdkEdgeWest
	case "e":
		return gdkEdgeEast
	case "sw":
		return gdkEdgeSouthWest
	case "s":
		return gdkEdgeSouth
	case "se":
		return gdkEdgeSouthEast
	}
	return -1
}

const appRegionScriptTmpl = `(function() {
  'use strict';

  var PLATFORM = %s;
  // The GTK backend spans Linux, FreeBSD and NetBSD, so its identifier is the
  // umbrella 'unix' - but the GOOS names it covers are accepted too, so the
  // tracker behaves identically whichever spelling the native side injects.
  var UNIX = PLATFORM === 'unix' || PLATFORM === 'linux' ||
             PLATFORM === 'netbsd' || PLATFORM === 'freebsd';
  var RESIZABLE = %v;
  var POST_REGIONS = %v;
  var EDGE = 6; // css px band at window edges treated as a resize handle
  var PROP = /(?:-app-region|-webview-app-region|-webkit-app-region)\s*:\s*(drag|no-drag)/gi;

  var elMap = new Map();    // element -> 'drag' | 'no-drag' (last rule wins)
  var dragRects = [];
  var noDragRects = [];
  var lastSig = '';
  var dirty = true;         // styles may have changed -> rescan selectors (initial scan too)
  var fetchedTexts = {};    // stylesheet href -> raw css text (fetched once)
  var fetching = {};        // stylesheet href -> true while a fetch is in flight
  var lastEdge = '';
  var cursorStyle = null;
  var dpr = window.devicePixelRatio || 1;
  var started = false;      // the rAF loop is armed once the bridge is present
  var lastClick = null;     // {x, y, t} of the previous left mouse-down in a drag box

  // The injected bridge (window.__webview__) may not be installed yet at
  // document-start on every backend/WebView2 runtime. Unlike an object shape
  // with a post() but no bridge, or no bridge at all, we must not bail out
  // permanently: instead wait until the bridge appears and then start tracking.
  function bridge() {
    return (window.__webview__ && typeof window.__webview__.post === 'function') ? window.__webview__ : null;
  }

  function propValue(decls) {
    var value = '';
    PROP.lastIndex = 0;
    var m;
    while ((m = PROP.exec(decls)) !== null) { value = m[1]; }
    return value;
  }

  // Split raw css text into {selector, decls} pairs, skipping at-rule
  // wrappers (@media / @supports: the nested rule's selector is what we
  // keep).
  function parseRules(text) {
    var out = [];
    var depth = 0, start = 0, selStart = 0, selEnd = 0;
    for (var i = 0; i < text.length; i++) {
      var ch = text.charAt(i);
      if (ch === '{') {
        selStart = start; selEnd = i;
        start = i + 1;
        depth++;
      } else if (ch === '}') {
        depth--;
        if (depth === 0) {
          out.push({ selector: text.slice(selStart, selEnd).trim(), decls: text.slice(start, i) });
          start = i + 1;
        }
      }
    }
    return out;
  }

  function applyRule(selectorList, value) {
    var sels = selectorList.split(',');
    for (var i = 0; i < sels.length; i++) {
      var sel = sels[i].trim();
      if (!sel || sel.charAt(0) === '@') { continue; }
      var matches;
      try { matches = document.querySelectorAll(sel); } catch (e) { continue; }
      for (var j = 0; j < matches.length; j++) { elMap.set(matches[j], value); }
    }
  }

  function addStyleSource(text) {
    // Strip comments first: parseRules splits on raw braces and would
    // otherwise merge a trailing /* comment */ into the next rule's selector
    // (making "#titlebar" unqueryable and silently dropping the drag box).
    text = String(text).replace(/\/\*[\s\S]*?\*\//g, '');
    var parsed = parseRules(text);
    for (var i = 0; i < parsed.length; i++) {
      var value = propValue(parsed[i].decls);
      if (value) { applyRule(parsed[i].selector, value); }
    }
  }

  function scanStyles() {
    // elMap is rebuilt from scratch on every scan, so every source must be
    // re-applied SYNCHRONOUSLY within the same scan: fetched stylesheets are
    // cached as raw text and replayed here, never applied from an async
    // callback that a later scan could wipe before collectRects runs.
    elMap = new Map();

    var styleEls = document.querySelectorAll('style');
    for (var i = 0; i < styleEls.length; i++) { addStyleSource(styleEls[i].textContent); }

    // External stylesheets are fetched as raw text - the CSSOM drops the
    // unknown -app-region property, so cssRules cannot be used. A
    // fetch that fails or hangs is retried on later scans (fetching[] is
    // cleared on settle), so a transient early-load failure self-heals.
    var linkEls = document.querySelectorAll('link[rel~="stylesheet"]');
    for (var j = 0; j < linkEls.length; j++) {
      var href = linkEls[j].href;
      if (!href || href.indexOf('file:') === 0) { continue; }
      if (fetchedTexts[href]) {
        addStyleSource(fetchedTexts[href]);
        continue;
      }
      if (fetching[href]) { continue; }
      fetching[href] = true;
      (function(href) {
        fetch(href).then(function(r) { return r.text(); }).then(function(text) {
          fetchedTexts[href] = text;
          delete fetching[href];
          dirty = true;
        }).catch(function() { delete fetching[href]; }); // next scan retries
      })(href);
    }

  // Inline style attributes (highest precedence, applied last).
    var styled = document.querySelectorAll('[style]');
    for (var k = 0; k < styled.length; k++) {
      var v = propValue(styled[k].getAttribute('style') || '');
      if (v) { elMap.set(styled[k], v); }
    }
  }

  function collectRects() {
    var drag = [], noDrag = [];
    var changed = false;
    elMap.forEach(function(value, el) {
      if (!document.documentElement || !document.documentElement.contains(el)) {
        elMap.delete(el);
        changed = true;
        return;
      }
      var rect = el.getBoundingClientRect();
      if (rect.width > 0 && rect.height > 0 &&
          rect.right > 0 && rect.left < window.innerWidth &&
          rect.bottom > 0 && rect.top < window.innerHeight) {
        var box = {
          x: rect.left * dpr,
          y: rect.top * dpr,
          w: rect.width * dpr,
          h: rect.height * dpr
        };
        if (value === 'drag') { drag.push(box); } else { noDrag.push(box); }
      }
    });
    dragRects = drag;
    noDragRects = noDrag;
    return changed;
  }

  function postRegions() {
    var sig = JSON.stringify({ drag: dragRects, noDrag: noDragRects });
    if (sig === lastSig) { return; }
    lastSig = sig;
    if (POST_REGIONS) {
      var b = bridge();
      if (b) {
        b.post(JSON.stringify({
          method: %q,
          params: [{ drag: dragRects, noDrag: noDragRects }]
        }));
      }
    }
  }

  function update() {
    if (dirty) { dirty = false; scanStyles(); }
    if (collectRects()) { lastSig = ''; }
    postRegions();
  }

  function inRects(x, y, rects) {
    for (var i = 0; i < rects.length; i++) {
      if (x >= rects[i].x && x < rects[i].x + rects[i].w &&
          y >= rects[i].y && y < rects[i].y + rects[i].h) { return true; }
    }
    return false;
  }

  function edgeAt(x, y) {
    var left = x <= EDGE, right = x >= window.innerWidth - EDGE;
    var top = y <= EDGE, bottom = y >= window.innerHeight - EDGE;
    if (top && left) { return 'nw'; }
    if (top && right) { return 'ne'; }
    if (bottom && left) { return 'sw'; }
    if (bottom && right) { return 'se'; }
    if (top) { return 'n'; }
    if (bottom) { return 's'; }
    if (left) { return 'w'; }
    if (right) { return 'e'; }
    return '';
  }

  function edgeCursor(edge) {
    switch (edge) {
      case 'n': case 's': return 'ns-resize';
      case 'e': case 'w': return 'ew-resize';
      case 'nw': case 'se': return 'nwse-resize';
      case 'ne': case 'sw': return 'nesw-resize';
    }
    return '';
  }

  function setCursor(edge) {
    if (edge === lastEdge) { return; }
    lastEdge = edge;
    if (!cursorStyle && document.head) {
      cursorStyle = document.createElement('style');
      cursorStyle.id = '__appkit_appregion_cursor';
      document.head.appendChild(cursorStyle);
    }
    if (cursorStyle) {
      cursorStyle.textContent = edge ? ('* { cursor: ' + edgeCursor(edge) + ' !important; }') : '';
    }
    // macOS WKWebView manages its own cursor in the web process and, unlike
    // WebKitGTK / WebView2, will not always honour a dynamic page-wide CSS
    // cursor over live web content (it can revert to the arrow even where
    // * { cursor: … } applies). When an edge/corner is hovered we therefore
    // ALSO send the edge to the engine so it can push AppKit's resize cursor
    // on the UI thread as a native override. Only fires when the edge actually
    // changes (guarded above), so it is cheap.
    if (PLATFORM === 'darwin') {
      post({ method: %q, params: [{ edge: edge }] });
    }
  }

  function post(obj) {
    var b = bridge();
    if (b) { b.post(JSON.stringify(obj)); }
  }

  // Native-side resizability is fixed at creation (View.State); the
  // backends push the initial value here once the tracker exists.
  function installBridgeAPI(b) {
    b.onAppRegionState = function(state) {
      if (state && typeof state.resizable === 'boolean') {
        RESIZABLE = state.resizable;
        if (!RESIZABLE) { setCursor(''); }
      }
    };
  }

  function boot() {
    var b = bridge();
    if (!b) { return; }
    installBridgeAPI(b);
    if (started) { return; }
    started = true;

  // The tracking loop and the initial scan are the critical part and must
  // always come up, so wire them FIRST, before anything that could throw on
  // a document that is still being parsed at document-start (e.g. a null
  // document.documentElement).
    (function loop() {
      update();
      window.requestAnimationFrame(loop);
    })();
  // A slow timer re-scans even if the fast loop's scans ran before the page
  // finished building its DOM/styles (document-start runs early). Forcing
  // dirty here guarantees late-appearing drag boxes are picked up even if
  // the 'load'/'resize' events are missed.
    window.setInterval(function() { dirty = true; }, 300);

    window.addEventListener('resize', function() { dirty = true; }, { passive: true });
    window.addEventListener('load', function() { dirty = true; });

    window.addEventListener('mousedown', function(event) {
      var x = event.clientX, y = event.clientY;

  // Window edge resize. Edges win over drag boxes, like native frames.
  // On the Unix and Windows backends the page reports the mouse-down and
  // native starts the resize (gtk_window_begin_resize_drag /
  // WM_NCLBUTTONDOWN+edge).
      if (RESIZABLE && (UNIX || PLATFORM === 'windows')) {
        var edge = edgeAt(x, y);
        if (edge) {
          event.preventDefault();
          event.stopPropagation();
          post({ method: %q, params: [{
            direction: edge,
            button: event.button,
  // GTK3 wants screen/root coordinates in device pixels; GTK4 uses
  // client (surface) coordinates, which are already logical.
            screenX: Math.round(event.screenX * dpr), screenY: Math.round(event.screenY * dpr),
            clientX: x, clientY: y,
            time: event.timeStamp
          }] });
          return;
        }
      }

      if (inRects(x * dpr, y * dpr, noDragRects)) { return; }
      if (!inRects(x * dpr, y * dpr, dragRects)) { return; }

  // Dragging a box swallows the click (title bars do not click through).
      event.preventDefault();
      event.stopPropagation();

  // Double-clicking a drag box toggles maximize, like a native title bar:
  // the SECOND left mouse-down of a pair - same spot within the platform
  // double-click tolerance (<= 400 ms and <= 5 css px) - posts the toggle
  // instead of a drag request. The pending pair is consumed when the toggle
  // fires, so a fast third click starts a fresh pair (a native triple-click
  // toggles once, then the third click drags). A non-left button breaks any
  // pending pair without starting one of its own. Clicks outside drag boxes
  // and on edge/resize or no-drag areas never reach this state (see above).
      if (event.button === 0 && lastClick &&
          Math.abs(x - lastClick.x) <= 5 && Math.abs(y - lastClick.y) <= 5 &&
          event.timeStamp - lastClick.t <= 400) {
        lastClick = null;
        post({ method: %q });
        return;
      }
      if (event.button === 0) {
        lastClick = { x: x, y: y, t: event.timeStamp };
      } else {
        lastClick = null;
      }
      post({ method: %q, params: [{
        button: event.button,
        screenX: Math.round(event.screenX * dpr), screenY: Math.round(event.screenY * dpr),
        clientX: x, clientY: y,
        time: event.timeStamp
      }] });
    }, true);

    if (RESIZABLE && (UNIX || PLATFORM === 'windows' || PLATFORM === 'darwin')) {
      window.addEventListener('mousemove', function(event) {
        setCursor(edgeAt(event.clientX, event.clientY));
      }, { passive: true });
      document.addEventListener('mouseout', function(event) {
        if (!event.relatedTarget) { setCursor(''); }
      });
      window.addEventListener('scroll', function() { setCursor(''); }, { passive: true });
    }

  // The MutationObserver is best-effort: at document-start the DOM is not
  // built yet (document.documentElement may be null) and observe() throws,
  // which must not take the tracking loop down with it. The 'load' listener
  // above re-scans once the page exists, so an early throw here only loses
  // incremental style watching until then.
    if (document.documentElement && window.MutationObserver) {
      try {
        new MutationObserver(function() { dirty = true; }).observe(document.documentElement, {
          attributes: true, attributeFilter: ['style', 'class'], childList: true, subtree: true
        });
      } catch (e) {}
    }
  }

  // Document-start scripts can run before the appkit bridge is in place
  // (this is exactly what happens on the WebView2/Windows backend on the
  // initial document). Retry quickly until the bridge appears so tracking
  // always arms.
  boot();
  var bootTries = 0;
  (function retry() {
    if (!started && bootTries++ < 100) {
      boot();
      setTimeout(retry, 10);
    }
  })();
})()
`

func buildRegionScript(resizable, postRegions bool, platform string) string {
	return fmt.Sprintf(appRegionScriptTmpl, jsonQuote(platform), resizable, postRegions,
		methodAppRegions, methodWindowCursor, methodWindowResize,
		methodWindowMaximize, methodWindowDrag)
}

// View declares a window: geometry, options, bindings and the first URL. Pass it
// to App.Show to create the window, then drive the live window through its
// methods. Fields are read once, at creation.
type View struct {
	// Debug opens the platform web inspector / developer tools for this view.
	Debug bool

	// FirstMouse lets a click reach this window while it is unfocused (macOS).
	// Opt in deliberately: the default protects destructive UI from stray clicks.
	FirstMouse bool

	// URL is the first page to load, usually an app:// path.
	URL string

	// Ready, if set, is called once after the first page finishes loading.
	Ready func()

	// window optionally attaches to an existing native window (set before Show).
	window unsafe.Pointer

	// Bind is this view's binding map; entries override App.Bind by name.
	Bind map[string]any

	// Width and Height are the initial window size in logical pixels.
	Width, Height int

	// State controls whether the window can be resized.
	State State

	w *webview

	app *App
}

// State controls whether and how the window can be resized.
type State int

const (
	// StateNone is freely resizable (the default).
	StateNone State = iota

	// StateMin is resizable, but not below Width x Height.
	StateMin

	// StateMax is resizable, but not above Width x Height.
	StateMax

	// StateFixed is not resizable; edge dragging is disabled.
	StateFixed
)

// App returns the App managing this view, or nil if the view is not shown.
func (v *View) App() *App {
	if v == nil {
		return nil
	}
	return v.app
}

// Show creates the window for view and starts its event bridge and bindings, or
// re-reveals it if already shown. The first call pins the calling goroutine to
// the UI thread; later window work must run there.
func (a *App) Show(view *View) error {
	if view == nil {
		return errors.New("appkit: Show requires a non-nil View")
	}
	if view.w != nil {
		view.w.Unminimize()
		view.w.Show()
		view.w.Raise()
		view.w.Focus()
		return nil
	}
	return a.showInitialView(view)
}

func (a *App) showInitialView(view *View) error {
	s, err := a.begin()
	if err != nil {
		return err
	}
	cfg := s.cfg
	view.Debug = view.Debug || cfg.Debug
	if err := a.start(s); err != nil {
		return err
	}
	view.app = a
	w, err := newView(view, fsContentFunc(cfg.FS, view))
	if err != nil {
		view.app = nil
		return err
	}
	view.w = w
	fail := func(err error) error {
		w.Close()
		view.w = nil
		view.app = nil
		return err
	}
	w.eventsGlobal = cfg.Events
	if err := w.installEvents(); err != nil {
		return fail(err)
	}
	if err := applyBindings(w, cfg.Bind, cloneBinds(view.Bind)); err != nil {
		return fail(err)
	}
	if view.window == nil {
		atomic.AddInt32(&s.windows, 1)
	}
	w.onReady = view.Ready
	if view.URL != "" {
		w.Navigate(view.URL)
	}
	return nil
}

func (v *View) mustEngine() *webview {
	if v.w == nil {
		panic("appkit: View is not shown: pass it to App.Show first")
	}
	return v.w
}

func notShown() error {
	return errors.New("appkit: View is not shown: pass it to App.Show first")
}

// Navigate loads url in the view.
func (v *View) Navigate(url string) { v.mustEngine().Navigate(url) }

// Window runs f on the UI thread with the platform's native window handle
// (NSWindow* / HWND / GtkWindow*), for anything the typed API does not cover.
func (v *View) Window(f func(wnd unsafe.Pointer)) {
	w := v.mustEngine()
	w.Dispatch(func() { f(w.Window()) })
}

// Close terminates and destroys the window and detaches it from its App.
func (v *View) Close() {
	if v.w == nil {
		return
	}
	v.w.Close()
	v.w = nil
	v.app = nil
}

// Eval executes js in the page.
func (v *View) Eval(js string) { v.mustEngine().Eval(js) }

// Focus gives the window keyboard focus, optionally raising it first.
func (v *View) Focus(raise bool) {
	w := v.mustEngine()
	if raise {
		w.Raise()
	}
	w.Focus()
}

// Show reveals a hidden view.
func (v *View) Show() { v.mustEngine().Show() }

// Hide hides the window, including its taskbar/dock entry.
func (v *View) Hide() { v.mustEngine().Hide() }

// Maximize maximizes the window.
func (v *View) Maximize() { v.mustEngine().Maximize() }

// Minimize minimizes the window.
func (v *View) Minimize() { v.mustEngine().Minimize() }

// Unminimize restores the window from the minimized state.
func (v *View) Unminimize() { v.mustEngine().Unminimize() }

// Unmaximize restores the window from the maximized state.
func (v *View) Unmaximize() { v.mustEngine().Unmaximize() }

// Maximized reports whether the OS currently considers the window maximized.
func (v *View) Maximized() bool {
	if v == nil || v.w == nil {
		return false
	}
	return v.w.Maximized()
}

// Dialog shows a native file dialog and returns the chosen paths (nil if the
// user cancelled).
func (v *View) Dialog(opts dialog.Options) ([]string, error) {
	if v.w == nil {
		return nil, notShown()
	}
	return v.w.Dialog(opts)
}

func (w *webview) Dialog(opts dialog.Options) ([]string, error) {
	ch := make(chan dialogOutcome, 1)
	w.Dispatch(func() {
		paths, err := dialog.Open(opts)
		ch <- dialogOutcome{paths: paths, err: err}
	})
	res := <-ch
	return res.paths, res.err
}

type dialogOutcome struct {
	paths []string
	err   error
}

func (w *webview) markReady() {
	if w.onReadyFired {
		return
	}
	w.onReadyFired = true
	if w.onReady != nil {
		w.onReady()
	}
}

func (w *webview) dropLoopback() {
	if w.transient == nil {
		return
	}
	closeLoopbackServer(w.transient)
	w.transient = nil
	w.contentBase = ""
}

func (w *webview) Close() {
	w.Terminate()
	w.Destroy()
}

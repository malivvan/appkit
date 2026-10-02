# AGENTS.md

Guidance for anyone - human or AI agent - modifying this repository. This file is
the contract: follow it unless a change explicitly and deliberately rewrites it.

## Hard rules

1. **No cgo - ever.** All platform interaction goes through `purego` (runtime
   `dlopen` / `LoadLibrary`) or existing bindings. Any GOOS/GOARCH must
   cross-build from any host with `CGO_ENABLED=0`.
2. **Root `*.go` files are prefixed `app_`, `lib_`, `bind_`, or `view_`.** No
   exceptions: a new root file must pick a prefix, and the package doc lives on
   `app_core.go`. Subpackages (`dialog/`, `notify/`, `tray/`, `demo/`) are not
   subject to this.
3. **Verify the build matrix before submitting.** At minimum `GOOS=windows
   GOARCH=amd64`, `GOOS=darwin GOARCH=amd64`, `GOOS=linux GOARCH=arm64`, plus the
   BSD targets (`make cross`, `make cross-bsd`) must all build from the host.
4. **Preserve the declarative contract.** `View` = config + post-spawn handle;
   `App` = config + runtime scope with settings frozen (snapshotted) on the first
   method call. No nested settings structs, no post-hoc mutation of `App`/`View`.
5. **Geometry is immutable after `Show`.** Wayland and GTK4 disagree on
   client-side move/resize; a unified setter would silently fail. Escape hatch:
   the native handle via `View.Window`.
6. **Bindings are kind-dispatched, never type-reflected.** The value's kind alone
   decides the binding (function / `[2]any{getter, setter}` / constant). Never
   walk structs or maps, expand methods, or add `bind:"…"` tags.
7. **Validate binding names at window creation** - bad names fail `App.Show`
   loudly: no empty or whitespace segments, no collisions with `window.*`
   built-ins, appkit internals (`__webview__`, `__appkit*`) or the events global;
   a leaf and its namespace cannot both be bound.
8. **Keep the uniform `app://` origin and its isolation headers** (COOP/COEP/CORP)
   intact on every backend. SharedArrayBuffer availability depends on it.
9. **Serve `App.FS` HTML with `html/template`** - never `text/template`. Pages
   interpolate `View` data and must stay auto-escaped.
10. **Windows are always frameless** with full transparency; drag regions are CSS
    (`-app-region`) tracked at runtime, not native chrome.
11. **Respect per-platform threading.** The goroutine creating the first window is
    pinned to its OS thread; UI calls belong there. Background work re-enters via
    `View.Window(func(unsafe.Pointer))`, or uses the goroutine-safe `View` methods.
    There is no exported `Dispatch` - only the internal engine has one.
12. **Never bundle or extract a native library.** Features that cannot be cleanly
    supported return `ErrUnsupported` - no flaky shims, no downloaded engines.
13. **Run `go test ./...` and `go test -short ./...` before submitting.** GUI tests
    must self-skip (stay green) when the platform view cannot run, and be
    skippable via `-short`.

## Architecture

- `app_core.go` - `App` config + lazily-created runtime scope, `Wait`/`Quit`,
  `Notify`/`Copy`/`Paste`/`Backend`/`Open`/`Reveal`, view registry, URL-scheme
  allow-list. The `appkit` package doc lives here.
- `app_autostart.go` - `Autostart` API and its shared label/slug/atomic-write
  helpers.
- `app_instance.go` + `app_instance_unix.go` - single-instance mode: the
  platform-independent claim/lock logic, and the Unix (`flock` + socket)
  transport (shared by Linux, BSD and macOS).
- `app_loopback.go` - the minimal loopback HTTP server used where `app://` is not
  available (macOS, or `App.HTTP`).
- `app_content.go` - `App.FS` resolver (`app://` paths, MIME map, HTML templating)
  and the request/response types.
- `app_icon.go` - app icon → tray/desktop icon helpers.
- `app_{darwin,unix,windows}.go` - per-platform app services: icon install,
  open/reveal, single-instance transport (Windows), autostart backend.
- `bind_core.go` - binding model, reflection wrapper, batch planning, serial call
  queue, `webview.Bind`.
- `bind_bridge.go` - the generated JS bridge (init/bind/unbind scripts, JSON
  helpers). `//go:build !js`.
- `bind_events.go` - the page-side pub/sub bridge and `View.On`/`Off`/`Emit`.
- `view_core.go` - `View`/window API: declarative `View`, `App.Show`, `State`,
  `View.Dialog`, the CSS drag-region tracker, and the shared `webview` handle
  methods.
- `lib_darwin.go` / `lib_unix.go` / `lib_windows.go` - the per-platform engine:
  every direct platform-API call (WKWebView via objc, WebKitGTK via GTK, WebView2
  + Win32/COM). **No engine-independent code belongs here.**
- `lib_windows_{386,amd64,arm64}.go` - the architecture-specific `putBounds`
  FFI shim.
- `demo/` - showcase app with `--selftest` (drives a real view; headless via
  `xvfb-run -a`).
- `tray/`, `notify/`, `dialog/` - companion packages, each with its own `demo/`.

## Design decisions & boundaries

- **Companion packages exist on purpose.** The core stays window + view only. New
  desktop services belong in a subpackage, not the core.
- **Error philosophy.** A feature a platform cannot cleanly support returns
  `ErrUnsupported` (or `ErrUnavailable`) - it never installs a shim that might
  half-work. Genuine failures (`App.Show`, `View.Dialog`, `App.Open`) return a
  wrapped error with an `appkit:` prefix.
- **`app://` first, loopback only when forced.** `app://` is a secure, isolated
  origin on every backend that supports it; the loopback server exists solely
  because macOS WKWebView cannot make a custom scheme a secure context.
- **No runtime geometry mutation** (see rule 5). This is a deliberate boundary,
  not a gap to fill.
- **One tray per process**; a second `Set`/`Run` returns `ErrAlreadyRunning`.
- **The binding surface is the API.** Do not add reflection-driven convenience
  (struct walking, method expansion, tags) - the kind-dispatch rule is the whole
  contract and keeps the JS bridge small and predictable.

## Platform backends

- **Linux/BSD:** the WebKitGTK stack is chosen at runtime by whether
  `libwebkitgtk-6.0.so.4` loads (→ GTK4; else GTK3). **Never load both** - that
  corrupts GTK's type system and crashes `gtk_init`. `APPKIT_BACKEND` pins
  `webkitgtk-6.0` or `webkit2gtk-4.1`; an unloadable pin → warning + autodetect.
  GTK4 additionally loads `libgio-2.0.so.0` for the first file dialog.
- **Windows:** the Edge WebView2 Runtime is located via the registry. To avoid
  shipping `WebView2Loader.dll`, appkit calls the runtime's internal
  environment-creation export directly (`createWebViewEnvironment` in
  `lib_windows.go`). That export is undocumented - if it changes, `App.Show` must
  return a clear error, not crash.
- **macOS:** WKWebView. Custom schemes cannot be secure contexts (long-standing
  WebKit bug), so macOS always serves the UI from a transient per-view loopback
  `http://localhost` torn down after the first page load. First-click behavior:
  `FirstMouse` subclasses `acceptsFirstMouse:`; `Focus` beats `Raise` for taking
  back focus without stealing keystrokes.

## Binding semantics

- Dotted paths nest under `window`; **the value's kind alone** dispatches:
  - Go function, 0 args → callable getter (awaitable via `await window.fn`)
  - Go function, 1 arg → callable setter (assignment runs the Go side but yields
    the assigned value)
  - Go function, other arity → plain callable
  - `[2]any{getter, setter}` → readable+writable property
  - Anything else → immutable constant, bound wholesale
- Installation order is deterministic: `App.Bind` first (alphabetical), then
  `View.Bind` (alphabetical). Never Go map order. Everything is `Object.freeze`d;
  the page's own `window` stays untouched.
- Page→Go calls dispatch in order, off the UI thread. Bound functions may return
  nothing, a value, an error, or value+error (the page sees a Promise either way).
  Unawaited rejections rethrow to the console.
- `View.Bind` replaces an `App.Bind` entry of the same name; a nil value in
  `View.Bind` unbinds.
- `events` is reserved (or whatever `App.Events` renames it to).

## Invariants to preserve in tests

- GUI tests: short, self-skipping when no display/engine is available, `-short`
  skippable. Linux: `xvfb-run -a go test ./...`.
- `demo --selftest` covers bridge echo, all binding forms, events round trip,
  clipboard, autostart, drag regions, stable ids, cross-origin isolation
  (SharedArrayBuffer), maximize toggle. Extend it when adding user-visible
  features.
- Serving `App.FS` HTML must escape `View` data (guarded by
  `TestServeAppFSRendersHTMLTemplates`).

## Definition of done

```bash
gofmt -l .                                   # must print nothing
go vet ./...                                 # and GOOS=darwin / GOOS=windows
golangci-lint run ./...                      # 0 issues; also GOOS=darwin/windows
go test -race ./...                          # linux
go test -run 'Script|Bridge' -count=1 .      # JS bridge behaviour
make check-imports                           # no net/http or crypto/tls imports
make cross cross-bsd                         # full build matrix
```

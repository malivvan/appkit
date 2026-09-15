# appkit

**Web-rendered desktop apps in pure Go.** appkit drives the web engine the
operating system already ships (WKWebView, Edge WebView2, WebKitGTK) behind one
Go API, and layers the desktop services on top: windows, drag regions, native
dialogs, notifications, clipboard, tray, single-instance, URL/file opening, and
autostart.

**No cgo.** Platform libraries are loaded at runtime with
[purego](https://github.com/ebitengine/purego), so any target cross-compiles from
any host with `CGO_ENABLED=0` - no MinGW, no sysroots, `go install` just works.

Not Electron: no engine is bundled, so binaries stay small - but the target
machine supplies the view.

## How it works

| OS | Engine | Extra requirement |
|---|---|---|
| macOS | WKWebView | none |
| Windows | Edge WebView2 Runtime | preinstalled on current Win10/11, else Evergreen |
| Linux | WebKitGTK (GTK4 or GTK3, auto-detected) | distro package |

`App.Backend()` reports the loaded engine; features a platform cannot support
cleanly return `ErrUnsupported` rather than a broken shim.

## Requirements

- **Go** 1.27 or newer.
- **Windows:** the Edge WebView2 Runtime (already present on current Windows).
- **macOS:** nothing.
- **Linux:** a WebKitGTK package on the dynamic-linker path:

  ```bash
  apt install libwebkit2gtk-4.1-0      # Debian/Ubuntu, GTK3
  apt install libwebkitgtk-6.0-4       # Debian/Ubuntu, GTK4
  dnf install webkit2gtk4.1            # Fedora
  pacman -S webkit2gtk-4.1             # Arch
  ```

  Debug with `ldconfig -p | grep webkit`.

## Quick start

```go
package main

import (
	"log"

	"github.com/malivvan/appkit"
)

func main() {
	app := &appkit.App{Name: "My App", Exit: true}

	view := &appkit.View{
		Width: 1280, Height: 800,
		URL: "app://index.html",
	}
	if err := app.Show(view); err != nil {
		log.Fatal(err)
	}
	defer view.Close()
	if err := app.Wait(); err != nil {
		log.Fatal(err)
	}
}
```

That is the whole program: a frameless, transparent 1280x800 window whose page
is the chrome. Assign an `io/fs.FS` to `App.FS` and every view is served from a
uniform **`app://`** origin - secure and cross-origin isolated (COOP/COEP/CORP),
so `localStorage`, `crypto.subtle` and `SharedArrayBuffer` all work. HTML files
are executed as templates with the requesting `View` as data (auto-escaped):

```html
<h1>{{.App.Name}}</h1>
<p>{{.URL}} - {{.Width}}x{{.Height}}</p>
```

## Core concepts

**Declarative windows.** Define a `View` (geometry, options, bindings, first
URL) and hand it to `App.Show`. An `*App` is configuration plus a runtime scope,
like `http.Server` - settings are frozen on the first method call.

**Bindings.** `App.Bind` (app-wide) and `View.Bind` (per-view) are declarative
maps exposed on the page under `window.*`. The value's kind alone decides the
behavior - no struct walking or tags:

- Go function, 0 args -> awaitable getter; 1 arg -> setter; other arity ->
  plain callable
- `[2]any{getter, setter}` -> readable+writable property
- Anything else -> immutable constant

Names are dotted paths (`"api.call"` -> `window.api.call`) validated at window
creation. `View.Bind` overrides `App.Bind`; all calls return Promises.

**Events.** Every view gets a lightweight pub/sub bridge: `View.On` /
`View.Off` / `View.Emit` on the Go side, `window.events` on the page (rename
via `App.Events`). Each event reaches every listener on both sides exactly once.

**Frameless everywhere.** Windows are always frameless and fully transparent -
the page *is* the chrome. Mark movable regions with CSS (`-app-region: drag`)
and they are tracked live through scrolling, resizing, and DOM changes.

## Desktop services

| Service | API |
|---|---|
| Tray | `App.Tray` - declarative menu tree; Linux via D-Bus StatusNotifierItem |
| Notifications | `App.Notify`, `notify.Show`, `notify.Alert`, `notify.Beep` |
| Native dialogs | `View.Dialog` - open / multi-open / save / directory |
| Clipboard | `App.Copy` / `App.Paste` |
| Single instance | `App.Exec` - later launches forward args and exit |
| Autostart | `App.Autostart()` - XDG `.desktop` or LaunchAgent |
| Open / reveal | `App.Open` / `App.Reveal` |

## Window runtime state

`Show`, `Hide`, `Maximize`/`Minimize` and their inverses, `Focus`, `Raise`, and
`Maximized` are goroutine-safe. Geometry is deliberately **not mutable at
runtime**: Wayland and GTK4 disagree on client-side move/resize, so a unified
setter would silently fail. Use `View.Window` for platform-specific changes.

## Demos & testing

```bash
go run ./demo             # showcase window (custom chrome, all features)
go run ./demo -tray       # showcase + tray menu
go test ./...             # unit + GUI smoke tests (GUI tests self-skip headless)
go test -short ./...      # headless-only
xvfb-run -a go test ./... # Linux GUI tests under Xvfb
```

## Troubleshooting

- **"none of [libgtk-3.so.0 ...] could be loaded"** - install a WebKitGTK
  package and confirm it is on the loader path: `ldconfig -p | grep webkit`.
- **Black/blank window on Linux** - another GTK stack may be loaded; try
  `APPKIT_BACKEND` to pin one, or run under `xvfb-run` to isolate display issues.
- **WebView2 missing on Windows** - install the Evergreen runtime (the app
  returns a clear error rather than crashing).

## Project layout

```
app_*.go    App: runtime, autostart, single-instance, app:// content
bind_*.go   Bindings: model + reflection, JS bridge, page-side events
view_*.go   View/window API, drag-region tracking, dialogs
lib_*.go    Per-platform engine (WKWebView / WebKitGTK / WebView2)
dialog/ notify/ tray/   Companion packages, each with its own demo
demo/       Showcase app
```

## License

See [`LICENSE`](LICENSE).

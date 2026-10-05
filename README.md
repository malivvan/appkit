# appkit

**Build web-rendered desktop apps in pure Go.** appkit drives the web engine the
operating system already ships (WKWebView, Edge WebView2, WebKitGTK) behind one
Go API, and layers the desktop services on top: windows, drag regions, native
dialogs, notifications, clipboard, tray, single-instance, URL/file opening, and
autostart.

**No cgo.** Platform libraries are loaded at runtime with
[purego](https://github.com/malivvan/purego), so any target cross-compiles from
any host with `CGO_ENABLED=0` - no MinGW, no sysroots, `go install` just works.

Not Electron: no engine is bundled, so binaries stay small - but the target
machine supplies the view.

> **TL;DR** - `go get github.com/malivvan/appkit`, paste the example below, done.
> Jump to [Quick start](#quick-start) or [Troubleshooting](#troubleshooting).

## Contents

- [How it works](#how-it-works)
- [Requirements](#requirements)
- [Quick start](#quick-start)
- [Core concepts](#core-concepts)
- [Desktop services](#desktop-services)
- [Window runtime state](#window-runtime-state)
- [Demos & testing](#demos--testing)
- [Troubleshooting](#troubleshooting)
- [Project layout](#project-layout)

## How it works

| OS | Engine | Extra requirement |
|---|---|---|
| macOS | WKWebView | none |
| Windows | Edge WebView2 Runtime | preinstalled on current Win10/11, else Evergreen |
| Linux/BSD | WebKitGTK (GTK4 or GTK3, auto-detected) | distro package |

Supported build targets track [purego](https://github.com/malivvan/purego): Linux
(amd64, arm64, 386, armv7/6/5, loong64, ppc64le, riscv64, s390x), FreeBSD & NetBSD
(amd64, arm64), Windows (amd64, arm64, 386), Darwin (amd64, arm64). Exotic targets
are compile-tested only - runtime verification is welcome via an issue.

`App.Backend()` reports the loaded engine; features a platform cannot support
cleanly return `ErrUnsupported` rather than a broken shim.

## Requirements

- **Go** 1.27 or newer.
- **Windows:** the Edge WebView2 Runtime (already present on current Windows).
- **macOS:** nothing.
- **Linux/BSD:** a WebKitGTK package on the dynamic-linker path, matching the
  binary's architecture:

  ```bash
  apt install libwebkit2gtk-4.1-0      # Debian/Ubuntu, GTK3
  apt install libwebkitgtk-6.0-4       # Debian/Ubuntu, GTK4
  dnf install webkit2gtk4.1            # Fedora
  pacman -S webkit2gtk-4.1             # Arch
  ```

  Debug with `ldconfig -p | grep webkit`. Pin the stack with
  `APPKIT_BACKEND=webkitgtk-6.0` (GTK4) or `webkit2gtk-4.1` (GTK3). On NixOS the
  libraries are not on the default loader path - add `webkitgtk_4_1` /
  `webkitgtk_6_0` to `buildInputs`, or use `LD_LIBRARY_PATH` / `nix-ld`.

## Quick start

```bash
mkdir hello && cd hello
go mod init hello
go get github.com/malivvan/appkit
```

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

That is the whole program: a frameless, transparent 1280×800 window whose page is
the chrome. To serve real content, assign an `io/fs.FS` to `App.FS` and point the
view at an `app://` path (see [Core concepts](#core-concepts)). The goroutine
that shows the first window is pinned to its OS thread, so keep direct window
calls there; run code on the UI thread from elsewhere with
`view.Window(func(unsafe.Pointer) { … })`, or use the goroutine-safe `View`
methods (`Show`, `Hide`, `Maximize`, `Eval`, `Emit`, …).

To hide the console on Windows: `go build -ldflags="-H windowsgui" .`.

## Core concepts

**Declarative windows.** Define a `View` (geometry, options, bindings, first URL)
and hand it to `App.Show`. An `*App` is configuration plus a runtime scope, like
`http.Server` - settings are frozen on the first method call. `Wait` returns on
`App.Quit`, or when the last window closes if `App.Exit` is true.

**Serving UI.** Assign an `io/fs.FS` to `App.FS` and every view is served from a
uniform **`app://`** origin - no ports, secure, cross-origin isolated
(COOP/COEP/CORP), so `localStorage`, `crypto.subtle`, `getUserMedia`, and
**SharedArrayBuffer** all work. HTML files are templates executed with the
requesting `View` as data (auto-escaped):

```html
<h1>{{.App.Name}}</h1>
<p>{{.URL}} - {{.Width}}x{{.Height}}</p>
```

**Bindings.** `App.Bind` (app-wide) and `View.Bind` (per-view) are declarative
maps exposed on the page under `window.*`. The value's kind alone decides the
behavior - no struct walking or tags:

- Go function → callable; 0 args makes it an awaitable getter, 1 arg a setter
- `[2]any{getter, setter}` → readable+writable property
- Anything JSON-encodable → immutable constant

Names are dotted paths (`"api.call"` → `window.api.call`) validated at window
creation. `View.Bind` overrides `App.Bind`; all calls return Promises.

**Frameless everywhere.** Windows are always frameless and fully transparent -
the page *is* the chrome. Mark movable regions with CSS (`-app-region: drag`;
Electron's `-webkit-app-region` alias is accepted), tracked live through
scrolling, resizing, and DOM changes. Double-clicking a drag region toggles
maximize. `State` controls resizability; `StateFixed` disables edge resizing.

**Events.** Every view gets a lightweight pub/sub bridge: `view.On` / `view.Off`
/ `view.Emit` on the Go side, `window.events` on the page (rename via
`App.Events`). Each event reaches every listener on both sides exactly once.

## Desktop services

| Service | API |
|---|---|
| Tray | `App.Tray` - declarative menu tree, checkboxes, submenus, per-item icons; Linux via D-Bus StatusNotifierItem, so no desktop is excluded |
| Notifications | `App.Notify`, `notify.Show`, `ShowOpts`, `Alert`, `Beep` |
| Native dialogs | `View.Dialog` - open / multi-open / save / directory with filters |
| Clipboard | `App.Copy` / `App.Paste` |
| Single instance | `App.Exec` - later launches forward args and exit; `--new-instance` bypasses |
| Autostart | `App.Autostart()` - XDG `.desktop`, `HKCU\…\Run`, or macOS LaunchAgent/`SMAppService` |
| Open / reveal | `App.Open` / `App.Reveal` |
| Runtime icon | `App.Icon` (best-effort; Windows reads from executable resources) |

Only one tray per process (`ErrAlreadyRunning` on a second).

## Window runtime state

`Show`, `Hide` (hide-to-tray: screen + taskbar), `Maximize`/`Minimize` and their
inverses, `Focus` (caret into the page), `Raise`, and `Maximized` (queries the OS
- correct after double-clicking a frameless drag area) are all goroutine-safe.

Geometry is deliberately **not mutable at runtime** - Wayland and GTK4 disagree
on client-side move/resize, so a unified setter would silently fail. Set the
initial size via `View.Width`/`Height`; for later changes, take the native handle
with `View.Window` and use the platform API.

### macOS notes

`View.FirstMouse` opts into first-click passthrough (clicks reach an unfocused
window). Opt in deliberately - the default protects destructive interfaces from
stray activation clicks. `Focus` recovers focus when your app knowingly took it
away; prefer it over `Raise`.

## Demos & testing

```bash
go run ./demo             # showcase window (custom chrome, all features)
go run ./demo -http       # UI over loopback http://localhost (App.HTTP)
go run ./demo -tray       # showcase + tray
go run ./demo -selftest   # automated self test, exit 0/1
```

```bash
go test ./...             # unit + GUI smoke tests (GUI tests self-skip headless)
go test -short ./...      # headless-only
xvfb-run -a go test ./... # Linux GUI tests under Xvfb
```

Standalone subpackage demos: `tray/demo`, `notify/demo`, `dialog/demo`.
`make all` runs build, vet, tests, the import check, the cross-build matrix, and
the JS-parsing check.

## Troubleshooting

- **"none of [libgtk-3.so.0 …] could be loaded"** - install a WebKitGTK package
  (see [Requirements](#requirements)) and confirm it is on the loader path:
  `ldconfig -p | grep -E 'gtk|webkit'`.
- **Black/blank window on Linux** - another GTK stack may be loaded, or you are
  missing the matching `libepoxy`/GPU driver; try `APPKIT_BACKEND` to pin a
  stack, or run under `xvfb-run` to isolate display issues.
- **No window on a headless box** - there is no display; use `xvfb-run -a`.
- **Notifications do nothing on macOS** - UserNotifications need a bundled
  `.app` the user has granted permission; `ErrUnavailable` is returned otherwise.
- **WebView2 missing on Windows** - install the Evergreen runtime (the app
  returns a clear error rather than crashing).

## Project layout

```
app_*.go    App: runtime, autostart, single-instance, loopback, app:// content, icon
bind_*.go   Bindings: model + reflection, JS bridge builder, page-side events
view_*.go   View/window API, CSS drag-region tracking, dialogs
lib_*.go    Per-platform engine (WKWebView / WebKitGTK / WebView2 + Win32/COM)
dialog/ notify/ tray/   Companion packages, each with its own demo
demo/       Showcase app (also drives -selftest)
```

Root `*.go` files are prefixed `app_`, `lib_`, `bind_`, or `view_`; see
[`AGENTS.md`](AGENTS.md) for the rules contributors (human or AI) must follow.

## License

See [`LICENSE`](LICENSE).

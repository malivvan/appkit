# appkit [![Go Reference](https://pkg.go.dev/badge/github.com/malivvan/appkit.svg)](https://pkg.go.dev/github.com/malivvan/appkit) [![Release](https://img.shields.io/github/v/release/malivvan/appkit.svg?sort=semver)](https://github.com/malivvan/appkit/releases/latest) [![License](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)

**appkit** drives the web engine the operating system already ships (WKWebView,
WebView2, WebKitGTK) behind one Go API, and layers the desktop services on top:
windows, drag regions, native dialogs, notifications, clipboard, tray, single-
instance, URL/file opening, and autostart.

Platform libraries are loaded at runtime with [purego](https://github.com/malivvan/purego),
so any target cross-compiles from any host - no MinGW, no sysroots, `go install` just works.

The package is deliberately small and simple, with no hidden dependencies. The API is designed
to be declarative and goroutine-safe. No engine is bundled, so binaries stay small.

## Installation
```sh
# go 1.27.1+
go get github.com/malivvan/webkitgtk@latest
```

## Supported Platforms

appkit binds the OS web engine through purego, so the following platforms are supported by simply setting
`GOOS` and `GOARCH` to the desired target and running `go build` or `go install`. The engine is chosen automatically
based on the OS.

| GOOS    | GOARCH                                                                           | Engine    |
|---------|----------------------------------------------------------------------------------|-----------|
| Linux   | amd64, arm64*, 386, armv7*, armv6*, armv5*, loong64*, ppc64le*, riscv64*, s390x* | WebKitGTK |
| FreeBSD | amd64, arm64*                                                                    | WebKitGTK |
| NetBSD  | amd64, arm64*                                                                    | WebKitGTK |
| Windows | amd64, arm64*, 386                                                               | WebView2  |
| Darwin  | amd64, arm64*                                                                    | WKWebView |

> Architectures marked with a `*` have only been tested to compile, not to run. If somebody has
> a machine of that architecture and can verify the runtime, please open an issue.

## Requirements
Either
[`webkit2gtk-4.1`](https://pkgs.org/search/?q=webkit2gtk-4.1&on=name)
([*stable*](https://webkitgtk.org/reference/webkit2gtk/stable/)) or
[`webkitgtk-6.0`](https://pkgs.org/search/?q=webkitgtk-6.0&on=name)
([*unstable*](https://webkitgtk.org/reference/webkitgtk/unstable))
is required at runtime. If both are installed the latest version will be used.
<table>
	<tr>
		<td style="font-size: 14px;font-weight: bold;">Debian / Ubuntu</td>
		<td><code>apt install libwebkit2gtk-4.1-0</code></td>
		<td><code>apt install libwebkitgtk-6.0-4</code></td>
	</tr>
	<tr>
		<td style="font-size: 14px;font-weight: bold;">RHEL / Fedora</td>
		<td><code>dnf install webkit2gtk4.1</code></td>
		<td><code>dnf install webkitgtk6.0</code></td>
	</tr>
	<tr>
		<td style="font-size: 14px;font-weight: bold;">Alpine</td>
		<td><code>apk add webkit2gtk-4.1</code></td>
		<td><code>apk add webkit2gtk-6.0</code></td>
	</tr>
	<tr>
		<td style="font-size: 14px;font-weight: bold;">Arch</td>
		<td><code>pacman -S webkit2gtk-4.1</code></td>
		<td><code>pacman -S webkitgtk-6.0</code></td>
	</tr>
	<tr>
		<td style="font-size: 14px;font-weight: bold;">Gentoo</td>
		<td colspan="2" align="center"><code style="margin:0px;padding:2px">emerge -av net-libs/webkit-gtk</code> (slot 4.1 or 6.0)</td>
	</tr>
	<tr>
		<td style="font-size: 14px;font-weight: bold;">NixOS</td>
		<td><code>nix-env -iA nixpkgs.webkitgtk_4_1</code></td>
		<td><code>nix-env -iA nixpkgs.webkitgtk_6_0</code></td>
	</tr>
	<tr>
		<td style="font-size: 14px;font-weight: bold;">FreeBSD</td>
		<td><code>pkg install webkit2-gtk3</code> (flavor 4.1)</td>
		<td><code>pkg install webkit2-gtk4</code></td>
	</tr>
	<tr>
		<td style="font-size: 14px;font-weight: bold;">NetBSD</td>
		<td><code>pkg_add webkit-gtk41</code></td>
		<td><em>not yet available (pkgsrc stuck at 2.36.8)</em></td>
	</tr>
</table>

> The environment variable `APPKIT_BACKEND` can be set to `webkit2gtk-4.1` or
> `webkitgtk-6.0` to force a specific WebKitGTK version.

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

**Secure Context** Assign an `io/fs.FS` to `App.FS` and every view is served from a
uniform **`app://`** origin - no ports, secure, cross-origin isolated
(COOP/COEP/CORP), so `localStorage`, `crypto.subtle`, `getUserMedia`, and
**SharedArrayBuffer** all work. HTML files are templates executed with the
requesting `View` as data (auto-escaped):

```html
<h1>{{.App.Name}}</h1>
<p>{{.URL}} - {{.Width}}x{{.Height}}</p>
```

**Bindings** `App.Bind` (app-wide) and `View.Bind` (per-view) are declarative
maps exposed on the page under `window.*`. The value's kind alone decides the
behavior - no struct walking or tags:

- Go function → callable; 0 args makes it an awaitable getter, 1 arg a setter
- `[2]any{getter, setter}` → readable+writable property
- Anything JSON-encodable → immutable constant

Names are dotted paths (`"api.call"` → `window.api.call`) validated at window
creation. `View.Bind` overrides `App.Bind`; all calls return Promises.

**Events** Every view gets a lightweight pub/sub bridge: `view.On` / `view.Off`
/ `view.Emit` on the Go side, `window.events` on the page (rename via
`App.Events`). Each event reaches every listener on both sides exactly once.

**Frameless by default** Windows are frameless and fully transparent unless
`View.Frame` is true (which asks for the ordinary OS-framed, opaque window) -
the page *is* the chrome. Mark movable regions with CSS (`-app-region: drag`;
Electron's `-webkit-app-region` alias is accepted), tracked live through
scrolling, resizing, and DOM changes. Double-clicking a drag region toggles
maximize. `State` controls resizability; `StateFixed` disables edge resizing.

## Desktop services

| Service         | API                                                                                                                                     |
|-----------------|-----------------------------------------------------------------------------------------------------------------------------------------|
| Systray         | `App.Tray` - declarative menu tree, checkboxes, submenus, per-item icons; Linux via D-Bus StatusNotifierItem, so no desktop is excluded ||
| Notifications   | `App.Notify`, `notify.Show`, `ShowOpts`, `Alert`, `Beep`                                                                                |
| Native dialogs  | `View.Dialog` - open / multi-open / save / directory with filters                                                                       |
| Clipboard       | `App.Copy` / `App.Paste`                                                                                                                |
| Single instance | `App.Exec` - later launches forward args and exit; `--new-instance` bypasses                                                            |
| Autostart       | `App.Autostart()` - XDG `.desktop`, `HKCU\…\Run`, or macOS LaunchAgent/`SMAppService`                                                   |
| Open / reveal   | `App.Open` / `App.Reveal`                                                                                                               |
| Runtime icon    | `App.Icon` (best-effort; Windows reads from executable resources)                                                                       |

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
go run ./demo -frame      # the same showcase with the OS window frame
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


## License

See [`LICENSE`](LICENSE).

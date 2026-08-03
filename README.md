# appkit

**Web-rendered desktop apps in pure Go.** appkit drives the web engine the
operating system already ships behind one Go API, and layers the desktop
services on top: windows, drag regions, native dialogs, notifications,
clipboard, tray, single-instance, URL/file opening, and autostart.

**No cgo.** Platform libraries are loaded at runtime with
[purego](https://github.com/ebitengine/purego), so any target cross-compiles from
any host with `CGO_ENABLED=0` - no MinGW, no sysroots, `go install` just works.

Not Electron: no engine is bundled, so binaries stay small - but the target
machine supplies the view.

> **Status** - early days. Linux (WebKitGTK) is the working backend; macOS
> and Windows are next.

## How it works

| OS | Engine | Extra requirement |
|---|---|---|
| Linux | WebKitGTK | distro package |
| macOS | WKWebView | planned |
| Windows | Edge WebView2 | planned |

## Requirements

- **Go** 1.27 or newer.
- **Linux:** a WebKitGTK package on the dynamic-linker path:

  ```bash
  apt install libwebkit2gtk-4.1-0      # Debian/Ubuntu
  dnf install webkit2gtk4.1            # Fedora
  pacman -S webkit2gtk-4.1             # Arch
  ```

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
is the chrome. Assign an `io/fs.FS` to `App.FS` to serve real content over the
`app://` origin, and expose Go values to the page with `App.Bind` / `View.Bind`.

## Project layout

```
app_*.go    App: runtime, view registry
bind_*.go   Bindings: model + reflection, generated JS bridge
view_*.go   View/window API and the shared engine handle
lib_*.go    Per-platform engine (direct platform-API calls)
demo/       Showcase app
```

## License

See [`LICENSE`](LICENSE).

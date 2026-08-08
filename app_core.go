// Package appkit builds web-rendered desktop applications in pure Go. It drives
// the operating system's preinstalled web engine - WKWebView on macOS, Edge
// WebView2 on Windows, WebKitGTK on Linux/BSD - behind a single API, with no
// cgo: platform libraries are resolved at runtime via purego, so every supported
// target cross-compiles from any host with CGO_ENABLED=0.
//
// An App is configuration plus a lazily-created runtime scope, like http.Server;
// a View declares a window and is handed to App.Show. Windows are always
// frameless and fully transparent - the page is the chrome - and app:// content
// is served from App.FS as a secure, cross-origin-isolated origin.
package appkit

import (
	"errors"
	"fmt"
	"io/fs"
	"sync"
	"sync/atomic"
)

// App is the application: declarative configuration plus a lazily-created
// runtime scope. Configure it by setting fields, then use it; like http.Server,
// its settings are frozen (snapshotted) on the first method call.
type App struct {
	// Debug enables the platform web inspector for every view.
	Debug bool

	// Events renames the page-side events global (default "events").
	Events string

	// ID is the stable identity used for single-instance mode and autostart.
	ID string

	// Exec, when set, enables single-instance mode: later launches forward their
	// arguments to the running instance through it and exit.
	Exec func(args []string)

	// Name is the application name shown to the OS.
	Name string

	// Icon is the app icon as PNG bytes; empty falls back to the built-in icon.
	Icon []byte

	// Bind is the app-wide binding map exposed to every view.
	Bind map[string]any

	// FS serves the page's content from the app:// origin.
	FS fs.FS

	// HTTP serves FS over a loopback http:// origin instead of app://.
	HTTP bool

	// Exit quits the app when its last window closes.
	Exit bool

	scopeOnce sync.Once
	scope     *appRuntime
}

type appSetup struct {
	Name   string
	Exit   bool
	Icon   []byte
	ID     string
	Exec   func(args []string)
	FS     fs.FS
	HTTP   bool
	Debug  bool
	Events string
	Bind   map[string]any
}

type appRuntime struct {
	cfg     appSetup
	initErr error

	startOnce   sync.Once
	startErr    error
	releaseInst func()

	windows  int32
	exitOnce sync.Once
	exitFlag int32
}

func snapshotSetup(a *App) appSetup {
	return appSetup{
		Name:   a.Name,
		Exit:   a.Exit,
		Icon:   a.Icon,
		ID:     a.ID,
		Exec:   a.Exec,
		FS:     a.FS,
		HTTP:   a.HTTP,
		Debug:  a.Debug || debugFromEnv(),
		Events: a.Events,
		Bind:   a.Bind,
	}
}

func (a *App) begin() (*appRuntime, error) {
	if a == nil {
		return nil, errors.New("appkit: nil *App")
	}
	a.scopeOnce.Do(func() {
		s := &appRuntime{cfg: snapshotSetup(a)}
		s.initErr = ensureInit()
		a.scope = s
		activeRuntime.Store(s)
	})
	if a.scope.initErr != nil {
		return nil, a.scope.initErr
	}
	return a.scope, nil
}

func (a *App) start(s *appRuntime) error {
	s.startOnce.Do(func() {
		release, err := claimPrimaryInstance(&s.cfg)
		if err != nil {
			s.startErr = err
			return
		}
		s.releaseInst = release
		icon := s.cfg.Icon
		if len(icon) == 0 {
			icon = embeddedIcon
		}
		_ = setAppIcon(icon, s.cfg.Name)
	})
	return s.startErr
}

// Wait blocks in the UI loop until the app exits: on App.Quit, or when the last
// window closes if Exit is set. It installs the tray (if configured) for the
// duration, and must be called from the goroutine that showed the first window.
func (a *App) Wait() error {
	s, err := a.begin()
	if err != nil {
		return fmt.Errorf("appkit: wait: %w", err)
	}
	if err := a.start(s); err != nil {
		return err
	}
	for atomic.LoadInt32(&s.exitFlag) == 0 {
		pumpUI()
	}
	if s.releaseInst != nil {
		s.releaseInst()
		s.releaseInst = nil
	}
	return nil
}

// Quit asks the UI loop to exit; a blocked Wait returns afterwards.
func (a *App) Quit() {
	s, err := a.begin()
	if err != nil {
		return
	}
	s.signalExit()
}

var activeRuntime atomic.Pointer[appRuntime]

func onAppWindowClosed() {
	if s := activeRuntime.Load(); s != nil {
		s.handleWindowClosed()
	}
}

func (s *appRuntime) handleWindowClosed() {
	if atomic.AddInt32(&s.windows, -1) == 0 && s.cfg.Exit {
		s.signalExit()
	}
}

func (s *appRuntime) signalExit() {
	s.exitOnce.Do(func() {
		atomic.StoreInt32(&s.exitFlag, 1)
		wakeUI()
	})
}

// Backend reports the identifier of the loaded engine (for example
// "webkit2gtk-4.1"), or "" if initialisation failed.
func (a *App) Backend() string {
	if _, err := a.begin(); err != nil {
		return ""
	}
	return platformBackend()
}

// Command demo is the appkit showcase: a real window with custom chrome that
// exercises windows, bindings, events, dialogs, clipboard, tray, notifications
// and autostart. Run with -selftest for an automated pass/fail.
package main

import (
	"embed"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"log"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/malivvan/appkit"
	"github.com/malivvan/appkit/dialog"
	"github.com/malivvan/appkit/tray"
)

//go:embed assets
var assetsFS embed.FS

func assetsRoot() fs.FS {
	sub, err := fs.Sub(assetsFS, "assets")
	if err != nil {
		panic(err)
	}
	return sub
}

type windowDemo struct {
	w     *appkit.View
	app   *appkit.App
	self  *selfTest
	close chan struct{}
}

type selfTest struct {
	active  bool
	ready   chan struct{}
	done    chan struct{}
	reports chan []testReport
}

type testReport struct {
	Name   string `json:"name"`
	Pass   bool   `json:"pass"`
	Detail string `json:"detail,omitempty"`
}

type autostartInfo struct {
	Enabled bool   `json:"enabled"`
	Backend string `json:"backend"`
	Path    string `json:"path"`
}

func main() {
	var (
		frame    = flag.Bool("frame", false, "use the OS window frame - title bar and system buttons - instead of the custom borderless chrome")
		debug    = flag.Bool("debug", false, "open the platform web inspector / dev tools")
		selftest = flag.Bool("selftest", false, "windowed showcase that runs the UI self test and exits 0/1")
		httpFn   = flag.Bool("http", false, "serve the window's app:// content over a temporary loopback HTTP server (App.HTTP); Linux/Windows opt in, macOS always does")
		trayFn   = flag.Bool("tray", false, "add a tray menu (Show/Hide/Quit) to the windowed showcase")
	)
	flag.Parse()

	_ = debug

	app := &appkit.App{Name: "appkit demo xx", Exit: true}

	var w *appkit.View
	if *trayFn && !*selftest {
		app.Tray = &tray.Config{
			Tooltip: "appkit demo",
			Items: []tray.Item{
				{Label: "Show", OnClick: func() {
					w.Unminimize()
					w.Show()
				}},
				{Label: "Hide", OnClick: func() { w.Hide() }},
				{Separator: true},
				{Label: "Quit", OnClick: app.Quit},
			},
		}
	}

	app.FS = assetsRoot()
	app.HTTP = *httpFn

	themeMu := &sync.Mutex{}
	theme := "ocean"
	readTheme := func() (string, error) {
		themeMu.Lock()
		defer themeMu.Unlock()
		return theme, nil
	}
	writeTheme := func(s string) error {
		s = strings.TrimSpace(s)
		if s == "" {
			return errors.New("theme must not be empty")
		}
		themeMu.Lock()
		theme = s
		themeMu.Unlock()
		log.Printf("demo: theme -> %q", s)
		return nil
	}

	counterMu := &sync.Mutex{}
	count := 0
	readCounter := func() (int, error) {
		counterMu.Lock()
		defer counterMu.Unlock()
		count++
		return count, nil
	}

	setpMu := &sync.Mutex{}
	setpoint := 0
	writeSetpoint := func(p int) error {
		setpMu.Lock()
		setpoint = p
		setpMu.Unlock()
		return nil
	}
	readSetpoint := func() (int, error) {
		setpMu.Lock()
		defer setpMu.Unlock()
		return setpoint, nil
	}

	pairValue := "left"

	var d *windowDemo
	view := &appkit.View{
		Debug:  true,
		Frame:  *frame,
		Width:  1000,
		Height: 680,
		Bind: map[string]any{
			"demoAdd":    func(a, b float64) float64 { return a + b },
			"demoEcho":   func(s string) string { return s },
			"demo.clock": func() string { return time.Now().Format("15:04:05.000") },
			"demo.mark":  func(s string) string { return "marked: " + s },
			"demo.meta":  map[string]any{"app": "appkit demo", "ui": "app://app/index.html"},
			"demo.pair": [2]any{
				func() (string, error) { return pairValue, nil },
				func(v string) error { pairValue = v; return nil },
			},
			"demo.theme":     [2]any{readTheme, writeTheme},
			"demo.counter":   readCounter,
			"demo.setp":      writeSetpoint,
			"demo.setpState": readSetpoint,
			"demoEmitGo": func(msg string) {
				_ = d.w.Emit("demo:goEvent", "from Go: "+msg)
			},
			"demoCopyText": func(s string) error { return d.app.Copy([]byte(s)) },
			"demoPaste": func() (string, error) {
				b, err := d.app.Paste()
				return string(b), err
			},
			"demoNotify": func() string {
				if err := d.app.Notify("appkit demo", "Hello from the appkit demo window!"); err != nil {
					return err.Error()
				}
				return ""
			},
			"demoAutostartState": func() autostartInfo {
				a := d.app.Autostart()
				return autostartInfo{
					Enabled: a.Enabled(),
					Backend: a.Backend(),
					Path:    a.Path(),
				}
			},
			"demoAutostartSet": func(on bool, args []string) error {
				a := d.app.Autostart()
				if on {
					return a.Enable(args...)
				}
				return a.Disable()
			},
			"demoDialog": func(kind string) []string {
				opts := dialog.Options{Title: "appkit demo"}
				switch kind {
				case "save":
					opts.Type = dialog.TypeSave
					opts.Filename = "demo.txt"
				case "dir":
					opts.Type = dialog.TypeDirectory
				default:
					opts.Type = dialog.TypeOpen
				}
				paths, _ := d.w.Dialog(opts)
				return paths
			},
			"demoOpen": func(rawurl string) string {
				if err := d.app.Open(rawurl); err != nil {
					return err.Error()
				}
				return ""
			},
			"demoReveal": func() string {
				exe, err := os.Executable()
				if err != nil {
					return err.Error()
				}
				if err := d.app.Reveal(exe); err != nil {
					return err.Error()
				}
				return ""
			},
			"demoExit": func() {
				close(d.close)
				d.app.Quit()
			},
			"demoMinimize": func() {
				w.Minimize()
			},
			"demoMaximize": func() bool {
				target := !w.Maximized()
				w.Unminimize()
				if target {
					w.Maximize()
				} else {
					w.Unmaximize()
				}
				for i := 0; i < 25 && w.Maximized() != target; i++ {
					time.Sleep(10 * time.Millisecond)
				}
				return w.Maximized()
			},
			"demoReady": func() {
				select {
				case <-d.self.ready:
				default:
					close(d.self.ready)
				}
			},
			"demoReport": func(reports []testReport) {
				select {
				case d.self.reports <- reports:
				default:
				}
				select {
				case <-d.self.done:
				default:
					close(d.self.done)
				}
				d.app.Quit()
			},
		},
	}
	d = &windowDemo{
		w:     view,
		app:   app,
		close: make(chan struct{}),
		self: &selfTest{
			active:  *selftest,
			ready:   make(chan struct{}),
			done:    make(chan struct{}),
			reports: make(chan []testReport, 1),
		},
	}

	page := "app://"
	if d.self.active {
		page += "#selftest"
	}
	view.URL = page

	if err := app.Show(view); err != nil {
		log.Fatalf("demo: %v", err)
	}
	w = view

	log.Printf("demo: webview backend: %s", app.Backend())

	d.w.On("demo:uiGreet", func(args ...json.RawMessage) {
		text := "?"
		if len(args) > 0 {
			var s string
			if json.Unmarshal(args[0], &s) == nil {
				text = s
			}
		}
		log.Printf("demo: JS greeted with %q", text)
		_ = d.w.Emit("demo:goEvent", "pong:"+text)
	})

	if d.self.active {
		go func() {
			select {
			case <-d.self.ready:
			case <-time.After(20 * time.Second):
				log.Println("demo: page never became ready; aborting self test")
				d.app.Quit()
				return
			}
			select {
			case <-d.self.done:
			case <-time.After(60 * time.Second):
				log.Println("demo: self test timed out")
				d.app.Quit()
			}
		}()
	}

	code := 0
	if err := d.app.Wait(); err != nil {
		log.Printf("demo: wait: %v", err)
		code = 1
	} else if !d.self.active {
		code = 0
	} else {
		select {
		case reports := <-d.self.reports:
			code = summarize(reports)
		default:
			log.Println("demo: no self-test report received")
			code = 1
		}
	}

	w.Close()
	os.Exit(code)
}
func summarize(reports []testReport) int {
	passed := 0
	for _, r := range reports {
		if r.Pass {
			passed++
		} else {
			fmt.Printf("FAIL %s: %s\n", r.Name, r.Detail)
		}
	}
	fmt.Printf("selftest %d/%d passed\n", passed, len(reports))
	if passed == len(reports) && len(reports) > 0 {
		return 0
	}
	return 1
}

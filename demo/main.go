// Command demo is the appkit showcase: a real window with custom chrome that
// exercises windows, bindings, events, dialogs, clipboard, the tray and
// notifications.
package main

import (
	"embed"
	"errors"
	"flag"
	"io/fs"
	"log"
	"strings"
	"sync"

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

func main() {
	var (
		debug  = flag.Bool("debug", false, "open the platform web inspector / dev tools")
		trayFn = flag.Bool("tray", false, "add a tray menu (Show/Hide/Quit) to the window")
	)
	flag.Parse()

	_ = debug

	app := &appkit.App{Name: "appkit demo", Exit: true}

	var view *appkit.View
	if *trayFn {
		app.Tray = &tray.Config{
			Tooltip: "appkit demo",
			Items: []tray.Item{
				{Label: "Show", OnClick: func() {
					view.Unminimize()
					view.Show()
				}},
				{Label: "Hide", OnClick: func() { view.Hide() }},
				{Separator: true},
				{Label: "Quit", OnClick: app.Quit},
			},
		}
	}

	app.FS = assetsRoot()

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

	view = &appkit.View{
		Width:  1000,
		Height: 680,
		URL:    "app://index.html",
		Bind: map[string]any{
			"app.name": app.Name,
			"theme":    [2]any{readTheme, writeTheme},
			"tick": func() (int, error) {
				counterMu.Lock()
				defer counterMu.Unlock()
				count++
				return count, nil
			},
			"open": func() (string, error) {
				paths, err := view.Dialog(dialog.Options{
					Type:  dialog.TypeOpen,
					Title: "Pick a file",
				})
				if err != nil {
					return "", err
				}
				if len(paths) == 0 {
					return "", nil
				}
				if err := app.Notify("appkit demo", "You picked "+paths[0]); err != nil {
					return "", err
				}
				return paths[0], nil
			},
			"copy": func(text string) error {
				return app.Copy([]byte(text))
			},
			"paste": func() (string, error) {
				b, err := app.Paste()
				return string(b), err
			},
			"quit": app.Quit,
		},
	}

	if err := app.Show(view); err != nil {
		log.Fatal(err)
	}
	defer view.Close()

	if err := app.Wait(); err != nil {
		log.Fatal(err)
	}
}

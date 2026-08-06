// Command demo is the appkit showcase: a frameless window with custom chrome
// that exercises windows, bindings and events.
package main

import (
	"embed"
	"io/fs"
	"log"
	"strings"
	"sync"

	"github.com/malivvan/appkit"
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
	app := &appkit.App{Name: "appkit demo", Exit: true}
	app.FS = assetsRoot()

	themeMu := &sync.Mutex{}
	theme := "ocean"

	counterMu := &sync.Mutex{}
	count := 0

	view := &appkit.View{
		Width:  900,
		Height: 620,
		URL:    "app://index.html",
		Bind: map[string]any{
			"app.name": app.Name,
			"theme": [2]any{
				func() (string, error) {
					themeMu.Lock()
					defer themeMu.Unlock()
					return theme, nil
				},
				func(name string) error {
					name = strings.TrimSpace(name)
					if name == "" {
						return errEmptyTheme
					}
					themeMu.Lock()
					theme = name
					themeMu.Unlock()
					return nil
				},
			},
			"tick": func() (int, error) {
				counterMu.Lock()
				defer counterMu.Unlock()
				count++
				return count, nil
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

var errEmptyTheme = errString("theme must not be empty")

type errString string

func (e errString) Error() string { return string(e) }

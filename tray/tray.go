// Package tray puts a status icon with a menu in the OS tray/menu bar. Linux
// uses a D-Bus StatusNotifierItem, so no desktop is excluded; unsupported
// platforms report ErrUnsupported.
package tray

import "errors"

// ErrUnsupported is returned when the platform has no tray.
var ErrUnsupported = errors.New("tray: not supported on this platform")

// ErrAlreadyRunning is returned when an icon is already installed (only one tray
// per process).
var ErrAlreadyRunning = errors.New("tray: already running")

// Config describes the icon and its menu.
type Config struct {
	Title string

	Tooltip string

	OnClick func()

	OnDoubleClick func()

	OnRightClick func()

	Items []Item
}

// Item is one menu entry. A Separator draws a divider; a Checkbox toggles; a
// Submenu nests further Items.
type Item struct {
	Label string

	Icon []byte

	Checkbox bool

	Checked bool

	Disabled bool

	Separator bool

	Submenu []Item

	OnClick func()
}

// Set installs the icon and menu, which are rendered until Remove.
func Set(id string, icon []byte, conf Config) error { return set(id, icon, conf) }

// Remove tears down the icon.
func Remove() { remove() }

// Run installs the icon and blocks serving its event loop until Stop.
func Run(id string, icon []byte, conf Config) error { return run(id, icon, conf) }

// Stop stops a Run loop.
func Stop() { stop() }

// Bounds reports the icon's on-screen rectangle, or all zeros if it is not
// currently visible.
func Bounds() (x, y, w, h int) { return bounds() }

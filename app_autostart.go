package appkit

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

const (
	autostartBackendSMAppService = "smappservice"
	autostartBackendLaunchAgent  = "launchagent"
	autostartBackendRegistryRun  = "registry-run"
	autostartBackendXDGAutostart = "xdg-autostart"
)

// ErrAutostartNotSupported is returned when the platform has no autostart
// mechanism (or Autostart is nil).
var ErrAutostartNotSupported = errors.New("appkit: autostart is not supported on this platform")

type autostartDriver interface {
	enable(id string, args []string) error
	disable() error
	status() (enabled bool, path, backend string)
}

// Autostart controls launching the app at login. Obtain one from App.Autostart;
// the concrete backend (LaunchAgent/SMAppService, registry Run key, or an XDG
// .desktop file) is chosen per platform.
type Autostart struct {
	cfg  appSetup
	impl autostartDriver
}

// Autostart returns a handle for configuring launch-at-login for this app. The
// app's ID (or a slug of its Name) names the login entry.
func (a *App) Autostart() *Autostart {
	cfg := snapshotSetup(a)
	return &Autostart{cfg: cfg, impl: newAutostartDriver(cfg)}
}

// Enabled reports whether a login entry currently exists.
func (a *Autostart) Enabled() bool {
	if a == nil || a.impl == nil {
		return false
	}
	enabled, _, _ := a.impl.status()
	return enabled
}

// Enable installs the login entry, passing args to the app on each launch.
func (a *Autostart) Enable(args ...string) error {
	if a == nil || a.impl == nil {
		return ErrAutostartNotSupported
	}
	id, err := autostartLabel(a.cfg)
	if err != nil {
		return err
	}
	return a.impl.enable(id, args)
}

// Disable removes the login entry.
func (a *Autostart) Disable() error {
	if a == nil || a.impl == nil {
		return ErrAutostartNotSupported
	}
	return a.impl.disable()
}

// Path reports the login entry's on-disk location (plist, .desktop, registry
// value), or "" when disabled.
func (a *Autostart) Path() string {
	if a == nil || a.impl == nil {
		return ""
	}
	_, path, _ := a.impl.status()
	return path
}

// Backend names the mechanism in use (one of "smappservice", "launchagent",
// "registry-run", "xdg-autostart"), or "" when disabled.
func (a *Autostart) Backend() string {
	if a == nil || a.impl == nil {
		return ""
	}
	_, _, backend := a.impl.status()
	return backend
}

const defaultAutostartSlug = "appkit-app"

func autostartLabel(cfg appSetup) (string, error) {
	if cfg.ID != "" {
		if err := validateAutostartLabel(cfg.ID); err != nil {
			return "", err
		}
		return cfg.ID, nil
	}
	if cfg.Name != "" {
		return slugifyAutostart(cfg.Name), nil
	}
	if exe, err := os.Executable(); err == nil {
		if id := slugifyAutostart(filepath.Base(exe)); id != "" && id != defaultAutostartSlug {
			return id, nil
		}
	}
	return defaultAutostartSlug, nil
}

func slugifyAutostart(name string) string {
	var b strings.Builder
	b.Grow(len(name))
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9',
			r == '.', r == '_', r == '-':
			b.WriteRune(r)
		case r >= 'A' && r <= 'Z':
			b.WriteRune(r + ('a' - 'A'))
		case r == ' ', r == '\t':
			b.WriteByte('-')
		}
	}
	out := strings.Trim(b.String(), "-._")
	if out == "" {
		return defaultAutostartSlug
	}
	return out
}

func validateAutostartLabel(id string) error {
	if id == "" {
		return nil
	}
	if len(id) > 200 {
		return fmt.Errorf("appkit: autostart identifier too long (max 200): %q", id)
	}
	for _, r := range id {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z',
			r >= '0' && r <= '9',
			r == '.', r == '_', r == '-':
		default:
			return fmt.Errorf("appkit: autostart identifier %q contains invalid character %q (allowed: A-Za-z0-9._-)", id, r)
		}
	}
	return nil
}

func executablePath() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("appkit: autostart: get executable path: %w", err)
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		return resolved, nil
	}
	return exe, nil
}

// atomicWriteFile writes data to a temp file in the target directory and renames
// it into place, so a reader never observes a partially written file.
func atomicWriteFile(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	cleanup := func() { _ = os.Remove(tmpName) }
	n, err := tmp.Write(data)
	if err == nil && n != len(data) {
		err = io.ErrShortWrite
	}
	if err != nil {
		_ = tmp.Close()
		cleanup()
		return err
	}
	if err := tmp.Chmod(perm); err != nil {
		_ = tmp.Close()
		cleanup()
		return err
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		cleanup()
		return err
	}
	return nil
}

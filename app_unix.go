//go:build linux

package appkit

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/draw"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func setAppIcon(pngData []byte, name string) error {
	if len(pngData) == 0 {
		return errors.New("appkit: the application icon is empty")
	}
	src, err := png.Decode(bytes.NewReader(pngData))
	if err != nil {
		return fmt.Errorf("appkit: application icon: %w", err)
	}
	b := src.Bounds()
	img := image.NewNRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	draw.Draw(img, img.Bounds(), src, b.Min, draw.Src)
	if gtk4 {
		return gtk4InstallAppIcon(img.Pix, b.Dx(), b.Dy())
	}
	if os.Getenv("WAYLAND_DISPLAY") != "" {
		if id := installWaylandAppID(name, img); id != "" && gSetPrgname != nil {
			gSetPrgname(id)
		}
	}
	return gtk3InstallAppIcon(img.Pix, b.Dx(), b.Dy())
}

func installWaylandAppID(appName string, img *image.NRGBA) string {
	id := desktopEntryID(appName)
	if id == "" {
		return ""
	}
	dataHome, err := xdgDataDir()
	if err != nil {
		return ""
	}
	sz := img.Bounds().Dx()
	if img.Bounds().Dy() != sz || sz <= 0 {
		return ""
	}
	sizes := iconSizeLadder(sz)
	if len(sizes) == 0 {
		return ""
	}
	pruneStaleIconSizes(dataHome, id, sizes)
	changed := false
	for _, s := range sizes {
		out := img
		if s != sz {
			out = downscaleIcon(img, s)
		}
		pngBytes := encodePNGImage(out)
		if len(pngBytes) == 0 {
			return ""
		}
		iconPath := filepath.Join(dataHome, "icons", "hicolor",
			fmt.Sprintf("%dx%d", s, s), "apps", id+".png")
		wrote, err := writeFileIfDifferent(iconPath, pngBytes)
		if err != nil {
			return ""
		}
		changed = changed || wrote
	}
	exe, _ := os.Executable()
	displayName := strings.TrimSpace(appName)
	if displayName == "" {
		displayName = id
	}
	desktop := "[Desktop Entry]\n" +
		"Type=Application\n" +
		"Name=" + strings.ReplaceAll(displayName, "\n", " ") + "\n" +
		"Icon=" + id + "\n" +
		"Exec=" + quoteDesktopValue(exe) + "\n" +
		"Terminal=false\n" +
		"Hidden=true\n" +
		"NoDisplay=true\n"
	desktopPath := filepath.Join(dataHome, "applications", id+".desktop")
	wroteDesktop, err := writeFileIfDifferent(desktopPath, []byte(desktop))
	if err != nil {
		return ""
	}
	if changed || wroteDesktop {
		refreshDesktopCaches()
	}
	return id
}

func refreshDesktopCaches() {
	for _, tool := range []string{"kbuildsycoca6", "kbuildsycoca5", "kbuildsycoca"} {
		bin, err := exec.LookPath(tool)
		if err != nil {
			continue
		}
		cmd := exec.Command(bin, "--noincremental")
		_ = cmd.Start()
		return
	}
}

func desktopEntryID(name string) string {
	id := strings.ToLower(strings.TrimSpace(name))
	if id == "" {
		if exe, err := os.Executable(); err == nil {
			id = strings.ToLower(filepath.Base(exe))
		}
	}
	var b strings.Builder
	lastDash := false
	for _, r := range id {
		ok := (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-'
		if ok {
			b.WriteRune(r)
			lastDash = r == '-'
		} else if !lastDash {
			b.WriteByte('-')
			lastDash = true
		}
	}
	out := strings.Trim(b.String(), "-")
	if out == "" {
		out = "app"
	}
	return out
}

func xdgDataDir() (string, error) {
	if d := os.Getenv("XDG_DATA_HOME"); d != "" {
		return d, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".local", "share"), nil
}

func encodePNGImage(img *image.NRGBA) []byte {
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil
	}
	return buf.Bytes()
}

func writeFileIfDifferent(path string, data []byte) (bool, error) {
	if old, err := os.ReadFile(path); err == nil && bytes.Equal(old, data) {
		return false, nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return false, err
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return false, err
	}
	return true, nil
}

var desktopIconSizes = []int{512, 256, 128, 64, 48, 32, 22}

func iconSizeLadder(src int) []int {
	if src <= 0 {
		return nil
	}
	sizes := make([]int, 0, len(desktopIconSizes)+1)
	for _, s := range desktopIconSizes {
		if s <= src {
			sizes = append(sizes, s)
		}
	}
	if len(sizes) == 0 || sizes[0] != src {
		sizes = append([]int{src}, sizes...)
	}
	return sizes
}

func pruneStaleIconSizes(dataHome, id string, sizes []int) {
	keep := make(map[string]bool, len(sizes))
	for _, s := range sizes {
		keep[fmt.Sprintf("%dx%d", s, s)] = true
	}
	root := filepath.Join(dataHome, "icons", "hicolor")
	dirs, err := os.ReadDir(root)
	if err != nil {
		return
	}
	for _, d := range dirs {
		if !d.IsDir() || keep[d.Name()] {
			continue
		}
		p := filepath.Join(root, d.Name(), "apps", id+".png")
		if _, err := os.Stat(p); err == nil {
			_ = os.Remove(p)
		}
	}
}

func quoteDesktopValue(s string) string {
	if s == "" {
		return `""`
	}
	for _, r := range s {
		if r <= ' ' || r == '"' || r == '\'' || r == '\\' {
			return `"` + strings.ReplaceAll(s, `"`, `\"`) + `"`
		}
	}
	return s
}

func openURL(rawurl string) error {
	return xdgOpen(rawurl)
}

func revealFile(absPath string) error {
	return xdgOpen(filepath.Dir(absPath))
}

func xdgOpen(arg string) error {
	err := exec.Command("xdg-open", arg).Run()
	if err != nil {
		return fmt.Errorf("open: xdg-open %q: %w", arg, err)
	}
	return nil
}

type xdgAutostart struct {
	name string
}

func newAutostartDriver(cfg appSetup) autostartDriver {
	name := cfg.Name
	if name == "" {
		if exe, err := os.Executable(); err == nil {
			name = filepath.Base(exe)
		}
	}
	return &xdgAutostart{name: name}
}

func (a *xdgAutostart) enable(id string, args []string) error {
	if err := validateExecToken(id); err != nil {
		return fmt.Errorf("appkit: autostart identifier: %w", err)
	}
	for i, arg := range args {
		if err := validateExecToken(arg); err != nil {
			return fmt.Errorf("appkit: autostart argument %d: %w", i, err)
		}
	}
	exe, err := executablePath()
	if err != nil {
		return err
	}
	if err := validateExecToken(exe); err != nil {
		return fmt.Errorf("appkit: autostart executable path: %w", err)
	}
	dir, err := a.autostartDir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("appkit: autostart: create autostart dir: %w", err)
	}
	path := filepath.Join(dir, id+".desktop")
	if existing, ferr := a.findDesktopFile(dir); ferr == nil && existing != "" && existing != path {
		_ = os.Remove(existing)
	}
	body := desktopEntry(a.name, exe, args)
	if err := atomicWriteFile(path, []byte(body), 0o644); err != nil {
		return fmt.Errorf("appkit: autostart: write desktop file %s: %w", path, err)
	}
	return nil
}

func (a *xdgAutostart) disable() error {
	dir, err := a.autostartDir()
	if err != nil {
		return err
	}
	path, err := a.findDesktopFile(dir)
	if err != nil {
		return err
	}
	if path == "" {
		return nil
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("appkit: autostart: remove desktop file: %w", err)
	}
	return nil
}

func (a *xdgAutostart) status() (bool, string, string) {
	dir, err := a.autostartDir()
	if err != nil {
		return false, "", ""
	}
	path, err := a.findDesktopFile(dir)
	if err != nil || path == "" {
		return false, "", ""
	}
	return true, path, autostartBackendXDGAutostart
}

func (a *xdgAutostart) autostartDir() (string, error) {
	cfg := os.Getenv("XDG_CONFIG_HOME")
	if cfg == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("appkit: autostart: %w", err)
		}
		cfg = filepath.Join(home, ".config")
	}
	return filepath.Join(cfg, "autostart"), nil
}

func (a *xdgAutostart) findDesktopFile(dir string) (string, error) {
	exe, err := executablePath()
	if err != nil {
		return "", err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", fmt.Errorf("appkit: autostart: read autostart dir: %w", err)
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".desktop") {
			continue
		}
		full := filepath.Join(dir, e.Name())
		data, err := os.ReadFile(full)
		if err != nil {
			continue
		}
		if parseDesktopExec(string(data)) == exe {
			return full, nil
		}
	}
	return "", nil
}

func desktopEntry(appName, exe string, args []string) string {
	if appName == "" {
		appName = filepath.Base(exe)
	}
	var b strings.Builder
	b.WriteString("[Desktop Entry]\n")
	b.WriteString("Type=Application\n")
	fmt.Fprintf(&b, "Name=%s\n", flattenDesktopValue(appName))
	b.WriteString("Exec=" + quoteExecToken(exe))
	for _, a := range args {
		b.WriteString(" " + quoteExecToken(a))
	}
	b.WriteString("\n")
	b.WriteString("X-GNOME-Autostart-enabled=true\n")
	b.WriteString("Hidden=false\n")
	b.WriteString("NoDisplay=true\n")
	b.WriteString("Terminal=false\n")
	return b.String()
}

func validateExecToken(s string) error {
	for _, r := range s {
		if r == '\t' || r == ' ' {
			continue
		}
		if r < 0x20 || r == 0x7f {
			return fmt.Errorf("control character %U not allowed in Exec field", r)
		}
	}
	return nil
}

func quoteExecToken(s string) string {
	needQuote := false
	var b strings.Builder
	for _, r := range s {
		switch r {
		case '"', '`', '$', '\\':
			b.WriteByte('\\')
			b.WriteRune(r)
			needQuote = true
		case ' ', '\t':
			b.WriteRune(r)
			needQuote = true
		default:
			b.WriteRune(r)
		}
	}
	if needQuote {
		return `"` + b.String() + `"`
	}
	return b.String()
}

func flattenDesktopValue(s string) string {
	s = strings.ReplaceAll(s, "\r", " ")
	s = strings.ReplaceAll(s, "\n", " ")
	return strings.TrimSpace(s)
}

func parseDesktopExec(contents string) string {
	for _, line := range strings.Split(contents, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "Exec=") {
			continue
		}
		val := strings.TrimSpace(strings.TrimPrefix(line, "Exec="))
		if strings.HasPrefix(val, `"`) {
			end := strings.Index(val[1:], `"`)
			if end < 0 {
				return ""
			}
			return unquoteDesktopToken(val[1 : 1+end])
		}
		if i := strings.IndexAny(val, " \t"); i >= 0 {
			return val[:i]
		}
		return val
	}
	return ""
}

func unquoteDesktopToken(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+1 < len(s) {
			b.WriteByte(s[i+1])
			i++
			continue
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

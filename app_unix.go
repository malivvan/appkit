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

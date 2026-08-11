//go:build linux

package notify

import (
	"bytes"
	"fmt"
	"image"
	"image/draw"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"sync"

	"github.com/godbus/dbus/v5"
)

const (
	notifInterface = "org.freedesktop.Notifications"
	notifPath      = "/org/freedesktop/Notifications"

	kdialogPopupSeconds = 5
)

var (
	connMu sync.Mutex
	conn   *dbus.Conn
)

type iconPayload struct {
	Width         int32
	Height        int32
	RowStride     int32
	HasAlpha      bool
	BitsPerSample int32
	Channels      int32
	Data          []byte
}

func sessionBus() (*dbus.Conn, error) {
	connMu.Lock()
	defer connMu.Unlock()
	if conn != nil {
		if conn.Connected() {
			return conn, nil
		}
		_ = conn.Close()
		conn = nil
	}
	c, err := dbus.ConnectSessionBus()
	if err != nil {
		return nil, fmt.Errorf("notify: connect to session bus: %w", err)
	}
	conn = c
	return conn, nil
}

func show(name, title, message string, opts Options) error {
	if name == "" {
		name = filepath.Base(os.Args[0])
	}
	err := notifyViaDBus(name, title, message, opts)
	if err == nil {
		return nil
	}
	err1 := notifySendCLI(name, title, message, opts)
	if err1 == nil {
		return nil
	}
	err2 := kdialogAlert(title, message, opts)
	if err2 == nil {
		return nil
	}
	return fmt.Errorf("%w: session bus: %v; notify-send: %v; kdialog: %v",
		ErrUnavailable, err, err1, err2)
}

func notifyViaDBus(name, title, message string, opts Options) error {
	c, err := sessionBus()
	if err != nil {
		return err
	}
	appIcon := opts.Icon
	if appIcon != "" {
		if _, err := os.Stat(appIcon); err == nil {
			appIcon, _ = filepath.Abs(appIcon)
		}
	}
	level := opts.Urgency.level()
	hints := map[string]dbus.Variant{
		"urgency": dbus.MakeVariant(byte(level)),
	}
	if level == 2 {
		hints["sound-name"] = dbus.MakeVariant("bell")
	}
	if len(opts.IconData) > 0 {
		rgba, err := rgbaFromPNG(opts.IconData)
		if err != nil {
			return fmt.Errorf("notify: decode Options.IconData: %w", err)
		}
		hints["image-data"] = dbus.MakeVariant(iconPayloadHint(rgba))
	}
	obj := c.Object(notifInterface, notifPath)
	call := obj.Call(notifInterface+".Notify", 0,
		name,
		uint32(0),
		appIcon,
		title,
		message,
		[]string{},
		hints,
		int32(-1),
	)
	if call.Err != nil {
		connMu.Lock()
		if conn == c {
			_ = conn.Close()
			conn = nil
		}
		connMu.Unlock()
		return fmt.Errorf("notify: %w", call.Err)
	}
	return nil
}

func notifySendCLI(name, title, message string, opts Options) error {
	bin, err := exec.LookPath("notify-send")
	if err != nil {
		return err
	}
	icon, cleanup, err := iconPathForCLI(opts)
	if err != nil {
		return err
	}
	defer cleanup()
	args := []string{title, message, "-a", name}
	if icon != "" {
		args = append(args, "-i", icon)
	}
	args = append(args, "-t", "-1", "-u", urgencyLabel(opts.Urgency.level()))
	return exec.Command(bin, args...).Run()
}

func kdialogAlert(title, message string, opts Options) error {
	bin, err := exec.LookPath("kdialog")
	if err != nil {
		return err
	}
	icon, cleanup, err := iconPathForCLI(opts)
	if err != nil {
		return err
	}
	defer cleanup()
	args := []string{"--title", title, "--passivepopup", message, strconv.Itoa(kdialogPopupSeconds)}
	if icon != "" {
		args = append(args, "--icon", icon)
	}
	return exec.Command(bin, args...).Run()
}

func iconPayloadHint(img *image.RGBA) iconPayload {
	b := img.Bounds()
	height := b.Dy()
	stride := img.Stride
	data := img.Pix
	if stride*height < len(data) {
		data = data[:stride*height]
	}
	return iconPayload{
		Width:         int32(b.Dx()),
		Height:        int32(height),
		RowStride:     int32(stride),
		HasAlpha:      true,
		BitsPerSample: 8,
		Channels:      4,
		Data:          data,
	}
}

func rgbaFromPNG(data []byte) (*image.RGBA, error) {
	src, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	if img, ok := src.(*image.RGBA); ok {
		return img, nil
	}
	b := src.Bounds()
	img := image.NewRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	draw.Draw(img, img.Bounds(), src, b.Min, draw.Src)
	return img, nil
}

func iconPathForCLI(opts Options) (icon string, cleanup func(), err error) {
	cleanup = func() {}
	if len(opts.IconData) > 0 {
		f, err := os.CreateTemp("", "appkit-notify-*.png")
		if err != nil {
			return "", cleanup, fmt.Errorf("notify: write Options.IconData to temp file: %w", err)
		}
		if _, err := f.Write(opts.IconData); err != nil {
			_ = f.Close()
			_ = os.Remove(f.Name())
			return "", cleanup, fmt.Errorf("notify: write Options.IconData to temp file: %w", err)
		}
		if err := f.Close(); err != nil {
			_ = os.Remove(f.Name())
			return "", cleanup, fmt.Errorf("notify: write Options.IconData to temp file: %w", err)
		}
		return f.Name(), func() { _ = os.Remove(f.Name()) }, nil
	}
	if opts.Icon != "" {
		if _, err := os.Stat(opts.Icon); err == nil {
			icon, _ = filepath.Abs(opts.Icon)
		} else {
			icon = opts.Icon
		}
	}
	return icon, cleanup, nil
}

func urgencyLabel(level int) string {
	switch level {
	case 0:
		return "low"
	case 2:
		return "critical"
	default:
		return "normal"
	}
}

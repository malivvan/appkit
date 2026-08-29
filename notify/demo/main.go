// Command demo is a minimal notification showcase.
package main

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"runtime"
	"time"

	"github.com/malivvan/appkit/notify"
)

const source = "appkit notify demo"

func main() {
	icon, err := makeIconPNG()
	if err != nil {
		fmt.Println("icon generation error:", err)
		os.Exit(1)
	}

	ok := true
	report := func(step string, err error) {
		switch {
		case err == nil:
			fmt.Printf("[%s] %s\n", timestamp(), step)
		case errors.Is(err, notify.ErrUnsupported):
			fmt.Printf("[%s] %s - skipped: %v\n", timestamp(), step, err)
		case errors.Is(err, notify.ErrUnavailable):
			fmt.Printf("[%s] %s - skipped: no notification service in this environment (%v)\n", timestamp(), step, err)
		default:
			fmt.Printf("[%s] %s - FAILED: %v\n", timestamp(), step, err)
			ok = false
		}
	}

	report("plain notification", notify.Show(source, "Information", "This is an informational message from appkit."))
	time.Sleep(1500 * time.Millisecond)

	opts := notify.Options{}
	if runtime.GOOS == "windows" {
		opts.Icon = "warning"
	} else {
		opts.IconData = icon
	}
	report("notification with a custom icon", notify.ShowOpts(source, "Custom icon", "The icon beside this message was generated in memory.", opts))
	time.Sleep(1500 * time.Millisecond)

	report("alert (critical + attention sound)", notify.Alert(source, "Alert", "This notification demands attention.", notify.Options{}))
	time.Sleep(500 * time.Millisecond)

	report("beep", notify.Beep(notify.DefaultFreq, 150))

	time.Sleep(1500 * time.Millisecond)
	fmt.Println("Done.")
	if !ok {
		os.Exit(1)
	}
}

func timestamp() string {
	return time.Now().Format("15:04:05")
}

func makeIconPNG() ([]byte, error) {
	const size = 48
	img := image.NewRGBA(image.Rect(0, 0, size, size))
	r := size/2 - 2
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			dx, dy := x-size/2, y-size/2
			if dx*dx+dy*dy <= r*r {
				img.SetRGBA(x, y, color.RGBA{R: 0xE8, G: 0x7D, B: 0x1E, A: 0xFF})
			}
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

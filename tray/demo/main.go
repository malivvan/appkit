// Command demo is a minimal tray showcase.
package main

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"

	"github.com/malivvan/appkit/notify"
	"github.com/malivvan/appkit/tray"
)

func main() {
	icon := generateIcon(22, color.RGBA{R: 0, G: 180, B: 80, A: 255})

	cfg := tray.Config{
		Tooltip:       "appkit tray test",
		OnClick:       func() { fmt.Println("Left click!") },
		OnDoubleClick: func() { fmt.Println("Double click!") },
		OnRightClick:  func() { fmt.Println("Right click!") },
		Items: []tray.Item{
			{Label: "Hello", OnClick: func() { fmt.Println("Hello clicked!") }},
			{Label: "Show Notification", OnClick: func() {
				fmt.Println("Sending notification...")
				if err := notify.Show("appkit tray demo", "appkit", "Hello from tray!"); err != nil {
					fmt.Println("Notify error:", err)
				}
			}},
			{Separator: true},
			{Label: "More...", Submenu: []tray.Item{
				{Label: "Sub Item 1", OnClick: func() { fmt.Println("Sub 1") }},
				{Label: "Sub Item 2", OnClick: func() { fmt.Println("Sub 2") }},
			}},
			{Label: "Check me", Checkbox: true, Checked: false, OnClick: func() {
				fmt.Println("Checkbox toggled")
			}},
			{Separator: true},
			{Label: "Quit", OnClick: func() {
				fmt.Println("Quit clicked, removing tray...")
				tray.Stop()
				os.Exit(0)
			}},
		},
	}

	fmt.Println("Tray running. Right-click the tray icon for the menu.")
	fmt.Println("Click Quit to exit.")
	if err := tray.Run("appkit-tray-demo", icon, cfg); err != nil {
		fmt.Println("Run error:", err)
	}
}

func generateIcon(size int, c color.RGBA) []byte {
	img := image.NewRGBA(image.Rect(0, 0, size, size))
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			if x == 0 || x == size-1 || y == 0 || y == size-1 {
				img.SetRGBA(x, y, color.RGBA{R: 255, G: 255, B: 255, A: 255})
			} else {
				img.SetRGBA(x, y, c)
			}
		}
	}
	var buf bytes.Buffer
	_ = png.Encode(&buf, img)
	return buf.Bytes()
}

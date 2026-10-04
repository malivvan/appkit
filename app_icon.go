package appkit

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"os"

	_ "embed"
)

// embeddedIcon is the built-in fallback app icon (app.png), used when App.Icon
// is not set.
//
//go:embed app.png
var embeddedIcon []byte

// trayIconSize is the square edge length the app icon is downscaled to before
// being handed to the tray.
const trayIconSize = 32

func scaleIconPNG(pngData []byte, size int) []byte {
	if size <= 0 {
		return nil
	}
	src, err := png.Decode(bytes.NewReader(pngData))
	if err != nil {
		return nil
	}
	sb := src.Bounds()
	dst := image.NewRGBA(image.Rect(0, 0, size, size))
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			x0, x1 := x*sb.Dx()/size, (x+1)*sb.Dx()/size
			y0, y1 := y*sb.Dy()/size, (y+1)*sb.Dy()/size
			if x1 <= x0 || y1 <= y0 {
				x1, y1 = x0+1, y0+1
			}
			var r, g, b, a uint64
			for yy := y0; yy < y1; yy++ {
				for xx := x0; xx < x1; xx++ {
					c := color.NRGBAModel.Convert(src.At(xx, yy)).(color.NRGBA)
					r += uint64(c.R)
					g += uint64(c.G)
					b += uint64(c.B)
					a += uint64(c.A)
				}
			}
			n := uint64((y1 - y0) * (x1 - x0))
			if n == 0 {
				n = 1
			}
			dst.SetRGBA(x, y, color.RGBA{R: uint8(r / n), G: uint8(g / n), B: uint8(b / n), A: uint8(a / n)})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, dst); err != nil {
		return nil
	}
	return buf.Bytes()
}

//lint:ignore U1000 macOS renders the Dock icon at any size natively, so only the Unix and Windows icon installers (app_unix.go, app_windows.go) downscale through this shared helper.
func downscaleIcon(src *image.NRGBA, size int) *image.NRGBA {
	sw, sh := src.Bounds().Dx(), src.Bounds().Dy()
	dst := image.NewNRGBA(image.Rect(0, 0, size, size))
	for y := 0; y < size; y++ {
		y0 := y * sh / size
		y1 := (y + 1) * sh / size
		if y1 <= y0 {
			y1 = y0 + 1
		}
		for x := 0; x < size; x++ {
			x0 := x * sw / size
			x1 := (x + 1) * sw / size
			if x1 <= x0 {
				x1 = x0 + 1
			}
			var r, g, b, a uint64
			for yy := y0; yy < y1; yy++ {
				row := src.PixOffset(x0, yy)
				for xx := x0; xx < x1; xx++ {
					r += uint64(src.Pix[row])
					g += uint64(src.Pix[row+1])
					b += uint64(src.Pix[row+2])
					a += uint64(src.Pix[row+3])
					row += 4
				}
			}
			n := uint64((y1 - y0) * (x1 - x0))
			if n == 0 {
				n = 1
			}
			o := dst.PixOffset(x, y)
			dst.Pix[o] = uint8(r / n)
			dst.Pix[o+1] = uint8(g / n)
			dst.Pix[o+2] = uint8(b / n)
			dst.Pix[o+3] = uint8(a / n)
		}
	}
	return dst
}

func debugFromEnv() bool { return os.Getenv("APPKIT_DEBUG") == "1" }

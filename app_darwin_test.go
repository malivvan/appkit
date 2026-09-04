package appkit

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"testing"
)

func samplePNG(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 8, 8))
	for y := range 8 {
		for x := range 8 {
			img.SetRGBA(x, y, color.RGBA{R: 0xff, G: 0x55, B: 0x55, A: 0xff})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encoding the sample PNG: %v", err)
	}
	return buf.Bytes()
}

func TestAppIconReachesTheApplication(t *testing.T) {
	if err := setAppIcon(samplePNG(t), ""); err != nil {
		t.Fatalf("setAppIcon: %v", err)
	}
	app := objcClass("NSApplication").Send(selector("sharedApplication"))
	got := app.Send(selector("applicationIconImage"))
	if got == 0 {
		t.Fatal("the application has no icon after one was set")
	}
	if got.Send(selector("isValid")) == 0 {
		t.Fatal("the icon AppKit holds is not a valid image")
	}
}

func TestAppIconRejectsWhatIsNotAnImage(t *testing.T) {
	if err := setAppIcon(nil, ""); err == nil {
		t.Error("an empty icon was accepted")
	}
	if err := setAppIcon([]byte("this is not a png"), ""); err == nil {
		t.Error("a string was accepted as an image")
	}
}

func TestKitDarwinPlatform(t *testing.T) {}

func TestDarwinObjcMarshaling(t *testing.T) {
	err := openEnsureInit()
	if err != nil {
		t.Fatalf("openEnsureInit: %v", err)
	}
	_, err = checkedClass("NSWorkspace")
	if err != nil {
		t.Fatalf("class NSWorkspace: %v", err)
	}
	urlCls, err := checkedClass("NSURL")
	if err != nil {
		t.Fatalf("class NSURL: %v", err)
	}
	arrCls, err := checkedClass("NSArray")
	if err != nil {
		t.Fatalf("class NSArray: %v", err)
	}
	autorelease(func() {
		u := urlCls.Send(selector("URLWithString:"), nsString("https://example.com"))
		if u == 0 {
			t.Error("URLWithString: returned nil")
		}
		fileURL := urlCls.Send(selector("fileURLWithPath:"), nsString("/tmp"))
		if fileURL == 0 {
			t.Error("fileURLWithPath: returned nil")
		}
		arr := arrCls.Send(selector("arrayWithObject:"), fileURL)
		if arr == 0 {
			t.Error("arrayWithObject: returned nil")
		}
	})
}

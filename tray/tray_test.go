package tray

import "testing"

func TestBoundsIdle(t *testing.T) {
	x, y, w, h := Bounds()
	if x != 0 || y != 0 || w != 0 || h != 0 {
		t.Fatalf("Bounds() with no active tray = (%d,%d,%d,%d), want (0,0,0,0)", x, y, w, h)
	}
}

func TestSentinels(t *testing.T) {
	if ErrUnsupported == nil || ErrAlreadyRunning == nil {
		t.Fatal("ErrUnsupported and ErrAlreadyRunning must be non-nil")
	}
	if ErrUnsupported == ErrAlreadyRunning {
		t.Fatal("ErrUnsupported and ErrAlreadyRunning must be distinct")
	}
}

func TestConfigSurface(t *testing.T) {
	cfg := Config{
		Title:         "title",
		Tooltip:       "tooltip",
		OnClick:       func() {},
		OnDoubleClick: func() {},
		OnRightClick:  func() {},
		Items: []Item{{
			Label:     "item",
			Icon:      []byte{4},
			Checkbox:  true,
			Checked:   true,
			Disabled:  false,
			Separator: false,
			Submenu:   []Item{{Label: "child", OnClick: func() {}}},
			OnClick:   func() {},
		}},
	}
	if cfg.Title != "title" || cfg.Items[0].Submenu[0].Label != "child" {
		t.Fatal("config surface mismatch")
	}
	expectSignature := func(func(string, []byte, Config) error) {}
	expectSignature(Set)
	expectSignature(Run)
}

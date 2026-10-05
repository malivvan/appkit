package tray

import (
	"fmt"
	"runtime"
	"sync"
	"unsafe"

	"github.com/malivvan/purego"
	"github.com/malivvan/purego/objc"
)

const (
	targetClassName = "NativeTrayTarget"

	nsApplicationActivationPolicyAccessory = 1
	nsEventTypeApplicationDefined          = 15
	nsVariableStatusItemLength             = -1.0
	statusIconSize                         = 18
)

type cgPoint struct{ X, Y float64 }

type nsSize struct{ W, H float64 }

type clickTarget struct {
	item    Item
	onClick func()
}

var (
	mu           sync.Mutex
	running      bool
	ownsLoop     bool
	trayClickFn  func()
	activeClicks []clickTarget
	trayTarget   objc.ID
	trayItem     objc.ID

	initOnce sync.Once
	initErr  error
	selCache sync.Map
)

func sel(name string) objc.SEL {
	v, ok := selCache.Load(name)
	if ok {
		return v.(objc.SEL)
	}
	s := objc.RegisterName(name)
	selCache.Store(name, s)
	return s
}

func class(name string) objc.ID {
	c := objc.GetClass(name)
	if c == 0 {
		panic(fmt.Sprintf("tray: objc class %q not found", name))
	}
	return objc.ID(c)
}

func nsstr(s string) objc.ID {
	return class("NSString").Send(sel("stringWithUTF8String:"), s)
}

func ensureInit() error {
	initOnce.Do(func() {
		for _, fw := range []string{
			"/System/Library/Frameworks/Foundation.framework/Foundation",
			"/System/Library/Frameworks/AppKit.framework/AppKit",
		} {
			_, err := purego.Dlopen(fw, purego.RTLD_LAZY|purego.RTLD_GLOBAL)
			if err != nil {
				initErr = fmt.Errorf("tray: load %s: %w", fw, err)
				return
			}
		}
		_, err := objc.RegisterClass(
			targetClassName, objc.GetClass("NSObject"), nil, nil,
			[]objc.MethodDef{
				{
					Cmd: sel("trayClicked:"),
					Fn: func(_self objc.ID, _cmd objc.SEL, _sender objc.ID) {
						mu.Lock()
						fn := trayClickFn
						mu.Unlock()
						if fn != nil {
							fn()
						}
					},
				},
				{
					Cmd: sel("menuItemClicked:"),
					Fn: func(_self objc.ID, _cmd objc.SEL, sender objc.ID) {
						tag := int(sender.Send(sel("tag")))
						mu.Lock()
						if tag < 0 || tag >= len(activeClicks) {
							mu.Unlock()
							return
						}
						entry := &activeClicks[tag]
						onClick := entry.onClick
						if entry.item.Disabled {
							mu.Unlock()
							return
						}
						if entry.item.Checkbox {
							if entry.item.Checked {
								entry.item.Checked = false
								sender.Send(sel("setState:"), 0)
							} else {
								entry.item.Checked = true
								sender.Send(sel("setState:"), 1)
							}
						}
						mu.Unlock()
						if onClick != nil {
							onClick()
						}
					},
				},
				{
					Cmd: sel("trayStop"),
					Fn: func(_self objc.ID, _cmd objc.SEL) {
						detachTray()
						mu.Lock()
						own := ownsLoop
						mu.Unlock()
						if own {
							haltRunLoop()
						}
					},
				},
			})
		if err != nil {
			initErr = fmt.Errorf("tray: register target class: %w", err)
		}
	})
	return initErr
}

func set(id string, icon []byte, cfg Config) error {
	mu.Lock()
	if running {
		mu.Unlock()
		return ErrAlreadyRunning
	}
	err := ensureInit()
	if err != nil {
		mu.Unlock()
		return err
	}
	running = true
	ownsLoop = false
	mu.Unlock()

	app := class("NSApplication").Send(sel("sharedApplication"))
	app.Send(sel("setActivationPolicy:"), nsApplicationActivationPolicyAccessory)
	app.Send(sel("finishLaunching"))

	target := class(targetClassName).Send(sel("alloc")).Send(sel("init"))
	target.Send(sel("retain"))

	bar := class("NSStatusBar").Send(sel("systemStatusBar"))
	item := bar.Send(sel("statusItemWithLength:"), float64(nsVariableStatusItemLength))
	item.Send(sel("retain"))

	btn := item.Send(sel("button"))
	applyStatusButton(btn, icon, cfg)
	if btn != 0 {
		btn.Send(sel("setTarget:"), target)
		btn.Send(sel("setAction:"), sel("trayClicked:"))
	}

	var clicks []clickTarget
	if len(cfg.Items) > 0 {
		nsMenu := buildTrayMenu(cfg.Items, &clicks, target)
		if nsMenu != 0 {
			item.Send(sel("setMenu:"), nsMenu)
		}
	}

	mu.Lock()
	trayTarget = target
	trayItem = item
	trayClickFn = cfg.OnClick
	activeClicks = clicks
	mu.Unlock()
	return nil
}

func run(id string, icon []byte, cfg Config) error {
	if err := set(id, icon, cfg); err != nil {
		return err
	}
	mu.Lock()
	ownsLoop = true
	mu.Unlock()

	runtime.LockOSThread()

	app := class("NSApplication").Send(sel("sharedApplication"))
	app.Send(sel("run"))

	detachTray()
	return nil
}

func detachTray() {
	mu.Lock()
	t := trayTarget
	it := trayItem
	r := running
	mu.Unlock()
	if !r || t == 0 {
		return
	}
	bar := class("NSStatusBar").Send(sel("systemStatusBar"))
	if it != 0 {
		bar.Send(sel("removeStatusItem:"), it)
		it.Send(sel("release"))
	}
	t.Send(sel("release"))

	mu.Lock()
	running = false
	ownsLoop = false
	trayTarget = 0
	trayItem = 0
	trayClickFn = nil
	activeClicks = nil
	mu.Unlock()
}

func applyStatusButton(button objc.ID, icon []byte, cfg Config) {
	if button == 0 {
		return
	}
	if len(icon) > 0 {
		if img := imageFromPNG(icon, statusIconSize); img != 0 {
			button.Send(sel("setImage:"), img)
			img.Send(sel("release"))
		}
	}
	switch {
	case cfg.Title != "":
		button.Send(sel("setTitle:"), nsstr(cfg.Title))
	case len(icon) == 0:
		button.Send(sel("setTitle:"), nsstr("●"))
	}
	if cfg.Tooltip != "" {
		button.Send(sel("setToolTip:"), nsstr(cfg.Tooltip))
	}
}

func buildTrayMenu(items []Item, clicks *[]clickTarget, target objc.ID) objc.ID {
	menu := class("NSMenu").Send(sel("alloc")).Send(sel("init"))
	menu.Send(sel("setAutoenablesItems:"), false)
	for _, it := range items {
		if it.Separator {
			menu.Send(sel("addItem:"), class("NSMenuItem").Send(sel("separatorItem")))
			continue
		}
		if it.Submenu != nil {
			mi := class("NSMenuItem").Send(sel("alloc")).Send(
				sel("initWithTitle:action:keyEquivalent:"), nsstr(it.Label), objc.SEL(0), nsstr(""))
			if sub := buildTrayMenu(it.Submenu, clicks, target); sub != 0 {
				mi.Send(sel("setSubmenu:"), sub)
			}
			if it.Disabled {
				mi.Send(sel("setEnabled:"), false)
			}
			menu.Send(sel("addItem:"), mi)
			mi.Send(sel("release"))
			continue
		}
		tag := len(*clicks)
		*clicks = append(*clicks, clickTarget{item: it, onClick: it.OnClick})
		mi := class("NSMenuItem").Send(sel("alloc")).Send(
			sel("initWithTitle:action:keyEquivalent:"), nsstr(it.Label), sel("menuItemClicked:"), nsstr(""))
		mi.Send(sel("setTarget:"), target)
		mi.Send(sel("setTag:"), tag)
		if it.Checked {
			mi.Send(sel("setState:"), 1)
		}
		if it.Disabled {
			mi.Send(sel("setEnabled:"), false)
		}
		if len(it.Icon) > 0 {
			if img := imageFromPNG(it.Icon, 16); img != 0 {
				mi.Send(sel("setImage:"), img)
				img.Send(sel("release"))
			}
		}
		menu.Send(sel("addItem:"), mi)
		mi.Send(sel("release"))
	}
	return menu
}

func imageFromPNG(png []byte, side float64) objc.ID {
	data := class("NSData").Send(sel("dataWithBytes:length:"),
		unsafe.Pointer(&png[0]), uint(len(png)))
	img := class("NSImage").Send(sel("alloc")).Send(sel("initWithData:"), data)
	if img == 0 {
		return 0
	}
	img.Send(sel("setSize:"), nsSize{side, side})
	return img
}

func haltRunLoop() {
	app := class("NSApplication").Send(sel("sharedApplication"))
	app.Send(sel("stop:"), objc.ID(0))
	event := class("NSEvent").Send(
		sel("otherEventWithType:location:modifierFlags:timestamp:windowNumber:context:subtype:data1:data2:"),
		nsEventTypeApplicationDefined, cgPoint{0, 0}, uint(0), float64(0), 0, objc.ID(0), int16(0), 0, 0)
	app.Send(sel("postEvent:atStart:"), event, true)
}

func stop() {
	mu.Lock()
	t := trayTarget
	r := running
	mu.Unlock()
	if !r || t == 0 {
		return
	}
	t.Send(sel("performSelectorOnMainThread:withObject:waitUntilDone:"), sel("trayStop"), objc.ID(0), false)
}

func remove() {
	mu.Lock()
	t := trayTarget
	r := running
	mu.Unlock()
	if !r || t == 0 {
		return
	}
	t.Send(sel("performSelectorOnMainThread:withObject:waitUntilDone:"), sel("trayStop"), objc.ID(0), true)
}

func bounds() (x, y, w, h int) { return 0, 0, 0, 0 }

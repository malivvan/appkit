package notify

import (
	"fmt"
	"os"
	"os/exec"
	"sync"
	"unsafe"

	"github.com/ebitengine/purego"
	"github.com/ebitengine/purego/objc"
)

var (
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
		panic(fmt.Sprintf("notify: objc class %q not found", name))
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
				initErr = fmt.Errorf("notify: load %s: %w", fw, err)
				return
			}
		}
	})
	return initErr
}

func show(name, title, message string, opts Options) error {
	if err := ensureInit(); err != nil {
		return err
	}

	notification := class("NSUserNotification").Send(sel("alloc")).Send(sel("init"))
	if notification == 0 {
		return fmt.Errorf("notify: failed to create NSUserNotification")
	}
	if title != "" {
		notification.Send(sel("setTitle:"), nsstr(title))
	}
	if message != "" {
		notification.Send(sel("setInformativeText:"), nsstr(message))
	}
	if opts.Urgency.level() == 2 {
		notification.Send(sel("setSoundName:"), nsstr("default"))
	}

	var image objc.ID
	switch {
	case opts.Icon != "":
		image = class("NSImage").Send(sel("alloc")).Send(sel("initWithContentsOfFile:"), nsstr(opts.Icon))
		if image == 0 {
			notification.Send(sel("release"))
			return fmt.Errorf("notify: cannot load Options.Icon file %q (macOS needs an image file path, not an icon name)", opts.Icon)
		}
	case len(opts.IconData) > 0:
		data := class("NSData").Send(sel("alloc")).Send(sel("initWithBytes:length:"), unsafe.Pointer(&opts.IconData[0]), len(opts.IconData))
		if data == 0 {
			notification.Send(sel("release"))
			return fmt.Errorf("notify: failed to create NSData for Options.IconData")
		}
		image = class("NSImage").Send(sel("alloc")).Send(sel("initWithData:"), data)
		data.Send(sel("release"))
		if image == 0 {
			notification.Send(sel("release"))
			return fmt.Errorf("notify: Options.IconData is not a decodable image")
		}
	}
	if image != 0 {
		notification.Send(sel("setContentImage:"), image)
	}

	center := class("NSUserNotificationCenter").Send(sel("defaultUserNotificationCenter"))
	if center == 0 {
		if image != 0 {
			image.Send(sel("release"))
		}
		notification.Send(sel("release"))
		return fmt.Errorf("%w (NSUserNotificationCenter is nil for this app: run as a bundled .app and grant Notification access)", ErrUnavailable)
	}
	center.Send(sel("deliverNotification:"), notification)
	if image != 0 {
		image.Send(sel("release"))
	}
	notification.Send(sel("release"))
	return nil
}

func beep(freq float64, duration int) error {
	osa, err := exec.LookPath("osascript")
	if err != nil {
		if _, err := os.Stdout.Write([]byte{7}); err != nil {
			return fmt.Errorf("notify: beep: write bell to stdout: %w", err)
		}
		return nil
	}
	return exec.Command(osa, "-e", "beep").Run()
}

func alertSound() error {
	return nil
}

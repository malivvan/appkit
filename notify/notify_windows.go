package notify

import (
	"errors"
	"fmt"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"unsafe"

	"github.com/malivvan/purego"
)

const (
	nimAdd        = 0x00000000
	nimModify     = 0x00000001
	nimSetVersion = 0x00000004

	nifMessage = 0x00000001
	nifIcon    = 0x00000002
	nifTip     = 0x00000004
	nifInfo    = 0x00000010

	niifInfo      = 0x00000001
	niifWarning   = 0x00000002
	niifError     = 0x00000003
	niifUser      = 0x00000004
	niifLargeIcon = 0x00000020

	imageIcon      = 0x00000001
	lrLoadFromFile = 0x00000010

	idiApplication = 32512
	idiError       = 32513
	idiInformation = 32515
	idiWarning     = 32516

	idcArrow = 32512

	notifyIconVersion4 = 4
)

type notifyIconData struct {
	cbSize       uint32
	hWnd         uintptr
	uID          uint32
	uFlags       uint32
	_            uint32
	hIcon        uintptr
	_            [128]uint16
	_            uint32
	_            uint32
	szInfo       [256]uint16
	uVersion     uint32
	szInfoTitle  [64]uint16
	dwInfoFlags  uint32
	_            [16]byte
	hBalloonIcon uintptr
}

var (
	_ [unsafe.Sizeof(notifyIconData{}) - notifyIconDataSize]byte
	_ [notifyIconDataSize - unsafe.Sizeof(notifyIconData{})]byte
)

type wndClassExW struct {
	cbSize        uint32
	_             uint32
	lpfnWndProc   uintptr
	_             int32
	_             int32
	hInstance     uintptr
	hIcon         uintptr
	hCursor       uintptr
	_             uintptr
	_             *uint16
	lpszClassName *uint16
	_             uintptr
}

var (
	initOnce sync.Once
	initErr  error

	nid notifyIconData
	hic uintptr

	version4 bool

	iconMu   sync.Mutex
	iconPath string
	iconH    uintptr

	wndProcCB uintptr
	className *uint16

	registerClassExW func(*wndClassExW) uint16
	createWindowExW  func(exStyle uint32, className, windowName *uint16, style uint32, x, y, width, height int32, parent, menu, instance, param uintptr) uintptr
	defWindowProcW   func(hwnd, msg, wParam, lParam uintptr) uintptr
	loadIconW        func(instance, name uintptr) uintptr
	loadCursorW      func(instance, name uintptr) uintptr
	getModuleHandleW func(name *uint16) uintptr
	shellNotifyIconW func(msg uint32, data *notifyIconData) int32
	loadImageW       func(instance uintptr, name *uint16, typ uint32, cx, cy int32, flags uint32) uintptr
	destroyIcon      func(icon uintptr) uint32
	messageBeep      func(uType uint32) uint32
	kernelBeep       func(freq, duration uint32) uint32
	getLastError     func() uint32
)

func ensureInit() error {
	initOnce.Do(func() {
		user32, err := syscall.LoadLibrary("user32.dll")
		if err != nil {
			initErr = fmt.Errorf("notify: load user32.dll: %w", err)
			return
		}
		shell32, err := syscall.LoadLibrary("shell32.dll")
		if err != nil {
			initErr = fmt.Errorf("notify: load shell32.dll: %w", err)
			return
		}
		kernel32, err := syscall.LoadLibrary("kernel32.dll")
		if err != nil {
			initErr = fmt.Errorf("notify: load kernel32.dll: %w", err)
			return
		}
		reg := func(p any, lib syscall.Handle, name string) {
			if initErr != nil {
				return
			}
			addr, e := syscall.GetProcAddress(lib, name)
			if e != nil {
				initErr = fmt.Errorf("notify: resolve %s: %w", name, e)
				return
			}
			purego.RegisterFunc(p, addr)
		}
		reg(&registerClassExW, user32, "RegisterClassExW")
		reg(&createWindowExW, user32, "CreateWindowExW")
		reg(&defWindowProcW, user32, "DefWindowProcW")
		reg(&loadIconW, user32, "LoadIconW")
		reg(&loadCursorW, user32, "LoadCursorW")
		reg(&loadImageW, user32, "LoadImageW")
		reg(&destroyIcon, user32, "DestroyIcon")
		reg(&messageBeep, user32, "MessageBeep")
		reg(&getModuleHandleW, kernel32, "GetModuleHandleW")
		reg(&kernelBeep, kernel32, "Beep")
		reg(&getLastError, kernel32, "GetLastError")
		reg(&shellNotifyIconW, shell32, "Shell_NotifyIconW")
		if initErr != nil {
			return
		}

		wndProcCB = purego.NewCallback(notifyWndProc)
		className = utf16Ptr("NativeNotifyWindow")
		hInst := getModuleHandleW(nil)
		wc := wndClassExW{
			lpfnWndProc:   wndProcCB,
			hInstance:     hInst,
			hIcon:         loadIconW(0, idiApplication),
			hCursor:       loadCursorW(0, idcArrow),
			lpszClassName: className,
		}
		wc.cbSize = uint32(unsafe.Sizeof(wc))
		if registerClassExW(&wc) == 0 {
			initErr = errors.New("notify: RegisterClassExW failed")
			return
		}

		w := createWindowExW(0, className, utf16Ptr("native notify"), 0, 0, 0, 0, 0, 0, 0, hInst, 0)
		if w == 0 {
			initErr = errors.New("notify: CreateWindowExW failed")
			return
		}

		hic = loadIconW(0, idiApplication)
		n := notifyIconData{}
		n.cbSize = uint32(unsafe.Sizeof(n))
		n.hWnd = w
		n.uID = 1
		n.uFlags = nifMessage | nifIcon
		n.hIcon = hic
		if shellNotifyIconW(nimAdd, &n) == 0 {
			initErr = errors.New("notify: Shell_NotifyIconW NIM_ADD failed")
			return
		}
		n.uVersion = notifyIconVersion4
		if shellNotifyIconW(nimSetVersion, &n) != 0 {
			version4 = true
		}
		nid = n
	})
	return initErr
}

func notifyWndProc(hwnd, msg, wParam, lParam uintptr) uintptr {
	return defWindowProcW(hwnd, msg, wParam, lParam)
}

func urgencyFlagsFor(u Urgency) uint32 {
	if u.level() == 2 {
		return niifError
	}
	return niifInfo
}

func stockIconGlyph(name string) (uint32, bool) {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "info", "information":
		return niifInfo, true
	case "warning":
		return niifWarning, true
	case "error":
		return niifError, true
	}
	return 0, false
}

func loadIconFromFile(path string) (uintptr, error) {
	iconMu.Lock()
	defer iconMu.Unlock()
	if path == iconPath && iconH != 0 {
		return iconH, nil
	}
	h := loadImageW(0, utf16Ptr(path), imageIcon, 0, 0, lrLoadFromFile)
	if h == 0 {
		return 0, fmt.Errorf("notify: LoadImageW cannot load icon file %q (error %d); Windows notification icons must be .ico or .bmp files", path, getLastError())
	}
	if iconH != 0 {
		destroyIcon(iconH)
	}
	iconPath = path
	iconH = h
	return h, nil
}

func show(name, title, message string, opts Options) error {
	if err := ensureInit(); err != nil {
		return err
	}
	if len(opts.IconData) > 0 {
		return fmt.Errorf("%w: Options.IconData (PNG bytes) is not decoded on Windows; pass an .ico or .bmp file path as Options.Icon", ErrUnsupported)
	}

	n := nid
	n.uFlags = nifInfo
	n.dwInfoFlags = urgencyFlagsFor(opts.Urgency)
	n.hBalloonIcon = 0
	swapped := false

	if opts.Icon != "" {
		if glyph, ok := stockIconGlyph(opts.Icon); ok {
			n.dwInfoFlags = glyph
		} else if h, err := loadIconFromFile(opts.Icon); err != nil {
			return err
		} else if version4 {
			n.dwInfoFlags = niifUser | niifLargeIcon
			n.hBalloonIcon = h
		} else {
			n.uFlags |= nifIcon
			n.hIcon = h
			swapped = true
		}
	}
	copyUTF16(n.szInfoTitle[:], title)
	copyUTF16(n.szInfo[:], message)

	if shellNotifyIconW(nimModify, &n) == 0 {
		return fmt.Errorf("notify: Shell_NotifyIconW NIM_MODIFY (notification) failed")
	}
	if swapped {
		r := nid
		r.uFlags = nifIcon
		r.hIcon = hic
		shellNotifyIconW(nimModify, &r)
	}
	return nil
}

func beep(freq float64, duration int) error {
	if err := ensureInit(); err != nil {
		return err
	}
	if freq == 0 {
		freq = DefaultFreq
	} else if freq > 32767 {
		freq = 32767
	} else if freq < 37 {
		freq = DefaultFreq
	}
	if duration == 0 {
		duration = DefaultDuration
	} else if duration < 0 {
		duration = DefaultDuration
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if kernelBeep(uint32(freq), uint32(duration)) == 0 {
		return fmt.Errorf("notify: kernel32 Beep failed (error %d)", getLastError())
	}
	return nil
}

func alertSound() error {
	messageBeep(0)
	return nil
}

func copyUTF16(buf []uint16, s string) {
	u, err := syscall.UTF16FromString(s)
	if err != nil {
		return
	}
	m := len(u)
	if m > len(buf) {
		m = len(buf)
	}
	copy(buf[:m], u[:m])
	buf[len(buf)-1] = 0
}

func utf16Ptr(s string) *uint16 {
	p, err := syscall.UTF16PtrFromString(s)
	if err != nil {
		empty, _ := syscall.UTF16PtrFromString("")
		return empty
	}
	return p
}

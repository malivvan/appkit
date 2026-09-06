package tray

import (
	"errors"
	"fmt"
	"runtime"
	"sync"
	"syscall"
	"unsafe"

	"github.com/ebitengine/purego"
)

const (
	wmDestroy       = 0x0002
	wmClose         = 0x0010
	wmNull          = 0x0000
	wmApp           = 0x8000
	wmLButtonUp     = 0x0202
	wmLButtonDblClk = 0x0203
	wmRButtonUp     = 0x0205
	wmSettingChange = 0x001A
	wmContextMenu   = 0x007B

	trayCallbackMsg = wmApp + 1

	nimAdd        = 0x00000000
	nimModify     = 0x00000001
	nimDelete     = 0x00000002
	nimSetVersion = 0x00000004

	nifMessage = 0x00000001
	nifIcon    = 0x00000002
	nifTip     = 0x00000004

	notifyIconVersion4 = 4

	idiApplication = 32512
	idcArrow       = 32512

	mfString    = 0x0000
	mfGrayed    = 0x0001
	mfChecked   = 0x0008
	mfPopup     = 0x0010
	mfSeparator = 0x0800

	tpmLeftAlign   = 0x0000
	tpmRightButton = 0x0002
	tpmNoNotify    = 0x0080
	tpmReturnCmd   = 0x0100

	lrDefaultColor = 0x00000000

	smCXSmIcon = 49
	smCYSmIcon = 50

	miiState       = 0x00000001
	mfsCheckedFlag = 0x00000008
	mfsDisabled    = 0x00000003
	byPosition     = 0x00000400
)

type point struct{ X, Y int32 }

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

type msgStruct struct{ _ [6]uintptr }

type notifyIconData struct {
	cbSize           uint32
	hWnd             uintptr
	uID              uint32
	uFlags           uint32
	uCallbackMessage uint32
	hIcon            uintptr
	szTip            [128]uint16
	_                uint32
	_                uint32
	_                [256]uint16
	uVersion         uint32
	_                [64]uint16
	_                uint32
	_                [16]byte
	_                uintptr
}

var (
	_ [unsafe.Sizeof(notifyIconData{}) - notifyIconDataSize]byte
	_ [notifyIconDataSize - unsafe.Sizeof(notifyIconData{})]byte
)

type notifyIconIdentifier struct {
	cbSize uint32
	hWnd   uintptr
	uID    uint32
	_      [16]byte
}

type winRect struct{ left, top, right, bottom int32 }

type menuItemInfoW struct {
	cbSize uint32
	fMask  uint32
	fState uint32
	_      uint32
	_      uint32
	_      uintptr
	_      uintptr
	_      uintptr
	_      uintptr
	_      uintptr
	_      uint32
	_      uintptr
}

type cmdEntry struct {
	onClick  func()
	checkbox bool
	checked  bool
	hmenu    uintptr
	pos      uint32
}

var (
	initOnce sync.Once
	initErr  error

	mu       sync.Mutex
	running  bool
	ownsLoop bool
	trayHwnd uintptr
	hMenu    uintptr
	nid      notifyIconData
	hicon    uintptr

	trayClickFn      func()
	trayDblClickFn   func()
	trayRightClickFn func()
	cmdByID          map[uintptr]*cmdEntry
	cmdNext          uintptr
	tip              string

	cbMu  sync.Mutex
	cmdMu sync.Mutex

	trayWndProcCB uintptr
	classNamePtr  *uint16

	registerClassExW         func(*wndClassExW) uint16
	createWindowExW          func(exStyle uint32, className, windowName *uint16, style uint32, x, y, width, height int32, parent, menu, instance, param uintptr) uintptr
	defWindowProcW           func(hwnd, msg, wParam, lParam uintptr) uintptr
	getMessageW              func(msg *msgStruct, hwnd uintptr, filterMin, filterMax uint32) int32
	translateMessage         func(*msgStruct) int32
	dispatchMessageW         func(*msgStruct) uintptr
	postQuitMessage          func(exitCode int32)
	destroyWindow            func(hwnd uintptr) int32
	loadIconW                func(instance, name uintptr) uintptr
	loadCursorW              func(instance, name uintptr) uintptr
	getCursorPos             func(*point) int32
	setForegroundWindow      func(hwnd uintptr) int32
	trackPopupMenu           func(menu uintptr, flags uint32, x, y, reserved int32, hwnd, rect uintptr) int32
	createPopupMenu          func() uintptr
	appendMenuW              func(menu uintptr, flags uint32, id uintptr, item *uint16) int32
	destroyMenu              func(menu uintptr) int32
	postMessageW             func(hwnd, msg, wParam, lParam uintptr) int32
	getModuleHandleW         func(name *uint16) uintptr
	shellNotifyIconW         func(msg uint32, data *notifyIconData) int32
	createIconFromResourceEx func(resource *byte, bytes uint32, isIcon int32, ver uint32, cx, cy int32, flags uint32) uintptr
	destroyIcon              func(icon uintptr) int32
	getSystemMetrics         func(index int32) int32
	setMenuItemInfoW         func(menu uintptr, item uintptr, byPosition int32, info *menuItemInfoW) int32
	shellNotifyIconGetRect   func(id *notifyIconIdentifier, rc *winRect) int32
)

func ensureInit() error {
	initOnce.Do(func() {
		user32, err := syscall.LoadLibrary("user32.dll")
		if err != nil {
			initErr = fmt.Errorf("tray: load user32.dll: %w", err)
			return
		}
		shell32, err := syscall.LoadLibrary("shell32.dll")
		if err != nil {
			initErr = fmt.Errorf("tray: load shell32.dll: %w", err)
			return
		}
		kernel32, err := syscall.LoadLibrary("kernel32.dll")
		if err != nil {
			initErr = fmt.Errorf("tray: load kernel32.dll: %w", err)
			return
		}

		reg := func(p any, lib syscall.Handle, name string) {
			if initErr != nil {
				return
			}
			addr, e := syscall.GetProcAddress(lib, name)
			if e != nil {
				initErr = fmt.Errorf("tray: resolve %s: %w", name, e)
				return
			}
			purego.RegisterFunc(p, addr)
		}
		reg(&registerClassExW, user32, "RegisterClassExW")
		reg(&createWindowExW, user32, "CreateWindowExW")
		reg(&defWindowProcW, user32, "DefWindowProcW")
		reg(&getMessageW, user32, "GetMessageW")
		reg(&translateMessage, user32, "TranslateMessage")
		reg(&dispatchMessageW, user32, "DispatchMessageW")
		reg(&postQuitMessage, user32, "PostQuitMessage")
		reg(&destroyWindow, user32, "DestroyWindow")
		reg(&loadIconW, user32, "LoadIconW")
		reg(&loadCursorW, user32, "LoadCursorW")
		reg(&getCursorPos, user32, "GetCursorPos")
		reg(&setForegroundWindow, user32, "SetForegroundWindow")
		reg(&trackPopupMenu, user32, "TrackPopupMenu")
		reg(&createPopupMenu, user32, "CreatePopupMenu")
		reg(&appendMenuW, user32, "AppendMenuW")
		reg(&destroyMenu, user32, "DestroyMenu")
		reg(&postMessageW, user32, "PostMessageW")
		reg(&setMenuItemInfoW, user32, "SetMenuItemInfoW")
		reg(&createIconFromResourceEx, user32, "CreateIconFromResourceEx")
		reg(&destroyIcon, user32, "DestroyIcon")
		reg(&getSystemMetrics, user32, "GetSystemMetrics")
		reg(&getModuleHandleW, kernel32, "GetModuleHandleW")
		reg(&shellNotifyIconW, shell32, "Shell_NotifyIconW")
		reg(&shellNotifyIconGetRect, shell32, "Shell_NotifyIconGetRect")
		if initErr != nil {
			return
		}

		trayWndProcCB = purego.NewCallback(trayWndProc)
		classNamePtr = utf16Ptr("NativeTrayWindow")
		hInst := getModuleHandleW(nil)
		wc := wndClassExW{
			lpfnWndProc:   trayWndProcCB,
			hInstance:     hInst,
			hIcon:         loadIconW(0, idiApplication),
			hCursor:       loadCursorW(0, idcArrow),
			lpszClassName: classNamePtr,
		}
		wc.cbSize = uint32(unsafe.Sizeof(wc))
		if registerClassExW(&wc) == 0 {
			initErr = errors.New("tray: RegisterClassExW failed")
		}
	})
	return initErr
}

func trayWndProc(hwnd uintptr, msg uint32, wParam, lParam unsafe.Pointer) uintptr {
	switch msg {
	case trayCallbackMsg:
		ev := uintptr(lParam) & 0xFFFF
		cbMu.Lock()
		left := trayClickFn
		dbl := trayDblClickFn
		right := trayRightClickFn
		cbMu.Unlock()
		switch ev {
		case wmLButtonUp:
			if left != nil {
				left()
			}
		case wmLButtonDblClk:
			if dbl != nil {
				dbl()
			}
		case wmRButtonUp:
			if right != nil {
				right()
			}
			showMenu(hwnd)
		case wmContextMenu:
			showMenu(hwnd)
		}
		return 0
	case wmClose:
		destroyWindow(hwnd)
		return 0
	case wmDestroy:
		mu.Lock()
		own := ownsLoop
		mu.Unlock()
		freeTrayResources()
		if own {
			postQuitMessage(0)
		}
		return 0
	}
	return defWindowProcW(hwnd, uintptr(msg), uintptr(wParam), uintptr(lParam))
}

func showMenu(hwnd uintptr) {
	mu.Lock()
	menu := hMenu
	cbMu.Lock()
	fns := map[uintptr]*cmdEntry{}
	cmdMu.Lock()
	for id, e := range cmdByID {
		fns[id] = e
	}
	cmdMu.Unlock()
	cbMu.Unlock()
	mu.Unlock()
	if menu == 0 {
		return
	}

	var pt point
	getCursorPos(&pt)
	setForegroundWindow(hwnd)
	cmd := trackPopupMenu(menu, tpmLeftAlign|tpmRightButton|tpmReturnCmd|tpmNoNotify, pt.X, pt.Y, 0, hwnd, 0)
	postMessageW(hwnd, wmNull, 0, 0)
	if cmd <= 0 {
		return
	}
	entry, ok := fns[uintptr(cmd)]
	if !ok {
		return
	}
	if entry.checkbox {
		entry.checked = !entry.checked
		setItemChecked(entry.hmenu, entry.pos, entry.checked)
	}
	if entry.onClick != nil {
		entry.onClick()
	}
}

func setItemChecked(hmenu uintptr, pos uint32, checked bool) {
	if hmenu == 0 {
		return
	}
	mii := menuItemInfoW{cbSize: uint32(unsafe.Sizeof(menuItemInfoW{}))}
	mii.fMask = miiState
	if checked {
		mii.fState = mfsCheckedFlag
	}
	setMenuItemInfoW(hmenu, uintptr(pos), byPosition, &mii)
}

func buildTrayMenu(items []Item) uintptr {
	h := createPopupMenu()
	buildTrayMenuItems(h, items, 0)
	return h
}

func buildTrayMenuItems(hmenu uintptr, items []Item, depth int) {
	pos := uint32(0)
	for _, it := range items {
		if it.Separator {
			appendMenuW(hmenu, mfSeparator, 0, nil)
			pos++
			continue
		}
		if it.Submenu != nil {
			label := utf16Ptr(it.Label)
			flags := uint32(mfString | mfPopup)
			if it.Disabled {
				flags |= mfGrayed
			}
			sub := createPopupMenu()
			buildTrayMenuItems(sub, it.Submenu, depth+1)
			appendMenuW(hmenu, flags, sub, label)
			pos++
			continue
		}
		label := utf16Ptr(it.Label)
		flags := uint32(mfString)
		checked := it.Checked
		if it.Disabled {
			flags |= mfGrayed
		}
		if checked {
			flags |= mfChecked
		}
		cmdMu.Lock()
		cmdNext++
		id := cmdNext
		cmdByID[id] = &cmdEntry{
			onClick:  it.OnClick,
			checkbox: it.Checkbox,
			checked:  checked,
			hmenu:    hmenu,
			pos:      pos,
		}
		cmdMu.Unlock()
		appendMenuW(hmenu, flags, id, label)
		pos++
	}
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

	hInst := getModuleHandleW(nil)
	hwnd := createWindowExW(0, classNamePtr, utf16Ptr("native tray"), 0, 0, 0, 0, 0, 0, 0, hInst, 0)
	if hwnd == 0 {
		mu.Lock()
		running = false
		mu.Unlock()
		return errors.New("tray: CreateWindowExW failed")
	}

	mu.Lock()
	trayClickFn = cfg.OnClick
	trayDblClickFn = cfg.OnDoubleClick
	trayRightClickFn = cfg.OnRightClick
	tip = cfg.Tooltip
	if tip == "" {
		tip = cfg.Title
	}
	cmdByID = map[uintptr]*cmdEntry{}
	cmdNext = 0
	mu.Unlock()

	png := icon
	hic := uintptr(0)
	if len(png) > 0 {
		hic, err = hiconFromPNG(png)
		if err != nil {
			hic = 0
		}
	}
	if hic == 0 {
		hic = loadIconW(0, idiApplication)
	}

	hmenu := uintptr(0)
	if len(cfg.Items) > 0 {
		hmenu = buildTrayMenu(cfg.Items)
	}

	n := notifyIconData{}
	n.cbSize = uint32(unsafe.Sizeof(n))
	n.hWnd = hwnd
	n.uID = 1
	n.uFlags = nifMessage | nifIcon | nifTip
	n.uCallbackMessage = trayCallbackMsg
	n.hIcon = hic
	setTooltip(&n, tip)
	shellNotifyIconW(nimAdd, &n)

	nv := n
	nv.uFlags = 0
	nv.uVersion = notifyIconVersion4
	shellNotifyIconW(nimSetVersion, &nv)

	mu.Lock()
	trayHwnd = hwnd
	hMenu = hmenu
	hicon = hic
	nid = n
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

	var msg msgStruct
	for {
		r := getMessageW(&msg, 0, 0, 0)
		if r == 0 || r == -1 {
			break
		}
		translateMessage(&msg)
		dispatchMessageW(&msg)
	}

	detachTray()
	return nil
}

func freeTrayResources() {
	mu.Lock()
	if hicon != 0 {
		destroyIcon(hicon)
		hicon = 0
	}
	if hMenu != 0 {
		destroyMenu(hMenu)
		hMenu = 0
	}
	trayHwnd = 0
	mu.Unlock()
}

func detachTray() {
	mu.Lock()
	h := trayHwnd
	r := running
	mu.Unlock()
	if !r || h == 0 {
		return
	}
	shellNotifyIconW(nimDelete, &nid)
	destroyWindow(h)

	mu.Lock()
	running = false
	ownsLoop = false
	trayClickFn = nil
	trayDblClickFn = nil
	trayRightClickFn = nil
	cmdByID = nil
	mu.Unlock()
}

func stop() {
	mu.Lock()
	h := trayHwnd
	r := running
	own := ownsLoop
	mu.Unlock()
	if !r || h == 0 {
		return
	}
	if own {
		postMessageW(h, wmClose, 0, 0)
		return
	}
	detachTray()
}

func remove() { detachTray() }

func hiconFromPNG(png []byte) (uintptr, error) {
	if len(png) == 0 {
		return 0, errors.New("tray: empty PNG data")
	}
	cx := getSystemMetrics(smCXSmIcon)
	cy := getSystemMetrics(smCYSmIcon)
	if cx == 0 {
		cx = 16
	}
	if cy == 0 {
		cy = 16
	}
	h := createIconFromResourceEx(&png[0], uint32(len(png)), 1, 0x00030000, cx, cy, lrDefaultColor)
	if h == 0 {
		return 0, fmt.Errorf("tray: CreateIconFromResourceEx failed for %d-byte PNG", len(png))
	}
	return h, nil
}

func setTooltip(n *notifyIconData, s string) {
	u, err := syscall.UTF16FromString(s)
	if err != nil {
		return
	}
	m := len(u)
	if m > len(n.szTip) {
		m = len(n.szTip)
	}
	copy(n.szTip[:m], u[:m])
	n.szTip[len(n.szTip)-1] = 0
}

func utf16Ptr(s string) *uint16 {
	p, err := syscall.UTF16PtrFromString(s)
	if err != nil {
		empty, _ := syscall.UTF16PtrFromString("")
		return empty
	}
	return p
}

func bounds() (x, y, w, h int) {
	mu.Lock()
	hw := trayHwnd
	mu.Unlock()
	if hw == 0 {
		return 0, 0, 0, 0
	}
	id := notifyIconIdentifier{
		cbSize: uint32(unsafe.Sizeof(notifyIconIdentifier{})),
		hWnd:   hw,
		uID:    1,
	}
	var rc winRect
	if shellNotifyIconGetRect(&id, &rc) != 0 {
		return 0, 0, 0, 0
	}
	return int(rc.left), int(rc.top), int(rc.right - rc.left), int(rc.bottom - rc.top)
}

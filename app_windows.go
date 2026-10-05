//go:build windows

package appkit

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/draw"
	"image/png"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/malivvan/purego"
	winregistry "golang.org/x/sys/windows/registry"
)

const (
	pipeAccessInbound         = 0x00000001
	fileFlagFirstPipeInstance = 0x00080000
	pipeWaitByte              = 0x00000000
	genericWrite              = 0x40000000
	openExisting              = 3
	pipeInBufferSize          = 64 * 1024
)

var invalidHandle = ^uintptr(0)

var _ = atomicWriteFile

var (
	initOnce sync.Once
	initErr  error

	createNamedPipeW    func(name *uint16, openMode, pipeMode, maxInstances, outBuf, inBuf, timeout uint32, sa uintptr) uintptr
	connectNamedPipe    func(h, overlapped uintptr) int32
	disconnectNamedPipe func(h uintptr) int32
	readFile            func(h uintptr, buf *byte, n uint32, read *uint32, overlapped uintptr) int32
	writeFile           func(h uintptr, buf *byte, n uint32, written *uint32, overlapped uintptr) int32
	createFileW         func(name *uint16, access, share uint32, sa uintptr, disp, flags uint32, template uintptr) uintptr
	closeHandle         func(h uintptr) int32
)

func ensureInit() error {
	initOnce.Do(func() {
		k32, err := syscall.LoadLibrary("kernel32.dll")
		if err != nil {
			initErr = err
			return
		}
		reg := func(p any, name string) {
			if initErr != nil {
				return
			}
			addr, e := syscall.GetProcAddress(k32, name)
			if e != nil {
				initErr = e
				return
			}
			purego.RegisterFunc(p, addr)
		}
		reg(&createNamedPipeW, "CreateNamedPipeW")
		reg(&connectNamedPipe, "ConnectNamedPipe")
		reg(&disconnectNamedPipe, "DisconnectNamedPipe")
		reg(&readFile, "ReadFile")
		reg(&writeFile, "WriteFile")
		reg(&createFileW, "CreateFileW")
		reg(&closeHandle, "CloseHandle")
	})
	return initErr
}

func instancePipeName(id string) string { return `\\.\pipe\native-si-` + instanceFingerprint(id) }

func acquireInstanceLock(id string, onMessage func([]string)) (*instanceGuard, error) {
	err := ensureInit()
	if err != nil {
		return nil, err
	}
	name, err := syscall.UTF16PtrFromString(instancePipeName(id))
	if err != nil {
		return nil, err
	}
	h := createNamedPipeW(name,
		pipeAccessInbound|fileFlagFirstPipeInstance,
		pipeWaitByte,
		1, 0, pipeInBufferSize, 0, 0)
	if h == invalidHandle {
		return nil, errInstanceRunning
	}

	stop := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		servePipe(h, onMessage, stop)
	}()

	return &instanceGuard{release: func() error {
		close(stop)
		deadline := time.Now().Add(5 * time.Second)
		for {
			select {
			case <-done:
				return nil
			default:
			}
			if time.Now().After(deadline) {
				return nil
			}
			if c := createFileW(name, genericWrite, 0, 0, openExisting, 0, 0); c != invalidHandle {
				closeHandle(c)
			}
			time.Sleep(10 * time.Millisecond)
		}
	}}, nil
}

func servePipe(h uintptr, onMessage func([]string), stop chan struct{}) {
	defer closeHandle(h)
	buf := make([]byte, pipeInBufferSize)
	for {
		connectNamedPipe(h, 0)
		select {
		case <-stop:
			return
		default:
		}

		var total []byte
		for {
			var n uint32
			ok := readFile(h, &buf[0], uint32(len(buf)), &n, 0)
			if n > 0 {
				total = append(total, buf[:n]...)
			}
			if ok == 0 {
				break
			}
		}
		if len(total) > 0 {
			var args []string
			if json.Unmarshal(total, &args) == nil && onMessage != nil {
				onMessage(args)
			}
		}
		disconnectNamedPipe(h)
	}
}

func sendInstanceMessage(id string, args []string) error {
	err := ensureInit()
	if err != nil {
		return err
	}
	data, err := json.Marshal(args)
	if err != nil {
		return err
	}
	name, err := syscall.UTF16PtrFromString(instancePipeName(id))
	if err != nil {
		return err
	}
	h := createFileW(name, genericWrite, 0, 0, openExisting, 0, 0)
	if h == invalidHandle {
		return errors.New("appkit: no running instance to receive the message")
	}
	defer closeHandle(h)
	var written uint32
	if writeFile(h, &data[0], uint32(len(data)), &written, 0) == 0 {
		return errors.New("appkit: failed to write to the running instance")
	}
	return nil
}

var appWindowIconPNG []byte

func setAppIcon(png []byte, _ string) error {
	if len(png) == 0 {
		return errors.New("appkit: the application icon is empty")
	}
	appWindowIconPNG = png
	return nil
}

var (
	windowIconOnce  sync.Once
	windowIconBig   uintptr
	windowIconSmall uintptr
)

func windowIconHandles() (uintptr, uintptr) {
	windowIconOnce.Do(func() {
		src, err := png.Decode(bytes.NewReader(appWindowIconPNG))
		if err != nil {
			return
		}
		b := src.Bounds()
		if b.Dx() <= 0 || b.Dy() <= 0 {
			return
		}
		img := image.NewNRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
		draw.Draw(img, img.Bounds(), src, b.Min, draw.Src)
		for _, sz := range []int{32, 16} {
			pngBytes := encodeWindowIconPNG(downscaleIcon(img, sz))
			if len(pngBytes) == 0 {
				continue
			}
			h := createIconFromResourceEx(&pngBytes[0], uint32(len(pngBytes)), 1, 0x00030000, int32(sz), int32(sz), 0)
			if h == 0 {
				continue
			}
			if sz == 32 {
				windowIconBig = h
			} else {
				windowIconSmall = h
			}
		}
	})
	return windowIconBig, windowIconSmall
}

func applyWindowAppIcon(hwnd uintptr) {
	if hwnd == 0 || len(appWindowIconPNG) == 0 || createIconFromResourceEx == nil {
		return
	}
	big, small := windowIconHandles()
	if big != 0 {
		sendMessageW(hwnd, wmSetIcon, iconBig, big)
	}
	if small != 0 {
		sendMessageW(hwnd, wmSetIcon, iconSmall, small)
	}
}

func encodeWindowIconPNG(img image.Image) []byte {
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil
	}
	return buf.Bytes()
}

const swShowNormal = 1

var (
	openInitOnce sync.Once
	openInitErr  error

	shellExecuteW func(hwnd uintptr, op, file, params, dir *uint16, showCmd int32) uintptr
)

func openEnsureInit() error {
	openInitOnce.Do(func() {
		shell32, err := syscall.LoadLibrary("shell32.dll")
		if err != nil {
			openInitErr = fmt.Errorf("open: load shell32.dll: %w", err)
			return
		}
		addr, err := syscall.GetProcAddress(shell32, "ShellExecuteW")
		if err != nil {
			openInitErr = fmt.Errorf("open: resolve ShellExecuteW: %w", err)
			return
		}
		purego.RegisterFunc(&shellExecuteW, addr)
	})
	return openInitErr
}

func openURL(rawurl string) error {
	err := openEnsureInit()
	if err != nil {
		return err
	}
	op, _ := syscall.UTF16PtrFromString("open")
	file, err := syscall.UTF16PtrFromString(rawurl)
	if err != nil {
		return fmt.Errorf("open: %q: %w", rawurl, err)
	}
	r := shellExecuteW(0, op, file, nil, nil, swShowNormal)
	if r <= 32 {
		return fmt.Errorf("open: ShellExecuteW(%q) failed (code %d)", rawurl, r)
	}
	return nil
}

func revealFile(absPath string) error {
	err := openEnsureInit()
	if err != nil {
		return err
	}
	file, _ := syscall.UTF16PtrFromString("explorer.exe")
	params, err := syscall.UTF16PtrFromString(`/select,"` + absPath + `"`)
	if err != nil {
		return fmt.Errorf("open: %q: %w", absPath, err)
	}
	r := shellExecuteW(0, nil, file, params, nil, swShowNormal)
	if r <= 32 {
		return fmt.Errorf("open: reveal %q failed (code %d)", absPath, r)
	}
	return nil
}

const autostartRunSubKey = `Software\Microsoft\Windows\CurrentVersion\Run`

type registryAutostart struct {
	subKey string
}

func newAutostartDriver(cfg appSetup) autostartDriver {
	return &registryAutostart{subKey: autostartRunSubKey}
}

func (a *registryAutostart) enable(id string, args []string) error {
	exe, err := executablePath()
	if err != nil {
		return err
	}
	cmd := quoteWindowsArg(exe)
	for _, arg := range args {
		cmd += " " + quoteWindowsArg(arg)
	}
	key, _, err := winregistry.CreateKey(winregistry.CURRENT_USER, a.subKey, winregistry.SET_VALUE)
	if err != nil {
		return fmt.Errorf("appkit: autostart: open registry key: %w", err)
	}
	defer func() { _ = key.Close() }()
	if existing, _, ferr := a.find(); ferr == nil && existing != "" && existing != id {
		_ = key.DeleteValue(existing)
	}
	if err := key.SetStringValue(id, cmd); err != nil {
		return fmt.Errorf("appkit: autostart: write registry value: %w", err)
	}
	return nil
}

func (a *registryAutostart) disable() error {
	id, _, err := a.find()
	if err != nil {
		return err
	}
	if id == "" {
		return nil
	}
	key, err := winregistry.OpenKey(winregistry.CURRENT_USER, a.subKey, winregistry.SET_VALUE)
	if err != nil {
		if errors.Is(err, winregistry.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("appkit: autostart: open registry key: %w", err)
	}
	defer func() { _ = key.Close() }()
	if err := key.DeleteValue(id); err != nil && !errors.Is(err, winregistry.ErrNotExist) {
		return fmt.Errorf("appkit: autostart: delete registry value: %w", err)
	}
	return nil
}

func (a *registryAutostart) status() (bool, string, string) {
	id, _, err := a.find()
	if err != nil || id == "" {
		return false, "", ""
	}
	return true, `HKCU\` + a.subKey + `\` + id, autostartBackendRegistryRun
}

func (a *registryAutostart) find() (string, string, error) {
	exe, err := executablePath()
	if err != nil {
		return "", "", err
	}
	key, err := winregistry.OpenKey(winregistry.CURRENT_USER, a.subKey, winregistry.QUERY_VALUE)
	if err != nil {
		if errors.Is(err, winregistry.ErrNotExist) {
			return "", "", nil
		}
		return "", "", fmt.Errorf("appkit: autostart: open registry key: %w", err)
	}
	defer func() { _ = key.Close() }()
	names, err := key.ReadValueNames(-1)
	if err != nil {
		return "", "", fmt.Errorf("appkit: autostart: list registry values: %w", err)
	}
	exeLower := strings.ToLower(exe)
	for _, name := range names {
		val, _, err := key.GetStringValue(name)
		if err != nil {
			continue
		}
		if strings.EqualFold(parseWindowsCommandExe(val), exeLower) {
			return name, val, nil
		}
	}
	return "", "", nil
}

func parseWindowsCommandExe(cmd string) string {
	cmd = strings.TrimSpace(cmd)
	if cmd == "" {
		return ""
	}
	if cmd[0] == '"' {
		end := strings.IndexByte(cmd[1:], '"')
		if end < 0 {
			return strings.ToLower(cmd[1:])
		}
		return strings.ToLower(cmd[1 : 1+end])
	}
	if i := strings.IndexAny(cmd, " \t"); i >= 0 {
		return strings.ToLower(cmd[:i])
	}
	return strings.ToLower(cmd)
}

func quoteWindowsArg(s string) string {
	if s != "" && !strings.ContainsAny(s, `" `+"\t") {
		return s
	}
	var b strings.Builder
	b.WriteByte('"')
	backslashes := 0
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch c {
		case '\\':
			backslashes++
		case '"':
			for j := 0; j < 2*backslashes; j++ {
				b.WriteByte('\\')
			}
			b.WriteByte('\\')
			b.WriteByte('"')
			backslashes = 0
		default:
			for j := 0; j < backslashes; j++ {
				b.WriteByte('\\')
			}
			backslashes = 0
			b.WriteByte(c)
		}
	}
	for j := 0; j < backslashes; j++ {
		b.WriteByte('\\')
		b.WriteByte('\\')
	}
	b.WriteByte('"')
	return b.String()
}

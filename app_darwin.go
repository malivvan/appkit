package appkit

import (
	"encoding/xml"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"unsafe"

	"github.com/malivvan/purego"
	"github.com/malivvan/purego/objc"
)

var (
	iconInitOnce sync.Once
	iconInitErr  error
	lastIcon     []byte
)

func iconEnsureInit() error {
	iconInitOnce.Do(func() {
		for _, fw := range []string{
			"/System/Library/Frameworks/Foundation.framework/Foundation",
			"/System/Library/Frameworks/AppKit.framework/AppKit",
		} {
			_, err := purego.Dlopen(fw, purego.RTLD_LAZY|purego.RTLD_GLOBAL)
			if err != nil {
				iconInitErr = fmt.Errorf("appkit: load %s: %w", fw, err)
				return
			}
		}
	})
	return iconInitErr
}

func setAppIcon(png []byte, _ string) error {
	if len(png) == 0 {
		return errors.New("appkit: the application icon is empty")
	}
	err := iconEnsureInit()
	if err != nil {
		return err
	}
	var failed bool
	autorelease(func() {
		data := objcClass("NSData").Send(selector("dataWithBytes:length:"), unsafe.Pointer(&png[0]), len(png))
		image := objcClass("NSImage").Send(selector("alloc")).Send(selector("initWithData:"), data)
		if image == 0 {
			failed = true
			return
		}
		image.Send(selector("autorelease"))
		app := objcClass("NSApplication").Send(selector("sharedApplication"))
		app.Send(selector("setApplicationIconImage:"), image)
	})
	if failed {
		return errors.New("appkit: the application icon is not an image AppKit can read")
	}
	lastIcon = png
	return nil
}

func reapplyAppIcon() {
	if len(lastIcon) == 0 {
		return
	}
	_ = setAppIcon(lastIcon, "")
}

var (
	openInitOnce sync.Once
	openInitErr  error
)

func openEnsureInit() error {
	openInitOnce.Do(func() {
		for _, fw := range []string{
			"/System/Library/Frameworks/Foundation.framework/Foundation",
			"/System/Library/Frameworks/AppKit.framework/AppKit",
		} {
			_, err := purego.Dlopen(fw, purego.RTLD_LAZY|purego.RTLD_GLOBAL)
			if err != nil {
				openInitErr = fmt.Errorf("open: load %s: %w", fw, err)
				return
			}
		}
	})
	return openInitErr
}

func checkedClass(name string) (objc.ID, error) {
	c := objc.GetClass(name)
	if c == 0 {
		return 0, fmt.Errorf("open: objc class %q not found", name)
	}
	return objc.ID(c), nil
}

func openURL(rawurl string) error {
	err := openEnsureInit()
	if err != nil {
		return err
	}
	wsCls, err := checkedClass("NSWorkspace")
	if err != nil {
		return err
	}
	urlCls, err := checkedClass("NSURL")
	if err != nil {
		return err
	}
	var ok bool
	autorelease(func() {
		ws := wsCls.Send(selector("sharedWorkspace"))
		nsurl := urlCls.Send(selector("URLWithString:"), nsString(rawurl))
		if nsurl != 0 {
			ok = ws.Send(selector("openURL:"), nsurl) != 0
		}
	})
	if !ok {
		return fmt.Errorf("open: NSWorkspace openURL: failed for %q", rawurl)
	}
	return nil
}

func revealFile(absPath string) error {
	err := openEnsureInit()
	if err != nil {
		return err
	}
	wsCls, err := checkedClass("NSWorkspace")
	if err != nil {
		return err
	}
	urlCls, err := checkedClass("NSURL")
	if err != nil {
		return err
	}
	arrCls, err := checkedClass("NSArray")
	if err != nil {
		return err
	}
	autorelease(func() {
		ws := wsCls.Send(selector("sharedWorkspace"))
		fileURL := urlCls.Send(selector("fileURLWithPath:"), nsString(absPath))
		urls := arrCls.Send(selector("arrayWithObject:"), fileURL)
		ws.Send(selector("activateFileViewerSelectingURLs:"), urls)
	})
	return nil
}

func appExitRequested() bool {
	if s := activeRuntime.Load(); s != nil {
		return atomic.LoadInt32(&s.exitFlag) != 0
	}
	return false
}

type darwinAutostart struct{}

func newAutostartDriver(cfg appSetup) autostartDriver {
	return &darwinAutostart{}
}

func (a *darwinAutostart) strategy() string {
	if !runningFromAppBundle() || bundleID() == "" {
		return autostartBackendLaunchAgent
	}
	major, err := darwinMajorVersion()
	if err != nil || major < 13 {
		return autostartBackendLaunchAgent
	}
	return autostartBackendSMAppService
}

func (a *darwinAutostart) enable(id string, args []string) error {
	switch a.strategy() {
	case autostartBackendSMAppService:
		if err := smAppServiceRegister(); err == nil {
			return nil
		} else if !errors.Is(err, errSMAppServiceUnavailable) {
			return fmt.Errorf("appkit: autostart: SMAppService register: %w", err)
		}
		fallthrough
	default:
		return a.enableLaunchAgent(id, args)
	}
}

func (a *darwinAutostart) disable() error {
	var errs []error
	if a.strategy() == autostartBackendSMAppService {
		if err := smAppServiceUnregister(); err != nil &&
			!errors.Is(err, errSMAppServiceUnavailable) &&
			!errors.Is(err, errSMAppServiceNotRegistered) {
			errs = append(errs, fmt.Errorf("appkit: autostart: SMAppService unregister: %w", err))
		}
	}
	if err := a.disableLaunchAgent(); err != nil {
		errs = append(errs, err)
	}
	return errors.Join(errs...)
}

func (a *darwinAutostart) status() (bool, string, string) {
	if a.strategy() == autostartBackendSMAppService {
		enabled, err := smAppServiceIsEnabled()
		if err == nil && enabled {
			return true, bundleID(), autostartBackendSMAppService
		}
	}
	if path, ok := a.findLaunchAgent(); ok {
		return true, path, autostartBackendLaunchAgent
	}
	return false, "", ""
}

func (a *darwinAutostart) launchAgentsDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("appkit: autostart: %w", err)
	}
	return filepath.Join(home, "Library", "LaunchAgents"), nil
}

func (a *darwinAutostart) enableLaunchAgent(id string, args []string) error {
	exe, err := executablePath()
	if err != nil {
		return err
	}
	dir, err := a.launchAgentsDir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("appkit: autostart: create LaunchAgents dir: %w", err)
	}
	path := filepath.Join(dir, id+".plist")
	body, err := launchAgentPlist(id, exe, args)
	if err != nil {
		return err
	}
	if err := atomicWriteFile(path, body, 0o644); err != nil {
		return fmt.Errorf("appkit: autostart: write plist: %w", err)
	}
	_ = launchctlBootstrap(path)
	return nil
}

func (a *darwinAutostart) disableLaunchAgent() error {
	path, ok := a.findLaunchAgent()
	if !ok {
		return nil
	}
	_ = launchctlBootout(path)
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("appkit: autostart: remove plist: %w", err)
	}
	return nil
}

func (a *darwinAutostart) findLaunchAgent() (string, bool) {
	dir, err := a.launchAgentsDir()
	if err != nil {
		return "", false
	}
	exe, err := executablePath()
	if err != nil {
		return "", false
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return "", false
		}
		return "", false
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".plist") {
			continue
		}
		full := filepath.Join(dir, e.Name())
		data, err := os.ReadFile(full)
		if err != nil {
			continue
		}
		if plistFirstProgramArg(data) == exe {
			return full, true
		}
	}
	return "", false
}

func runningFromAppBundle() bool {
	exe, err := executablePath()
	if err != nil {
		return false
	}
	macOSDir := filepath.Dir(exe)
	contentsDir := filepath.Dir(macOSDir)
	appDir := filepath.Dir(contentsDir)
	return filepath.Base(macOSDir) == "MacOS" &&
		filepath.Base(contentsDir) == "Contents" &&
		strings.HasSuffix(appDir, ".app")
}

func bundleID() string {
	exe, err := executablePath()
	if err != nil {
		return ""
	}
	macOSDir := filepath.Dir(exe)
	appDir := filepath.Dir(filepath.Dir(macOSDir))
	if filepath.Base(macOSDir) != "MacOS" || !strings.HasSuffix(appDir, ".app") {
		return ""
	}
	data, err := os.ReadFile(filepath.Join(appDir, "Contents", "Info.plist"))
	if err != nil {
		return ""
	}
	return plistStringForKey(data, "CFBundleIdentifier")
}

func plistStringForKey(data []byte, want string) string {
	dec := xml.NewDecoder(strings.NewReader(string(data)))
	dec.Strict = false
	var inDict, captureKey bool
	var lastKey string
	for {
		tok, err := dec.Token()
		if err != nil {
			return ""
		}
		switch t := tok.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "dict":
				inDict = true
			case "key":
				if inDict {
					captureKey = true
				}
			case "string":
				if lastKey == want {
					var s string
					if err := dec.DecodeElement(&s, &t); err == nil {
						return s
					}
					return ""
				}
			}
		case xml.CharData:
			if captureKey {
				lastKey = string(t)
				captureKey = false
			}
		}
	}
}

func darwinMajorVersion() (int, error) {
	out, err := exec.Command("sw_vers", "-productVersion").Output()
	if err != nil {
		return 0, err
	}
	ver := strings.TrimSpace(string(out))
	if i := strings.IndexByte(ver, '.'); i > 0 {
		ver = ver[:i]
	}
	return strconv.Atoi(ver)
}

var launchctlBootstrap = func(plistPath string) error {
	target := fmt.Sprintf("gui/%d", os.Getuid())
	return exec.Command("launchctl", "bootstrap", target, plistPath).Run()
}

var launchctlBootout = func(plistPath string) error {
	target := fmt.Sprintf("gui/%d", os.Getuid())
	return exec.Command("launchctl", "bootout", target, plistPath).Run()
}

func launchAgentPlist(label, exe string, args []string) ([]byte, error) {
	progArgs := append([]string{exe}, args...)
	var sb strings.Builder
	sb.WriteString(`<?xml version="1.0" encoding="UTF-8"?>` + "\n")
	sb.WriteString(`<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">` + "\n")
	sb.WriteString(`<plist version="1.0">` + "\n")
	sb.WriteString("  <dict>\n")
	sb.WriteString("    <key>Label</key>\n")
	fmt.Fprintf(&sb, "    <string>%s</string>\n", xmlEscape(label))
	sb.WriteString("    <key>ProgramArguments</key>\n")
	sb.WriteString("    <array>\n")
	for _, a := range progArgs {
		fmt.Fprintf(&sb, "      <string>%s</string>\n", xmlEscape(a))
	}
	sb.WriteString("    </array>\n")
	sb.WriteString("    <key>RunAtLoad</key>\n")
	sb.WriteString("    <true/>\n")
	sb.WriteString("    <key>KeepAlive</key>\n")
	sb.WriteString("    <false/>\n")
	sb.WriteString("  </dict>\n")
	sb.WriteString("</plist>\n")
	return []byte(sb.String()), nil
}

func xmlEscape(s string) string {
	var b strings.Builder
	_ = xml.EscapeText(&b, []byte(s))
	return b.String()
}

func plistFirstProgramArg(data []byte) string {
	dec := xml.NewDecoder(strings.NewReader(string(data)))
	dec.Strict = false
	var inDict, inArray, captureKey bool
	var lastKey string
	for {
		tok, err := dec.Token()
		if err != nil {
			return ""
		}
		switch t := tok.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "dict":
				inDict = true
			case "key":
				if inDict {
					captureKey = true
				}
			case "array":
				if lastKey == "ProgramArguments" {
					inArray = true
				}
			case "string":
				if inArray {
					var s string
					if err := dec.DecodeElement(&s, &t); err == nil {
						return s
					}
					return ""
				}
			}
		case xml.CharData:
			if captureKey {
				lastKey = string(t)
				captureKey = false
			}
		case xml.EndElement:
			if t.Name.Local == "array" && inArray {
				return ""
			}
		}
	}
}

const (
	smAppServiceStatusNotRegistered    = 0
	smAppServiceStatusEnabled          = 1
	smAppServiceStatusRequiresApproval = 2
	smAppServiceStatusNotFound         = 3
)

var (
	errSMAppServiceUnavailable      = errors.New("appkit: SMAppService unavailable on this macOS")
	errSMAppServiceNotRegistered    = errors.New("appkit: SMAppService not registered")
	errSMAppServiceRequiresApproval = errors.New("appkit: SMAppService requires user approval in System Settings")
)

func smAppService() objc.ID {
	if objcClass("SMAppService") == 0 {
		return 0
	}
	return objcClass("SMAppService").Send(selector("mainAppService"))
}

func smAppServiceRegister() error {
	svc := smAppService()
	if svc == 0 {
		return errSMAppServiceUnavailable
	}
	var errID objc.ID
	if svc.Send(selector("registerAndReturnError:"), unsafe.Pointer(&errID)) != 0 {
		return nil
	}
	return smAppServiceFailure(errID, "register")
}

func smAppServiceUnregister() error {
	svc := smAppService()
	if svc == 0 {
		return errSMAppServiceUnavailable
	}
	switch int(svc.Send(selector("status"))) {
	case smAppServiceStatusNotRegistered, smAppServiceStatusNotFound:
		return errSMAppServiceNotRegistered
	}
	var errID objc.ID
	if svc.Send(selector("unregisterAndReturnError:"), unsafe.Pointer(&errID)) != 0 {
		return nil
	}
	return smAppServiceFailure(errID, "unregister")
}

func smAppServiceIsEnabled() (bool, error) {
	svc := smAppService()
	if svc == 0 {
		return false, errSMAppServiceUnavailable
	}
	switch int(svc.Send(selector("status"))) {
	case smAppServiceStatusEnabled:
		return true, nil
	case smAppServiceStatusRequiresApproval:
		return false, errSMAppServiceRequiresApproval
	default:
		return false, nil
	}
}

func smAppServiceFailure(errID objc.ID, what string) error {
	if errID != 0 {
		if desc := errID.Send(selector("localizedDescription")); desc != 0 {
			if s := cString(desc.Send(selector("UTF8String"))); s != "" {
				return errors.New("appkit: SMAppService " + what + ": " + s)
			}
		}
	}
	return errors.New("appkit: SMAppService " + what + " failed")
}

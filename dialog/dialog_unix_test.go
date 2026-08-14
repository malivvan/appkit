//go:build linux || freebsd || netbsd

package dialog

import (
	"runtime"
	"testing"
)

func TestGTKSmoke(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	if !gtkReady() {
		t.Skipf("GTK unavailable or no display (init error: %v)", initErr)
	}
	t.Logf("gtk4=%v", gtk4)

	dlg := gtkFileChooserNativeNew("smoke", 0, gtkFileChooserActionOpen, "_Open", "_Cancel")
	if dlg == 0 {
		t.Fatal("gtk_file_chooser_native_new returned nil")
	}
	addExtensionFilter(dlg, []string{"png", ".jpg"})
	setFolder(dlg, t.TempDir())
	gObjectUnref(dlg)
}

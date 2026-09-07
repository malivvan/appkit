package dialog

import (
	"testing"
)

func TestFileDialogComPlumbing(t *testing.T) {
	if err := ensureInit(); err != nil {
		t.Skipf("COM unavailable: %v", err)
	}
	coInitializeEx(0, coinitApartmentThreaded)

	var pdlg uintptr
	hr := coCreateInstance(&clsidFileOpenDialog, 0, clsctxInprocServer, &iidIFileOpenDialog, &pdlg)
	if hr < 0 || pdlg == 0 {
		t.Fatalf("CoCreateInstance failed: 0x%08X", uint32(hr))
	}
	dlg := (*fileDialog)(ptr(pdlg))
	defer dlg.Release()

	dlg.SetOptions(dlg.GetOptions() | fosForceFilesystem | fosPickFolders | fosAllowMultiSelect)
	got := dlg.GetOptions()
	if got&fosPickFolders == 0 || got&fosAllowMultiSelect == 0 {
		t.Fatalf("options roundtrip lost bits: 0x%08X", got)
	}

	dlg.SetTitle(utf16Ptr("Pick"))
	if keep := addFileTypeFilters(dlg, []string{"png", ".jpg"}); keep == nil {
		t.Fatal("applyFileTypes returned no spec for png/.jpg")
	}
	if keep := addNamedFilters(dlg, []FileFilter{{Name: "Images", Extensions: []string{"png", "jpg"}}}); keep == nil {
		t.Fatal("applyFileFilters returned no spec for a named filter")
	}
}

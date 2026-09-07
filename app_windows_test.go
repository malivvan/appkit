package appkit

import (
	"testing"
)

func TestWindowsBinding(t *testing.T) {
	err := openEnsureInit()
	if err != nil {
		t.Fatalf("openEnsureInit: %v", err)
	}
	if shellExecuteW == nil {
		t.Fatal("ShellExecuteW was not resolved")
	}
}

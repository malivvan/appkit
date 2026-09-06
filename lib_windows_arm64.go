//go:build windows && arm64

package appkit

import (
	"unsafe"

	"github.com/ebitengine/purego"
)

func (i *controller) putBounds(r rect) {
	lo := uintptr(uint32(r.Left)) | uintptr(uint32(r.Top))<<32
	hi := uintptr(uint32(r.Right)) | uintptr(uint32(r.Bottom))<<32
	purego.SyscallN(i.vtbl.PutBounds, uintptr(unsafe.Pointer(i)), lo, hi)
}

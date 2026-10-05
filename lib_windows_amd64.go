//go:build windows && amd64

package appkit

import (
	"unsafe"

	"github.com/malivvan/purego"
)

func (i *controller) putBounds(r rect) {
	purego.SyscallN(i.vtbl.PutBounds, uintptr(unsafe.Pointer(i)), uintptr(unsafe.Pointer(&r)))
}

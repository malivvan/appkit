//go:build windows && 386

package appkit

import (
	"unsafe"

	"github.com/malivvan/purego"
)

func (i *controller) putBounds(r rect) {
	purego.SyscallN(i.vtbl.PutBounds,
		uintptr(unsafe.Pointer(i)),
		uintptr(uint32(r.Left)),
		uintptr(uint32(r.Top)),
		uintptr(uint32(r.Right)),
		uintptr(uint32(r.Bottom)))
}

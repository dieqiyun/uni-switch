//go:build windows && !nogui

package fonts

import (
	"fmt"
	"runtime"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

const Available = true

// DirectWrite works without cgo, as the Windows production build does.
// The method order is the IDWrite interfaces in the Windows SDK's dwrite.h.
type dwrite struct{ vtbl *[16]uintptr }

//go:uintptrescapes
func (p *dwrite) call(method int, args ...uintptr) uintptr {
	r, _, _ := syscall.SyscallN(p.vtbl[method], append([]uintptr{uintptr(unsafe.Pointer(p))}, args...)...)
	runtime.KeepAlive(p)
	return r
}

func (p *dwrite) release()   { p.call(2) }
func failed(hr uintptr) bool { return int32(hr) < 0 }

func installed() ([]Face, error) {
	create := windows.NewLazySystemDLL("dwrite.dll").NewProc("DWriteCreateFactory")
	if err := create.Find(); err != nil {
		return nil, err
	}
	iid := windows.GUID{Data1: 0xb859ee5a, Data2: 0xd838, Data3: 0x4b5b, Data4: [8]byte{0xa2, 0xe8, 0x1a, 0xdc, 0x7d, 0x93, 0xdb, 0x48}}
	var factory *dwrite
	hr, _, _ := create.Call(0, uintptr(unsafe.Pointer(&iid)), uintptr(unsafe.Pointer(&factory)))
	if failed(hr) {
		return nil, fmt.Errorf("DirectWrite factory: HRESULT %#x", uint32(hr))
	}
	defer factory.release()
	var collection *dwrite
	// checkForUpdates asks DirectWrite to notice an install or removal.
	if hr = factory.call(3, uintptr(unsafe.Pointer(&collection)), 1); failed(hr) {
		return nil, fmt.Errorf("DirectWrite fonts: HRESULT %#x", uint32(hr))
	}
	defer collection.release()
	var faces []Face
	for i, n := uintptr(0), collection.call(3); i < n; i++ {
		var family *dwrite
		if failed(collection.call(4, i, uintptr(unsafe.Pointer(&family)))) {
			continue
		}
		faces = append(faces, familyFaces(family)...)
		family.release()
	}
	return faces, nil
}

func familyFaces(family *dwrite) []Face {
	name := fontString(family, 6)
	if name == "" {
		return nil
	}
	var faces []Face
	for i, n := uintptr(0), family.call(4); i < n; i++ {
		var font *dwrite
		if failed(family.call(5, i, uintptr(unsafe.Pointer(&font)))) {
			continue
		}
		// Synthesised bold/italic is not an installed style to offer.
		if font.call(10) == 0 {
			style := "normal"
			switch font.call(6) {
			case 1:
				style = "oblique"
			case 2:
				style = "italic"
			}
			faces = append(faces, Face{Family: name, Name: fontString(font, 8), Weight: float64(font.call(4)), Style: style, Stretch: width(int(font.call(5)))})
		}
		font.release()
	}
	return faces
}

// Stable English names where present, otherwise the font's first locale.
// CSS accepts these same names regardless of the page's language.
func fontString(owner *dwrite, method int) string {
	var names *dwrite
	if failed(owner.call(method, uintptr(unsafe.Pointer(&names)))) {
		return ""
	}
	defer names.release()
	if names.call(3) == 0 {
		return ""
	}
	locale, _ := windows.UTF16PtrFromString("en-us")
	var index, exists, size uint32
	if failed(names.call(4, uintptr(unsafe.Pointer(locale)), uintptr(unsafe.Pointer(&index)), uintptr(unsafe.Pointer(&exists)))) || exists == 0 {
		index = 0
	}
	if failed(names.call(7, uintptr(index), uintptr(unsafe.Pointer(&size)))) || size > 4096 {
		return ""
	}
	buf := make([]uint16, size+1)
	if failed(names.call(8, uintptr(index), uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))) {
		return ""
	}
	return windows.UTF16ToString(buf)
}

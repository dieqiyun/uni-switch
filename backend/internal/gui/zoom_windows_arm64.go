//go:build windows && arm64 && !nogui

package gui

import (
	"errors"
	"fmt"
	"log"
	"math"
	"sync"
	"syscall"
	"unsafe"

	"github.com/wailsapp/wails/v3/pkg/application"
	"golang.org/x/sys/windows"
)

// setPageZoom zooms w's page to z, the text size. On Windows on ARM,
// Wails' SetZoom (and the Zoom option a window is made with) never reaches
// the page: WebView2's put_ZoomFactor takes its double by value, which the
// ARM64 ABI passes in d0, and Go's syscall fills only x0–x7
// (golang.org/issue/62583), so Wails refuses the call and only logs it. The
// choice was saved and shown picked, and the page stayed at 100% (#2212).
// uni-switch makes the call itself, through a two-instruction thunk that moves
// the double's bits from x1 into d0.
func setPageZoom(w *application.WebviewWindow, z float64) {
	if err := putZoomFactor(w, z); err != nil {
		log.Println("text size:", err)
	}
}

// zoomsAfterLoad: a window's Zoom option is lost here, so its text size is
// put on its page once WebView2 has made the page's controller.
const zoomsAfterLoad = true

// putZoomFactor calls ICoreWebView2Controller::put_ZoomFactor(z) on w's
// WebView2 controller.
func putZoomFactor(w *application.WebviewWindow, z float64) error {
	ctrl := webviewController(w)
	if ctrl == nil {
		return errors.New("no WebView2 controller yet")
	}
	thunk, err := doubleThunk()
	if err != nil {
		return err
	}
	// ICoreWebView2ControllerVtbl: QueryInterface, AddRef, Release,
	// get_IsVisible, put_IsVisible, get_Bounds, put_Bounds,
	// get_ZoomFactor, put_ZoomFactor (WebView2.h; Wails'
	// ICoreWebView2ControllerVtbl in the same order)
	const putZoomFactorSlot = 8
	vtbl := *(*unsafe.Pointer)(ctrl)
	fn := *(*uintptr)(unsafe.Add(vtbl, putZoomFactorSlot*unsafe.Sizeof(uintptr(0))))
	hr, _, _ := syscall.SyscallN(thunk, uintptr(ctrl), uintptr(math.Float64bits(z)), fn)
	if int32(hr) < 0 {
		return fmt.Errorf("put_ZoomFactor(%g): %w", z, windows.Errno(hr))
	}
	return nil
}

// webviewController is w's ICoreWebView2Controller, nil before WebView2
// has made it (chromiumPart). A Wails without it gives nil, and the text
// size stays as Wails left it.
func webviewController(w *application.WebviewWindow) unsafe.Pointer {
	return chromiumPart(w, "controller")
}

var (
	flushInstructionCache = windows.NewLazySystemDLL("kernel32.dll").NewProc("FlushInstructionCache")

	thunkOnce sync.Once
	thunkAddr uintptr
	thunkErr  error
)

// doubleThunk is code that calls x2 with x0 as it is and the bits in x1 as
// the double in d0, so a COM method taking (this, double) is called as
// SyscallN(thunk, this, bits, method):
//
//	fmov d0, x1
//	br   x2
//
// br leaves the return address the caller's bl put in x30, so the method
// returns straight to Go's asmstdcall with its HRESULT in x0.
func doubleThunk() (uintptr, error) {
	thunkOnce.Do(func() {
		code := []uint32{0x9E670020, 0xD61F0040}
		size := uintptr(len(code) * 4)
		addr, err := windows.VirtualAlloc(0, size, windows.MEM_COMMIT|windows.MEM_RESERVE, windows.PAGE_READWRITE)
		if err != nil {
			thunkErr = fmt.Errorf("VirtualAlloc: %w", err)
			return
		}
		// addr is memory VirtualAlloc gave, not Go's
		copy(unsafe.Slice((*uint32)(*(*unsafe.Pointer)(unsafe.Pointer(&addr))), len(code)), code)
		var old uint32
		if err := windows.VirtualProtect(addr, size, windows.PAGE_EXECUTE_READ, &old); err != nil {
			thunkErr = fmt.Errorf("VirtualProtect: %w", err)
			return
		}
		if ok, _, err := flushInstructionCache.Call(uintptr(windows.CurrentProcess()), addr, size); ok == 0 {
			thunkErr = fmt.Errorf("FlushInstructionCache: %w", err)
			return
		}
		thunkAddr = addr
	})
	return thunkAddr, thunkErr
}

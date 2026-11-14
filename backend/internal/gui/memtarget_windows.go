//go:build windows && !nogui

package gui

import (
	"fmt"
	"log"
	"reflect"
	"syscall"
	"unsafe"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
	"golang.org/x/sys/windows"
)

// A closed window on Windows is hidden, its WebView2 kept: the tray panel
// and the main window both stay loaded, so each opens at once. Kept at
// WebView2's normal memory target, a hidden one held what it last drew:
// the GPU process's tiles and the renderer's caches, ~100 MB more after
// the window had been open than before (molamaker11 on X: "后台占用快接近
// 600MB"). lowWhileHidden puts a window's WebView2 on the low memory
// target while it is hidden, the level Microsoft gives for a webview out
// of sight (ICoreWebView2_19::put_MemoryUsageTargetLevel), and back on
// normal as it is shown. The page stays loaded and running, unlike
// lightweight mode, which lets the webview go.

// lowWhileHidden sets w's memory target on each hide and show, and on the
// low one once its first page has come while it is still hidden (a window
// made hidden, as both are at the start, is never sent a hide).
func lowWhileHidden(w *application.WebviewWindow) {
	set := func(low bool) {
		application.InvokeAsync(func() {
			if low && w.IsVisible() {
				return // shown again meanwhile
			}
			if err := memoryTarget(w, low); err != nil {
				log.Printf("webview memory target (%s window): %v", w.Name(), err)
			}
		})
	}
	w.OnWindowEvent(events.Windows.WindowHide, func(*application.WindowEvent) { set(true) })
	w.OnWindowEvent(events.Windows.WindowShow, func(*application.WindowEvent) { set(false) })
	w.OnWindowEvent(events.Windows.WebViewNavigationCompleted, func(*application.WindowEvent) { set(true) })
}

// WebView2's COREWEBVIEW2_MEMORY_USAGE_TARGET_LEVEL values.
const (
	memoryTargetNormal = 0
	memoryTargetLow    = 1
)

// iidCoreWebView2_19 is ICoreWebView2_19's interface id (WebView2.h).
var iidCoreWebView2_19 = windows.GUID{Data1: 0x6921f954, Data2: 0x79b0, Data3: 0x437f,
	Data4: [8]byte{0xa9, 0x97, 0xc8, 0x58, 0x11, 0x89, 0x7c, 0x68}}

// memoryTarget puts w's WebView2 on the low memory target, or back on the
// normal one; on the main thread. A runtime older than ICoreWebView2_19
// (WebView2 1.0.2210) answers E_NOINTERFACE, and nothing changes.
func memoryTarget(w *application.WebviewWindow, low bool) error {
	wv := chromiumPart(w, "webview")
	if wv == nil {
		return nil // no webview yet: its page's coming sets it
	}
	// Slots in ICoreWebView2_19Vtbl, which holds every method it inherits:
	// IUnknown's three, then ICoreWebView2 to _18's, then
	// get_MemoryUsageTargetLevel (119) and put_MemoryUsageTargetLevel (120),
	// counted in WebView2.h of SDK 1.0.2210.55.
	const queryInterface, release, putLevel = 0, 2, 120
	var wv19 unsafe.Pointer
	if hr, _, _ := syscall.SyscallN(comMethod(wv, queryInterface), uintptr(wv),
		uintptr(unsafe.Pointer(&iidCoreWebView2_19)), uintptr(unsafe.Pointer(&wv19))); int32(hr) < 0 || wv19 == nil {
		return fmt.Errorf("ICoreWebView2_19: %w", windows.Errno(hr))
	}
	defer syscall.SyscallN(comMethod(wv19, release), uintptr(wv19))
	level := uintptr(memoryTargetNormal)
	if low {
		level = memoryTargetLow
	}
	if hr, _, _ := syscall.SyscallN(comMethod(wv19, putLevel), uintptr(wv19), level); int32(hr) < 0 {
		return fmt.Errorf("put_MemoryUsageTargetLevel(%d): %w", level, windows.Errno(hr))
	}
	return nil
}

// comMethod is the slot'th method of the COM object at p's vtable.
func comMethod(p unsafe.Pointer, slot uintptr) uintptr {
	vtbl := *(*unsafe.Pointer)(p)
	return *(*uintptr)(unsafe.Add(vtbl, slot*unsafe.Sizeof(uintptr(0))))
}

// chromiumPart is one of the COM objects Wails' edge.Chromium holds for
// w ("controller", "webview"), nil before WebView2 has made it. Wails
// keeps them unexported: WebviewWindow.impl, a *windowsWebviewWindow,
// holds chromium, an *edge.Chromium (Wails v3.0.0-beta.24). A Wails
// without that path gives nil.
func chromiumPart(w *application.WebviewWindow, part string) unsafe.Pointer {
	if w == nil {
		return nil
	}
	v := reflect.ValueOf(w).Elem()
	for _, name := range []string{"impl", "chromium", part} {
		if v.Kind() != reflect.Struct {
			return nil
		}
		v = v.FieldByName(name)
		for v.IsValid() && v.Kind() == reflect.Interface {
			if v.IsNil() {
				return nil
			}
			v = v.Elem()
		}
		if !v.IsValid() || v.Kind() != reflect.Pointer || v.IsNil() {
			return nil
		}
		if name != part {
			v = v.Elem()
		}
	}
	return v.UnsafePointer()
}

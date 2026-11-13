//go:build linux && cgo && gtk3 && !nogui

package gui

/*
#cgo pkg-config: gtk+-3.0 wayland-client
#include "clipboard_gtk3.h"
*/
import "C"

import (
	"unsafe"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// copyText puts text on the clipboard and says whether it is there. Wails'
// own copy sets CLIPBOARD and then PRIMARY, which GTK 3 serves empty where
// the compositor has no primary selection (#1881, see clipboard_gtk3.h),
// and its answer is always true. On Wayland the clipboard is read back
// through the compositor, as a paste in another program would read it; a
// copy that can't be read back is not reported as made, so the page tries
// its own ways. X11 (GDK_BACKEND=x11) keeps GTK's word, as before.
func copyText(_ *application.App, text string) bool {
	ct := C.CString(text)
	defer C.free(unsafe.Pointer(ct))
	var display, seat unsafe.Pointer
	wayland := false
	application.InvokeSync(func() {
		C.uni_switch_clipboard_set(ct)
		wayland = C.uni_switch_clipboard_wayland(&display, &seat) != 0
	})
	if !wayland {
		return true
	}
	// off the main thread: GTK answers the read from it when the copy is
	// uni-switch's own
	var out *C.char
	var n C.size_t
	got := C.uni_switch_clipboard_read(display, seat, 2000, &out, &n)
	if got == -2 {
		// a compositor too old to read back from without harm (#2039)
		return true
	}
	var held string
	if out != nil {
		held = C.GoStringN(out, C.int(n))
		C.free(unsafe.Pointer(out))
	}
	return copyHeld(int(got), held, text)
}

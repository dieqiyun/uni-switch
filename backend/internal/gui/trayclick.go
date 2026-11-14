package gui

import (
	"reflect"
	"runtime"
	"strings"
	"unsafe"
)

// menuOpenFrame is the Wails function that tells the tray's click handler
// when Linux's tray host opens the icon's menu (com.canonical.dbusmenu's
// "opened" event). KDE Plasma opens the menu on a right-click, so a click
// counted there opened the quick panel too, which took the focus and shut
// the menu on Wayland at once (#1430).
const menuOpenFrame = "pkg/application.(*linuxSystemTray).Event"

// menuOpening says the click handler was called because the tray's menu is
// opening, not because the icon was clicked: Wails calls the same handler
// for both, and only the call stack tells them apart.
func menuOpening() bool {
	pcs := make([]uintptr, 32)
	n := runtime.Callers(2, pcs)
	frames := runtime.CallersFrames(pcs[:n])
	var names []string
	for {
		f, more := frames.Next()
		names = append(names, f.Function)
		if !more {
			break
		}
	}
	return openedBy(names)
}

// openedBy says one of the callers is the menu's "opened" event.
func openedBy(callers []string) bool {
	for _, c := range callers {
		if strings.HasSuffix(c, menuOpenFrame) {
			return true
		}
	}
	return false
}

// trayImpl is the struct behind a Wails SystemTray's impl, the platform's
// tray (Linux's linuxSystemTray), given the SystemTray itself; not valid
// once a Wails version keeps it elsewhere.
func trayImpl(tray reflect.Value) (impl reflect.Value) {
	defer func() {
		if recover() != nil {
			impl = reflect.Value{}
		}
	}()
	i := tray.FieldByName("impl")
	if !i.IsValid() || i.Kind() != reflect.Interface || i.IsNil() {
		return reflect.Value{}
	}
	p := i.Elem()
	if p.Kind() != reflect.Pointer || p.IsNil() || p.Elem().Kind() != reflect.Struct {
		return reflect.Value{}
	}
	return p.Elem()
}

// clickIn reads the click Wails' Linux tray keeps from the tray host's
// Activate(x, y), lastClickX and lastClickY, given the SystemTray. (0, 0)
// is no click, as Wails reads it too: a host that passes no coordinates
// sends that.
func clickIn(tray reflect.Value) (x int, ok bool) {
	impl := trayImpl(tray)
	if !impl.IsValid() {
		return 0, false
	}
	fx, fy := impl.FieldByName("lastClickX"), impl.FieldByName("lastClickY")
	if !fx.IsValid() || !fy.IsValid() || fx.Kind() != reflect.Int || fy.Kind() != reflect.Int || !fx.CanAddr() {
		return 0, false
	}
	x = *(*int)(unsafe.Pointer(fx.UnsafeAddr()))
	y := *(*int)(unsafe.Pointer(fy.UnsafeAddr()))
	return x, x != 0 || y != 0
}

package agent

import (
	"testing"
	ptr "unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

// setUserEnv sets the variables in the user's environment in the registry,
// and tells Explorer, so an app opened from the Start menu from now on has
// them; nil removes them.
func setUserEnv(env map[string]string) error {
	if testing.Testing() {
		return nil
	}
	k, err := registry.OpenKey(registry.CURRENT_USER, `Environment`, registry.QUERY_VALUE|registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer k.Close()
	for _, n := range cursorLocalVars {
		if v, ok := env[n]; ok {
			err = k.SetStringValue(n, v)
		} else if err = k.DeleteValue(n); err == registry.ErrNotExist {
			err = nil
		}
		if err != nil {
			return err
		}
	}
	const hwndBroadcast, wmSettingChange, smtoAbortIfHung = 0xffff, 0x001A, 0x0002
	what, _ := windows.UTF16PtrFromString("Environment")
	var res uintptr
	windows.NewLazySystemDLL("user32.dll").NewProc("SendMessageTimeoutW").Call(
		hwndBroadcast, wmSettingChange, 0, uintptr(ptr.Pointer(what)), smtoAbortIfHung, 5000, uintptr(ptr.Pointer(&res)))
	return nil
}

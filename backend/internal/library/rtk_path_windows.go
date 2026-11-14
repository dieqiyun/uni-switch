package library

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	ptr "unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

// rtkPathDir is rtk's own folder, added to the user's PATH: a link needs
// Developer Mode or an administrator, and a copy or a hard link is left
// behind when winget upgrades rtk, while its package folder stays the same.
func rtkPathDir(bin string) (dir string, link bool) {
	return filepath.Dir(bin), false
}

// rtkOnPath adds dir to the user's PATH in the registry, where a terminal
// opened from now on takes it up, and to uni-switch's own.
func rtkOnPath(_, dir string, _ bool) error {
	k, err := registry.OpenKey(registry.CURRENT_USER, `Environment`, registry.QUERY_VALUE|registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer k.Close()
	v, typ, err := k.GetStringValue("Path")
	if err != nil && err != registry.ErrNotExist {
		return err
	}
	for _, d := range strings.Split(v, ";") {
		if strings.EqualFold(strings.TrimRight(strings.TrimSpace(d), `\`), strings.TrimRight(dir, `\`)) {
			return nil
		}
	}
	if v = strings.TrimRight(v, ";"); v != "" {
		v += ";"
	}
	v += dir
	// kept REG_EXPAND_SZ, as Windows has it, so %USERPROFILE% in it still works
	if err == registry.ErrNotExist || typ == registry.EXPAND_SZ {
		err = k.SetExpandStringValue("Path", v)
	} else {
		err = k.SetStringValue("Path", v)
	}
	if err != nil {
		return fmt.Errorf("adding %s to your PATH: %w", dir, err)
	}
	os.Setenv("PATH", os.Getenv("PATH")+";"+dir)
	settingChanged()
	return nil
}

// settingChanged tells Explorer the environment changed, so what it starts
// next — a terminal from the Start menu — has the new PATH.
func settingChanged() {
	const hwndBroadcast, wmSettingChange, smtoAbortIfHung = 0xffff, 0x001A, 0x0002
	env, _ := windows.UTF16PtrFromString("Environment")
	var res uintptr
	windows.NewLazySystemDLL("user32.dll").NewProc("SendMessageTimeoutW").Call(
		hwndBroadcast, wmSettingChange, 0, uintptr(ptr.Pointer(env)), smtoAbortIfHung, 5000, uintptr(ptr.Pointer(&res)))
}

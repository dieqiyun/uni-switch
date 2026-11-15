package update

import (
	"fmt"
	"os"
	"path/filepath"
	ptr "unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

// installedShells are PowerShell and cmd, which both take the registry's
// PATH (proc.ShellPath).
func installedShells() []cliShell {
	return []cliShell{{Name: "PowerShell", Default: true}, {Name: "cmd"}}
}

// cliDir is the app's own folder, put first in the user's PATH: a link
// needs Developer Mode or an administrator, and a copy wouldn't follow
// the app's updates.
func cliDir(exe string, _ []string, _ string) string { return filepath.Dir(exe) }

// cliPlace puts dir first in the user's PATH in the registry, where a
// terminal opened from now on takes it up, and tells Explorer. The app's
// uni-switch.com goes there first (com.go), which a terminal waits for; a
// build without it gives an app not named uni-switch.exe a uni-switch.cmd
// (writeShim).
func cliPlace(exe, dir string) error {
	if comLauncher != nil {
		if comOwner(exe) {
			if _, err := writeCom(comLauncher, exe); err != nil {
				return err
			}
		}
	} else if _, err := writeShim(exe); err != nil {
		return err
	}
	k, err := registry.OpenKey(registry.CURRENT_USER, `Environment`, registry.QUERY_VALUE|registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer k.Close()
	v, typ, err := k.GetStringValue("Path")
	if err != nil && err != registry.ErrNotExist {
		return err
	}
	nv := pathFirst(v, dir)
	if nv == v {
		return nil
	}
	// kept REG_EXPAND_SZ, as Windows has it, so %USERPROFILE% in it still works
	if err == registry.ErrNotExist || typ == registry.EXPAND_SZ {
		err = k.SetExpandStringValue("Path", nv)
	} else {
		err = k.SetStringValue("Path", nv)
	}
	if err != nil {
		return fmt.Errorf("adding %s to your PATH: %w", dir, err)
	}
	os.Setenv("PATH", dir+";"+os.Getenv("PATH"))
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

//go:build windows

package edit

import (
	"os/exec"
	"os/user"
	"syscall"
	"unsafe"
)

var moveFileEx = syscall.NewLazyDLL("kernel32.dll").NewProc("MoveFileExW")

func replace(from, to string) error {
	a, e := syscall.UTF16PtrFromString(from)
	if e != nil {
		return e
	}
	b, e := syscall.UTF16PtrFromString(to)
	if e != nil {
		return e
	}
	ok, _, e := moveFileEx.Call(uintptr(unsafe.Pointer(a)), uintptr(unsafe.Pointer(b)), 0x1|0x8)
	if ok == 0 {
		return e
	}
	return nil
}
func syncDirectory(string) error { return nil } // MoveFileExW uses WRITE_THROUGH.
func private(path string, directory bool) error {
	account, e := user.Current()
	if e != nil {
		return fail("permissions", "无法确定当前用户权限")
	}
	grant := "*" + account.Uid + ":F"
	if directory {
		grant = "*" + account.Uid + ":(OI)(CI)F"
	}
	cmd := exec.Command("icacls.exe", path, "/inheritance:r", "/grant:r", grant, "/Q")
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
	if cmd.Run() != nil {
		return fail("permissions", "无法设置当前用户私有权限")
	}
	return nil
}

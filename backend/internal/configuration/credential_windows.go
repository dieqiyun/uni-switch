package configuration

import (
	"errors"
	"golang.org/x/sys/windows"
	"runtime"
	"unsafe"
)

type credential struct {
	Flags, Type     uint32
	Target, Comment *uint16
	Written         windows.Filetime
	Size            uint32
	Blob            *byte
	Persist, Count  uint32
	Attributes      uintptr
	Alias, User     *uint16
}

var credDLL = windows.NewLazySystemDLL("advapi32.dll")

func readCredential(endpoint string) (*string, error) {
	target, err := windows.UTF16PtrFromString("zed:url=" + endpoint)
	if err != nil {
		return nil, err
	}
	var p *credential
	ok, _, e := credDLL.NewProc("CredReadW").Call(uintptr(unsafe.Pointer(target)), 1, 0, uintptr(unsafe.Pointer(&p)))
	if ok == 0 {
		if errors.Is(e, windows.ERROR_NOT_FOUND) {
			return nil, nil
		}
		return nil, fail("credential_store", "无法读取 Zed 的 Windows 凭据")
	}
	defer credDLL.NewProc("CredFree").Call(uintptr(unsafe.Pointer(p)))
	if windows.UTF16PtrToString(p.User) != "Bearer" || p.Size > 4096 {
		return nil, fail("credential_store", "Zed 凭据格式不兼容，已保留")
	}
	s := string(unsafe.Slice(p.Blob, int(p.Size)))
	return &s, nil
}
func writeCredential(endpoint string, key *string) error {
	target, err := windows.UTF16PtrFromString("zed:url=" + endpoint)
	if err != nil {
		return err
	}
	if key == nil {
		ok, _, e := credDLL.NewProc("CredDeleteW").Call(uintptr(unsafe.Pointer(target)), 1, 0)
		if ok == 0 && !errors.Is(e, windows.ERROR_NOT_FOUND) {
			return fail("credential_store", "无法恢复 Zed 的 Windows 凭据")
		}
		return nil
	}
	if len(*key) == 0 || len(*key) > 4096 {
		return fail("credential_store", "Zed 密钥长度无效")
	}
	user, _ := windows.UTF16PtrFromString("Bearer")
	b := []byte(*key)
	c := credential{Type: 1, Target: target, User: user, Size: uint32(len(b)), Blob: &b[0], Persist: 2}
	ok, _, _ := credDLL.NewProc("CredWriteW").Call(uintptr(unsafe.Pointer(&c)), 0)
	runtime.KeepAlive(c)
	runtime.KeepAlive(b)
	if ok == 0 {
		return fail("credential_store", "无法写入 Zed 的 Windows 凭据")
	}
	return nil
}

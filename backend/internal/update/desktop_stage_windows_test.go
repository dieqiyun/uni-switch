package update

import "golang.org/x/sys/windows"

func hiddenForTest(p string) bool {
	u, err := windows.UTF16PtrFromString(p)
	if err != nil {
		return false
	}
	a, err := windows.GetFileAttributes(u)
	return err == nil && a&windows.FILE_ATTRIBUTE_HIDDEN != 0
}

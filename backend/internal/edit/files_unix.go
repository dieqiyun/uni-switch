//go:build !windows

package edit

import "os"

func replace(from, to string) error { return os.Rename(from, to) }
func private(path string, directory bool) error {
	mode := os.FileMode(0600)
	if directory {
		mode = 0700
	}
	if os.Chmod(path, mode) != nil {
		return fail("permissions", "无法设置私有权限")
	}
	return nil
}
func syncDirectory(path string) error {
	f, e := os.Open(path)
	if e != nil {
		return fail("file_error", "无法打开配置目录")
	}
	defer f.Close()
	if f.Sync() != nil {
		return fail("file_error", "无法同步配置目录")
	}
	return nil
}

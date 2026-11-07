package edit

import (
	"os"
	"path/filepath"
)

func CheckPath(path string) error {
	if !filepath.IsAbs(path) {
		return fail("invalid_path", "配置路径必须是绝对路径")
	}
	for p := path; ; p = filepath.Dir(p) {
		if fi, e := os.Lstat(p); e == nil {
			if fi.Mode()&os.ModeSymlink != 0 || reparse(fi) {
				return fail("symlink", "配置路径包含符号链接或重解析点，请选择实际目录")
			}
		}
		if filepath.Dir(p) == p {
			break
		}
	}
	if fi, e := os.Stat(path); e == nil && fi.IsDir() {
		return fail("invalid_path", "配置文件路径被目录占用")
	}
	return nil
}

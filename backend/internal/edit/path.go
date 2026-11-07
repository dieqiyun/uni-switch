package edit

import (
	"os"
	"path/filepath"
	"runtime"
)

func CheckPath(path string) error {
	if !filepath.IsAbs(path) {
		return fail("invalid_path", "配置路径必须是绝对路径")
	}
	for p := path; ; p = filepath.Dir(p) {
		if fi, e := os.Lstat(p); e == nil {
			if (fi.Mode()&os.ModeSymlink != 0 || reparse(fi)) && !systemAlias(p) {
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

// macOS exposes these system directories through fixed links into /private.
// Only these exact root aliases are accepted; links in user/config directories
// are still rejected by the ancestor walk above.
func systemAlias(path string) bool {
	if runtime.GOOS != "darwin" || (path != "/var" && path != "/tmp" && path != "/etc") {
		return false
	}
	want := "/private" + path
	target, e := os.Readlink(path)
	if e != nil {
		return false
	}
	if !filepath.IsAbs(target) {
		target = filepath.Join(filepath.Dir(path), target)
	}
	resolved, e := filepath.EvalSymlinks(path)
	return e == nil && filepath.Clean(target) == want && resolved == want
}

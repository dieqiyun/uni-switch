// Package edit implements our file transaction primitives following the
// reviewed edit.WriteAtomic/Atomically and agent.Applied snapshot design.
package edit

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

type Error struct{ Code, Message string }

func (e *Error) Error() string        { return e.Message }
func fail(code, message string) error { return &Error{code, message} }

type Change struct {
	Path   string  `json:"path"`
	Before *string `json:"before"`
	After  *string `json:"after"`
}

func Read(path string) (*string, error) {
	b, e := os.ReadFile(path)
	if errors.Is(e, fs.ErrNotExist) {
		return nil, nil
	}
	if e != nil {
		return nil, fail("file_error", "无法读取配置文件")
	}
	if !utf8.Valid(b) {
		return nil, fail("invalid_encoding", "配置不是有效 UTF-8")
	}
	s := strings.TrimPrefix(string(b), "\ufeff")
	return &s, nil
}
func same(a, b *string) bool { return a == nil && b == nil || a != nil && b != nil && *a == *b }
func WriteAtomic(path string, content *string) error {
	if e := CheckPath(path); e != nil {
		return e
	}
	if !filepath.IsAbs(path) {
		return fail("invalid_path", "配置路径必须是绝对路径")
	}
	if fi, e := os.Lstat(path); e == nil && fi.Mode()&os.ModeSymlink != 0 {
		return fail("symlink", "配置是符号链接，请选择实际目录")
	}
	if content == nil {
		e := os.Remove(path)
		if errors.Is(e, fs.ErrNotExist) {
			return nil
		}
		if e != nil {
			return fail("file_error", "无法删除配置文件")
		}
		return nil
	}
	parent := filepath.Dir(path)
	if os.MkdirAll(parent, 0700) != nil {
		return fail("file_error", "无法创建配置目录")
	}
	tmp, e := os.CreateTemp(parent, ".uni-switch-*")
	if e != nil {
		return fail("file_error", "无法创建配置临时文件")
	}
	name := tmp.Name()
	defer os.Remove(name)
	if e = private(name, false); e != nil {
		tmp.Close()
		return e
	}
	if _, e = tmp.WriteString(*content); e == nil {
		e = tmp.Sync()
	}
	closeErr := tmp.Close()
	if e != nil || closeErr != nil {
		return fail("file_error", "无法写入配置临时文件")
	}
	if e = replace(name, path); e != nil {
		return fail("file_error", "无法原子替换配置文件")
	}
	return syncDirectory(parent)
}
func PrivateDirectory(path string) error {
	if os.MkdirAll(path, 0700) != nil {
		return fail("permissions", "无法创建私有目录")
	}
	return private(path, true)
}

// Rollback continues past conflicts and restores every other safe file. A
// conflict is never treated as a successful transaction or accepted snapshot.
func Rollback(changes []Change) error {
	var first error
	for i := len(changes) - 1; i >= 0; i-- {
		c := changes[i]
		if e := CheckPath(c.Path); e != nil {
			if first == nil {
				first = e
			}
			continue
		}
		now, e := Read(c.Path)
		if e == nil && !same(now, c.Before) {
			if !same(now, c.After) {
				e = fail("recovery_conflict", "恢复时发现外部修改，已保留该文件并继续恢复其他文件")
			} else {
				e = WriteAtomic(c.Path, c.Before)
			}
		}
		if first == nil && e != nil {
			first = e
		}
	}
	return first
}
func Apply(changes []Change) error { return applyWith(changes, WriteAtomic) }
func applyWith(changes []Change, write func(string, *string) error) error {
	for _, c := range changes {
		now, e := Read(c.Path)
		if e != nil {
			return e
		}
		if !same(now, c.Before) {
			return fail("external_change", "配置刚刚被其他程序修改，请刷新后重试")
		}
	}
	for _, c := range changes {
		now, e := Read(c.Path)
		if e == nil && !same(now, c.Before) {
			e = fail("external_change", "写入期间发现外部修改")
		}
		if e == nil {
			e = write(c.Path, c.After)
		}
		if e == nil {
			now, e = Read(c.Path)
			if e == nil && !same(now, c.After) {
				e = fail("verify_failed", "配置回读校验失败")
			}
		}
		if e != nil {
			if back := Rollback(changes); back != nil {
				return fail("recovery_required", "部分配置无法恢复，操作日志和备份已保留")
			}
			return e
		}
	}
	return nil
}

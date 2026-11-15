//go:build !windows

package edit

import (
	"os"
	"syscall"
)

// hardLinked reports whether the file at path has other names too. Where
// the system gives no link count it is taken to have none.
func hardLinked(path string) bool {
	fi, err := os.Stat(path)
	if err != nil || !fi.Mode().IsRegular() {
		return false
	}
	st, ok := fi.Sys().(*syscall.Stat_t)
	return ok && st.Nlink > 1
}

// keepGroup gives the temp file that replaces the file at st the group
// that file had: a new file takes its folder's group, so a config shared
// with a group (Aside's group-writable models.json, #1499) would lose it on
// the rename. A group the user can't give is left as it was made.
func keepGroup(tmp *os.File, st os.FileInfo) {
	old, ok := st.Sys().(*syscall.Stat_t)
	if !ok {
		return
	}
	now, err := tmp.Stat()
	if err != nil {
		return
	}
	if cur, ok := now.Sys().(*syscall.Stat_t); ok && cur.Gid != old.Gid {
		_ = tmp.Chown(-1, int(old.Gid))
	}
}

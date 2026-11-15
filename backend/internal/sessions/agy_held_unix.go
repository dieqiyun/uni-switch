//go:build unix

package sessions

import (
	"errors"
	"io/fs"
	"os"
	"syscall"
)

// agyHeld says whether a running agy may hold the presence lock p: it
// flocks presence/<id>.lock for as long as it has the conversation open.
// No lock is not held; a lock that can't be opened or tried is unknown, and
// counts as held.
func agyHeld(p string) bool {
	f, err := os.Open(p)
	if errors.Is(err, fs.ErrNotExist) {
		return false
	}
	if err != nil {
		return true
	}
	defer f.Close()
	if syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB) != nil {
		return true
	}
	syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
	return false
}

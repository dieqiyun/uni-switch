//go:build unix

package sessions

import (
	"os"
	"syscall"
)

// syscallFlock holds f's lock as agy holds a conversation's presence lock.
func syscallFlock(f *os.File) error {
	return syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
}

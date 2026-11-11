//go:build !windows

package agent

import "golang.org/x/sys/unix"

// writable says whether this user can write in dir.
func writable(dir string) bool { return unix.Access(dir, unix.W_OK) == nil }

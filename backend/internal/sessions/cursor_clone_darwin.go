//go:build darwin

package sessions

import "golang.org/x/sys/unix"

// cloneFile makes to share from's blocks on APFS, so a snapshot of a
// multi-GB Cursor database costs no disk until one side is written. It
// fails off APFS or across volumes, and the caller then copies.
func cloneFile(from, to string) error { return unix.Clonefile(from, to, unix.CLONE_NOFOLLOW) }

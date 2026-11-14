package handoff

import "golang.org/x/sys/unix"

// peerUID is the user of the process at the other end of the unix socket
// fd, -1 when it can't be told.
func peerUID(fd int) int {
	cred, err := unix.GetsockoptXucred(fd, unix.SOL_LOCAL, unix.LOCAL_PEERCRED)
	if err != nil {
		return -1
	}
	return int(cred.Uid)
}

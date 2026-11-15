package handoff

import "golang.org/x/sys/unix"

// peerUID is the user of the process at the other end of the unix socket
// fd, -1 when it can't be told.
func peerUID(fd int) int {
	cred, err := unix.GetsockoptUcred(fd, unix.SOL_SOCKET, unix.SO_PEERCRED)
	if err != nil {
		return -1
	}
	return int(cred.Uid)
}

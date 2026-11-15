//go:build darwin || linux

package handoff

import (
	"bufio"
	"net"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/unix"
)

// Give reads the number of f to send it along (unix.UnixRights), and f is
// the copy of a listening socket (ListenerFile, from TCPListener.File)
// that shares the listener's open file description. os.File.Fd puts such
// a description in blocking mode (the net package hands a socket's File
// out expecting that), which left the gateway's own listener blocking: its
// accept then slept in the syscall once nothing connected, past Close,
// so a handover's end never finished (the full-suite hang of
// TestRestartHandsTheGatewayOver, #4107). The listener keeps its
// non-blocking mode across Give.
func TestGiveKeepsTheListenerNonblocking(t *testing.T) {
	ln, err := net.ListenTCP("tcp", &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	f, err := ln.File()
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	la := &net.UnixAddr{Name: filepath.Join(t.TempDir(), "s"), Net: "unix"}
	ul, err := net.ListenUnix("unix", la)
	if err != nil {
		t.Fatal(err)
	}
	defer ul.Close()
	client, err := net.DialUnix("unix", nil, la)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	server, err := ul.AcceptUnix()
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	c := &Conn{c: server, r: bufio.NewReader(server)}

	nonblocking := func() bool {
		t.Helper()
		rc, err := ln.SyscallConn()
		if err != nil {
			t.Fatal(err)
		}
		set := false
		var ferr error
		rc.Control(func(p uintptr) {
			flags, err := unix.FcntlInt(p, unix.F_GETFL, 0)
			if err != nil {
				ferr = err
				return
			}
			set = flags&unix.O_NONBLOCK != 0
		})
		if ferr != nil {
			t.Fatal(ferr)
		}
		return set
	}
	if !nonblocking() {
		t.Fatal("the listener isn't in non-blocking mode to begin with")
	}
	if err := c.Give(f, Meta{Addr: ln.Addr().String(), PID: os.Getpid()}); err != nil {
		t.Fatal(err)
	}
	if !nonblocking() {
		t.Fatal("Give left the listener in blocking mode (os.File.Fd on the copy): its accept sleeps in the syscall past Close, and the handover never finishes (#4107)")
	}
}

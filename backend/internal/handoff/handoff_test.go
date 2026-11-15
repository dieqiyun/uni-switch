//go:build darwin || linux

package handoff

import (
	"bufio"
	"context"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// asAnother has this process take its own Offer, as the new uni-switch would.
func asAnother(t *testing.T) {
	t.Helper()
	old := self
	self = func() int { return -1 }
	t.Cleanup(func() { self = old })
}

// offerListener offers ln from a goroutine, as the uni-switch handing over
// does, and reports AwaitAck's answer.
func offerListener(t *testing.T, dir, exe string, ln *net.TCPListener) (*Offer, <-chan error) {
	t.Helper()
	o, err := Open(dir, exe)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(o.Close)
	acked := make(chan error, 1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		c, err := o.Accept(ctx)
		if err != nil {
			acked <- err
			return
		}
		defer c.Close()
		f, err := ln.File()
		if err != nil {
			acked <- err
			return
		}
		err = c.Give(f, Meta{Addr: ln.Addr().String(), Callbacks: "/cb", PID: os.Getpid()})
		f.Close()
		if err != nil {
			acked <- err
			return
		}
		acked <- c.AwaitAck(10 * time.Second)
	}()
	return o, acked
}

// The socket handed over is the one listened on: a client connecting
// after the old process let go of its copy reaches the new one, and its
// ack reaches the old.
func TestHandOverListener(t *testing.T) {
	asAnother(t)
	dir := t.TempDir()
	ln, err := net.ListenTCP("tcp", &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	_, acked := offerListener(t, dir, "/apps/uni-switch", ln)

	got, err := Take(dir, "/apps/uni-switch", Hello{PID: 4242, Version: "v2"}, 5*time.Second)
	if err != nil || got == nil {
		t.Fatalf("Take = %v, %v", got, err)
	}
	defer got.Listener.Close()
	if got.Meta.Addr != addr || got.Meta.Callbacks != "/cb" || got.Meta.PID != os.Getpid() {
		t.Fatalf("meta = %+v", got.Meta)
	}
	if err := got.Ack(); err != nil {
		t.Fatal(err)
	}
	if err := <-acked; err != nil {
		t.Fatalf("the old one didn't hear the ack: %v", err)
	}
	ln.Close() // the old one stops accepting

	served := make(chan string, 1)
	go func() {
		c, err := got.Listener.Accept()
		if err != nil {
			served <- err.Error()
			return
		}
		defer c.Close()
		line, _ := bufio.NewReader(c).ReadString('\n')
		served <- line
	}()
	c, err := net.DialTimeout("tcp", addr, 2*time.Second)
	if err != nil {
		t.Fatalf("the port was closed between them: %v", err)
	}
	c.Write([]byte("hi\n"))
	c.Close()
	select {
	case line := <-served:
		if line != "hi\n" {
			t.Fatalf("the new one read %q", line)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the new one never accepted")
	}
}

// A new uni-switch that drops the connection without an ack leaves the
// socket with the old one, which hears so at once.
func TestNoAckKeepsTheSocket(t *testing.T) {
	asAnother(t)
	dir := t.TempDir()
	ln, err := net.ListenTCP("tcp", &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	_, acked := offerListener(t, dir, "/apps/uni-switch", ln)
	got, err := Take(dir, "/apps/uni-switch", Hello{PID: 4242}, 5*time.Second)
	if err != nil || got == nil {
		t.Fatalf("Take = %v, %v", got, err)
	}
	got.Listener.Close()
	got.Close()
	select {
	case err := <-acked:
		if err == nil {
			t.Fatal("an ack was heard that was never sent")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the old one waited out its timeout for a uni-switch gone")
	}
}

// Only a fresh Offer, of a process running, for the same program and not
// this process's own, is taken.
func TestTakeOnlyAFreshOfferForThisProgram(t *testing.T) {
	dir := t.TempDir()
	write := func(p pointer) {
		b, _ := json.Marshal(p)
		if err := os.WriteFile(filepath.Join(dir, pointerName), b, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	take := func() *Taken {
		got, err := Take(dir, "/apps/uni-switch", Hello{PID: 1}, time.Second)
		if err != nil {
			return nil
		}
		return got
	}
	if take() != nil {
		t.Fatal("took an Offer with none made")
	}
	sock := filepath.Join(t.TempDir(), "s")
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	for name, p := range map[string]pointer{
		"its own":         {PID: os.Getpid(), Sock: sock, Exe: "/apps/uni-switch", At: time.Now()},
		"stale":           {PID: os.Getppid(), Sock: sock, Exe: "/apps/uni-switch", At: time.Now().Add(-time.Hour)},
		"another program": {PID: os.Getppid(), Sock: sock, Exe: "/dev/uni-switch-dev", At: time.Now()},
		"a process gone":  {PID: 1 << 30, Sock: sock, Exe: "/apps/uni-switch", At: time.Now()},
	} {
		write(p)
		if _, ok := readPointer(dir, "/apps/uni-switch"); ok {
			t.Errorf("%s: the Offer was taken", name)
		}
	}
	write(pointer{PID: os.Getppid(), Sock: sock, Exe: "/apps/uni-switch", At: time.Now()})
	if _, ok := readPointer(dir, "/apps/uni-switch"); !ok {
		t.Fatal("a fresh Offer of a running process wasn't taken")
	}
}

// Close takes the pointer away, and only while it is the Offer's own.
func TestCloseRemovesOnlyItsPointer(t *testing.T) {
	dir := t.TempDir()
	o, err := Open(dir, "/apps/uni-switch")
	if err != nil {
		t.Fatal(err)
	}
	if fi, err := os.Stat(o.Dir()); err != nil || fi.Mode().Perm() != 0o700 {
		t.Fatalf("the sockets' folder: %v %v", fi, err)
	}
	o.Close()
	if _, err := os.Stat(filepath.Join(dir, pointerName)); !os.IsNotExist(err) {
		t.Fatalf("the pointer stayed: %v", err)
	}
	o2, err := Open(dir, "/apps/uni-switch")
	if err != nil {
		t.Fatal(err)
	}
	// another uni-switch's offer since
	b, _ := json.Marshal(pointer{PID: os.Getppid(), Sock: "/x", Exe: "/apps/uni-switch", At: time.Now()})
	os.WriteFile(filepath.Join(dir, pointerName), b, 0o600)
	o2.Close()
	if _, err := os.Stat(filepath.Join(dir, pointerName)); err != nil {
		t.Fatal("Close removed another's pointer")
	}
}

// A temporary folder whose path leaves a unix socket no room (a TMPDIR set
// long: the sandbox run's bind failed with "invalid argument", and the
// restart cut its stream as before) still gets an Offer, its sockets in
// /tmp.
func TestOpenWithALongTemporaryFolder(t *testing.T) {
	long := filepath.Join(t.TempDir(), "a-temporary-folder-whose-path-is-longer-than-a-unix-socket-has-room-for-on-a-mac")
	if err := os.MkdirAll(long, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TMPDIR", long)
	o, err := Open(t.TempDir(), "/apps/uni-switch")
	if err != nil {
		t.Fatalf("no offer: %v", err)
	}
	defer o.Close()
	if fi, err := os.Stat(o.Dir()); err != nil || fi.Mode().Perm() != 0o700 {
		t.Fatalf("the sockets' folder %s: %v %v", o.Dir(), fi, err)
	}
	cb, err := net.Listen("unix", filepath.Join(o.Dir(), "cb"))
	if err != nil {
		t.Fatalf("no room for the callbacks' socket: %v", err)
	}
	cb.Close()
}

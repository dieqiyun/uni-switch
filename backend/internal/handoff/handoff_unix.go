//go:build darwin || linux

package handoff

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

// Offer is a socket waiting to be handed to the new uni-switch.
type Offer struct {
	dir     string // the folder of the unix sockets, only this user's
	pointer string
	ln      *net.UnixListener
}

// sockRoom is the longest path a unix socket here may have: sun_path has
// 104 bytes on a Mac and 108 on Linux, with room kept for the callbacks'
// "cb" beside "s".
const sockRoom = 100

// sockDir makes the folder, only this user's, the sockets go in: in the
// temporary folder, or in /tmp where that one's path leaves a socket no
// room (a TMPDIR set long; the bind fails with "invalid argument", and
// the restart went back to cutting requests short).
func sockDir() (string, error) {
	var tmp string
	var err error
	for _, root := range []string{"", "/tmp"} {
		if tmp, err = os.MkdirTemp(root, "uni-switch-ho-"); err != nil {
			continue
		}
		if len(filepath.Join(tmp, "cb")) <= sockRoom {
			break
		}
		os.RemoveAll(tmp)
		tmp, err = "", fmt.Errorf("no folder with room for a socket's path (%s)", os.TempDir())
	}
	if err != nil {
		return "", err
	}
	if err := os.Chmod(tmp, 0o700); err != nil {
		os.RemoveAll(tmp)
		return "", err
	}
	return tmp, nil
}

// Open offers the gateway's socket to the next uni-switch to run exe on the
// config folder dir.
func Open(dir, exe string) (*Offer, error) {
	tmp, err := sockDir()
	if err != nil {
		return nil, err
	}
	sock := filepath.Join(tmp, "s")
	ln, err := net.ListenUnix("unix", &net.UnixAddr{Name: sock, Net: "unix"})
	if err != nil {
		os.RemoveAll(tmp)
		return nil, err
	}
	o := &Offer{dir: tmp, pointer: filepath.Join(dir, pointerName), ln: ln}
	b, _ := json.Marshal(pointer{PID: os.Getpid(), Sock: sock, Exe: exe, At: time.Now()})
	part := o.pointer + ".tmp"
	if err := os.WriteFile(part, b, 0o600); err != nil {
		o.Close()
		return nil, err
	}
	if err := os.Rename(part, o.pointer); err != nil {
		os.Remove(part)
		o.Close()
		return nil, err
	}
	return o, nil
}

// Dir is a folder of the Offer's own, only this user's, for another unix
// socket beside it (Meta.Callbacks); it is removed with the Offer.
func (o *Offer) Dir() string { return o.dir }

// Close takes the Offer back: its pointer and sockets are removed.
func (o *Offer) Close() {
	if o == nil {
		return
	}
	// the pointer only while it is still this one's
	if b, err := os.ReadFile(o.pointer); err == nil {
		var p pointer
		if json.Unmarshal(b, &p) == nil && p.PID == os.Getpid() {
			os.Remove(o.pointer)
		}
	}
	o.ln.Close()
	os.RemoveAll(o.dir)
}

// Withdraw removes the pointer, so no later uni-switch takes the Offer, and
// stops taking connections; the folder stays for sockets still in use.
func (o *Offer) Withdraw() {
	if b, err := os.ReadFile(o.pointer); err == nil {
		var p pointer
		if json.Unmarshal(b, &p) == nil && p.PID == os.Getpid() {
			os.Remove(o.pointer)
		}
	}
	o.ln.Close()
}

// Conn is the new uni-switch, connected to an Offer.
type Conn struct {
	c     *net.UnixConn
	r     *bufio.Reader
	Hello Hello
}

// Accept waits for the new uni-switch to connect and say hello, until ctx
// ends.
func (o *Offer) Accept(ctx context.Context) (*Conn, error) {
	stop := context.AfterFunc(ctx, func() { o.ln.SetDeadline(time.Now()) })
	defer stop()
	for {
		c, err := o.ln.AcceptUnix()
		if err != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			return nil, err
		}
		if !ownUser(c) {
			c.Close()
			continue
		}
		c.SetReadDeadline(time.Now().Add(10 * time.Second))
		r := bufio.NewReader(c)
		line, err := r.ReadBytes('\n')
		var h Hello
		if err != nil || json.Unmarshal(line, &h) != nil || h.PID <= 0 {
			c.Close()
			continue
		}
		c.SetReadDeadline(time.Time{})
		return &Conn{c: c, r: r, Hello: h}, nil
	}
}

// Give sends the listening socket f, with m. The number for the send
// comes from SyscallConn, not Fd: f is a listener's copy sharing the
// listener's open file description (ListenerFile), and Fd puts such a
// description in blocking mode (a socket's File hands out expecting its
// Fd to be read) — the gateway's own accept would then sleep in the
// syscall once nothing connected, past Close, and the handover would
// never finish (#4107).
func (c *Conn) Give(f *os.File, m Meta) error {
	b, _ := json.Marshal(m)
	b = append(b, '\n')
	rc, err := f.SyscallConn()
	if err != nil {
		return err
	}
	var fd int
	if err := rc.Control(func(p uintptr) { fd = int(p) }); err != nil {
		return err
	}
	n, oobn, err := c.c.WriteMsgUnix(b, unix.UnixRights(fd), nil)
	if err != nil {
		return err
	}
	if n != len(b) || oobn == 0 {
		return fmt.Errorf("handing the socket over: short write")
	}
	return nil
}

// AwaitAck waits, timeout at most, for the new uni-switch to say its gateway
// serves on the socket given. An error is the new one gone or not ready
// in time: the socket is then still this one's to serve.
func (c *Conn) AwaitAck(timeout time.Duration) error {
	c.c.SetReadDeadline(time.Now().Add(timeout))
	line, err := c.r.ReadString('\n')
	if err != nil {
		return fmt.Errorf("the new uni-switch didn't say it serves: %w", err)
	}
	if line != ackLine {
		return fmt.Errorf("the new uni-switch said %q", strings.TrimSpace(line))
	}
	return nil
}

// Close ends the connection; the new uni-switch reads it as the one before
// gone.
func (c *Conn) Close() { c.c.Close() }

// Taken is the socket handed over, with its Meta.
type Taken struct {
	Listener net.Listener
	Meta     Meta
	c        *net.UnixConn
}

// Take takes the socket the uni-switch before this one offers, when it runs
// exe on dir: nil, nil when none does. It waits timeout at most.
func Take(dir, exe string, hello Hello, timeout time.Duration) (*Taken, error) {
	p, ok := readPointer(dir, exe)
	if !ok {
		return nil, nil
	}
	deadline := time.Now().Add(timeout)
	c, err := net.DialTimeout("unix", p.Sock, timeout)
	if err != nil {
		return nil, err
	}
	uc := c.(*net.UnixConn)
	fail := func(err error) (*Taken, error) {
		uc.Close()
		return nil, err
	}
	uc.SetDeadline(deadline)
	b, _ := json.Marshal(hello)
	if _, err := uc.Write(append(b, '\n')); err != nil {
		return fail(err)
	}
	buf := make([]byte, 4096)
	oob := make([]byte, unix.CmsgSpace(4))
	n, oobn, _, _, err := uc.ReadMsgUnix(buf, oob)
	if err != nil {
		return fail(err)
	}
	msgs, err := unix.ParseSocketControlMessage(oob[:oobn])
	if err != nil || len(msgs) == 0 {
		return fail(errors.New("no socket came with the handover"))
	}
	fds, err := unix.ParseUnixRights(&msgs[0])
	if err != nil || len(fds) == 0 {
		return fail(errors.New("no socket came with the handover"))
	}
	for _, fd := range fds[1:] {
		unix.Close(fd)
	}
	unix.CloseOnExec(fds[0])
	var m Meta
	if json.Unmarshal([]byte(strings.TrimSpace(string(buf[:n]))), &m) != nil {
		unix.Close(fds[0])
		return fail(errors.New("the handover's details didn't parse"))
	}
	f := os.NewFile(uintptr(fds[0]), "gateway")
	ln, err := net.FileListener(f) // a copy of its own; f is let go
	f.Close()
	if err != nil {
		return fail(err)
	}
	uc.SetDeadline(time.Time{})
	return &Taken{Listener: ln, Meta: m, c: uc}, nil
}

// Ack tells the uni-switch before that this one serves on the socket.
func (t *Taken) Ack() error {
	t.c.SetWriteDeadline(time.Now().Add(5 * time.Second))
	_, err := t.c.Write([]byte(ackLine))
	t.c.Close()
	return err
}

// Close drops the connection without an ack: the uni-switch before keeps
// serving.
func (t *Taken) Close() { t.c.Close() }

// ownUser is whether the process at the other end of c runs as this
// user. The folder the socket is in already keeps others out; this is a
// second look.
func ownUser(c *net.UnixConn) bool {
	raw, err := c.SyscallConn()
	if err != nil {
		return false
	}
	uid := -1
	raw.Control(func(fd uintptr) { uid = peerUID(int(fd)) })
	return uid == os.Getuid()
}

// alive is whether a process pid is running.
func alive(pid int) bool {
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}

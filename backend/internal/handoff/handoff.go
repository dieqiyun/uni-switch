// Package handoff hands the gateway's listening socket from the uni-switch
// restarting to update to the version it started, so the port is never
// closed between them: the new one serves on the same socket, and the old
// one stops accepting and finishes what it has.
//
// The uni-switch handing over opens an Offer: a unix socket in a folder only
// its user can enter, named in a pointer file in uni-switch's config folder.
// The new uni-switch Takes it: it connects, says hello, and is sent the
// listening socket (SCM_RIGHTS) with a Meta; once its gateway serves on
// it, it Acks, and the one before lets go. Only Unix has this (darwin and
// linux); elsewhere Open says ErrUnsupported (Take finds nothing), and a restart waits
// for the gateway to be idle as before.
package handoff

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"
)

// ErrUnsupported is Open's error where a socket can't be handed
// to another process (Windows).
var ErrUnsupported = errors.New("handing the gateway over isn't supported here")

// self is this process's pid; a test stands in for it, to take its own
// Offer.
var self = os.Getpid

// pointerName is the pointer file in uni-switch's config folder.
const pointerName = "handover.json"

// pointerFresh is how long an Offer's pointer is taken: one older is a
// restart that gave up (Offer.Close removes it; a crash may not).
const pointerFresh = 2 * time.Minute

// pointer is what the pointer file says.
type pointer struct {
	PID  int       `json:"pid"`
	Sock string    `json:"sock"`
	Exe  string    `json:"exe"` // the program the new uni-switch runs
	At   time.Time `json:"at"`
}

// Hello is what the new uni-switch says it is.
type Hello struct {
	PID     int    `json:"pid"`
	Version string `json:"version"`
}

// Meta comes with the socket handed over.
type Meta struct {
	// Addr is where the socket listens.
	Addr string `json:"addr"`
	// Callbacks is the unix socket where the uni-switch handing over answers
	// its own Claude Code runs' tool calls until it ends.
	Callbacks string `json:"callbacks,omitempty"`
	// PID is the uni-switch handing over.
	PID int `json:"pid"`
}

// ackLine is what Ack writes.
const ackLine = "serving\n"

// readPointer is the Offer named in dir for exe, if one is there, fresh,
// from a process still running and not this one.
func readPointer(dir, exe string) (pointer, bool) {
	b, err := os.ReadFile(filepath.Join(dir, pointerName))
	if err != nil {
		return pointer{}, false
	}
	var p pointer
	if json.Unmarshal(b, &p) != nil || p.Sock == "" || p.PID <= 0 || p.PID == self() {
		return pointer{}, false
	}
	if time.Since(p.At) > pointerFresh || p.At.After(time.Now().Add(time.Minute)) {
		return pointer{}, false
	}
	if !sameExe(p.Exe, exe) || !alive(p.PID) {
		return pointer{}, false
	}
	return p, true
}

// sameExe is whether two paths name one program, symlinks followed.
func sameExe(a, b string) bool {
	if a == "" || b == "" {
		return false
	}
	if r, err := filepath.EvalSymlinks(a); err == nil {
		a = r
	}
	if r, err := filepath.EvalSymlinks(b); err == nil {
		b = r
	}
	return filepath.Clean(a) == filepath.Clean(b)
}

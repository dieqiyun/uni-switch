// Package claudelock takes the mkdir locks Claude Code holds while it
// saves its files, taken as Claude Code takes them (2.1.316, its bundled
// proper-lockfile): a lock is a directory made with mkdir, held while it
// stands and let go by removing it. Its holder touches it every few
// seconds; one not touched for longer than the lock's stale time is left
// by a holder that died, and is taken over.
package claudelock

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

var (
	// Wait is how long a caller waits for Claude Code to let a lock go.
	// A refresh holds them for one token request; Claude Code itself
	// gives up after five tries a second or two apart.
	Wait = 20 * time.Second
	// poll is how often a held lock is looked at again.
	poll = 100 * time.Millisecond
	// touch is how often a lock uni-switch holds is touched, as Claude
	// Code's update (5s for the refresh locks, half the stale time for
	// ~/.claude.json's), so it never reads as left behind.
	touch = 2 * time.Second
)

const (
	// RefreshStale is how long a lock Claude Code refreshes its sign-in
	// under may stand untouched (.oauth_refresh.lock, ~/.claude.lock).
	RefreshStale = 60 * time.Second
	// ConfigStale is how long the lock Claude Code saves ~/.claude.json
	// under may stand untouched.
	ConfigStale = 10 * time.Second
)

// ErrHeld is a lock Claude Code held past Wait.
var ErrHeld = errors.New("Claude Code is saving its settings: try again in a moment")

// Config takes the lock Claude Code saves the config file path under: at
// the file's real path (proper-lockfile resolves it) plus ".lock". A
// file whose folder isn't there yet (Claude Code never run) has no lock
// to wait for.
func Config(path string) (release func(), err error) {
	if r, err := filepath.EvalSymlinks(path); err == nil {
		path = r
	}
	if fi, err := os.Stat(filepath.Dir(path)); err != nil || !fi.IsDir() {
		return func() {}, nil
	}
	return Take(path+".lock", ConfigStale)
}

// Take makes the lock directory path, waiting while another holds it and
// taking over one left stale. Held, it is touched until released.
func Take(path string, stale time.Duration) (release func(), err error) {
	deadline := time.Now().Add(Wait)
	for {
		err := os.Mkdir(path, 0o755)
		if err == nil {
			break
		}
		if !os.IsExist(err) {
			return nil, fmt.Errorf("take Claude Code's lock %s: %w", path, err)
		}
		if fi, serr := os.Stat(path); serr == nil && time.Since(fi.ModTime()) > stale {
			// its holder died without letting it go: Claude Code takes
			// such a lock over the same way
			if os.Remove(path) == nil {
				continue
			}
		}
		if time.Now().After(deadline) {
			return nil, ErrHeld
		}
		time.Sleep(poll)
	}
	stop, done := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(done)
		t := time.NewTicker(touch)
		defer t.Stop()
		for {
			select {
			case <-stop:
				return
			case <-t.C:
				now := time.Now()
				_ = os.Chtimes(path, now, now)
			}
		}
	}()
	return func() {
		close(stop)
		<-done
		_ = os.Remove(path)
	}, nil
}

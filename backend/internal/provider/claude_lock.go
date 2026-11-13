package provider

import (
	"errors"
	"os"
	"path/filepath"

	"github.com/dieqiyun/uni-switch/backend/internal/appdir"
	"github.com/dieqiyun/uni-switch/backend/internal/claudelock"
)

// Claude Code refreshes its sign-in under two of its own locks, in this
// order: <config dir>/.oauth_refresh.lock, then the older <config
// dir>.lock (~/.claude.lock), both stale after 60s. Under them it reads
// the sign-in again, posts the refresh token, and writes back the new
// pair. A switch that read the sign-in it was replacing while a refresh
// ran kept that account's refresh token as it was before the refresh:
// spent, and the account signed out of uni-switch on its next use (#4532).
// It saves ~/.claude.json under <that file>.lock, stale after 10s, while
// it may hold the refresh locks: so it is taken after them, as Claude
// Code does. The locks are mkdir locks, taken as Claude Code takes them
// (claudelock).

// errClaudeLocked is a lock Claude Code held past claudelock.Wait.
var errClaudeLocked = errors.New("Claude Code is refreshing its sign-in: try again in a moment")

// claudeRefreshLocks takes the locks Claude Code refreshes its sign-in
// under, for the config directory uni-switch reads the sign-in of. The
// release lets them go in the reverse order. A directory not there yet
// (Claude Code never run) has no refresh to wait for.
func claudeRefreshLocks() (release func(), err error) {
	dir := appdir.ClaudeDir()
	if fi, err := os.Stat(dir); err != nil || !fi.IsDir() {
		return func() {}, nil
	}
	first, err := claudelock.Take(filepath.Join(dir, ".oauth_refresh.lock"), claudelock.RefreshStale)
	if err != nil {
		return nil, claudeHeld(err)
	}
	resolved := dir
	if r, err := filepath.EvalSymlinks(dir); err == nil {
		resolved = r
	}
	legacy, err := claudelock.Take(resolved+".lock", claudelock.RefreshStale)
	if err != nil {
		first()
		return nil, claudeHeld(err)
	}
	return func() { legacy(); first() }, nil
}

// claudeConfigLock takes the lock Claude Code saves ~/.claude.json under.
func claudeConfigLock() (release func(), err error) {
	release, err = claudelock.Config(claudeProfilePath())
	return release, claudeHeld(err)
}

// claudeHeld says a lock Claude Code held past the wait as a switch's
// own: the one waiting on a refresh, not on a save.
func claudeHeld(err error) error {
	if errors.Is(err, claudelock.ErrHeld) {
		return errClaudeLocked
	}
	return err
}

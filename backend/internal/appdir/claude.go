package appdir

import (
	"os"
	"path/filepath"
)

// ClaudeDir is the folder Claude Code keeps its files in: settings.json,
// the credentials it keeps off a keychain, history, sessions, skills. It is
// $CLAUDE_CONFIG_DIR when that is set, as Claude Code reads it, else
// ~/.claude. Every place uni-switch reads or writes Claude Code's files goes
// through here: the Agents page's wiring once wrote ~/.claude/settings.json
// and looked for ~/.claude alone while the sessions, the library and the
// sign-in read $CLAUDE_CONFIG_DIR, so a Claude Code kept there wasn't
// listed and a model picked for it went to a file it never read (#1482).
// It is read through LookupEnv, so the value a login shell lends the
// desktop app (proc.UserPath) counts, and a relative one reads as unset.
func ClaudeDir() string {
	home, _ := os.UserHomeDir()
	return ClaudeDirIn(home)
}

// ClaudeDirIn is ClaudeDir with home as the user's home folder.
func ClaudeDirIn(home string) string {
	if d := Getenv("CLAUDE_CONFIG_DIR"); d != "" {
		return d
	}
	return filepath.Join(home, ".claude")
}

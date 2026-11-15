package sessions

import (
	"os"
	"path/filepath"
)

// Cindy (a desktop app that runs Claude Code, Codex and Pi) starts Pi with
// --session-dir <its data>/pi-agent-home/sessions: Pi's SDK files,
// <time>_<session id>.jsonl, all in one folder (Cindy 0.x's
// resolvePiAgentHome, the userData folder Electron gives it). They are read
// as Pi's, as agent "cindy", which is what the gateway counts their
// requests as (Cindy's key). Before, Pi's own folder was read and Cindy's
// never, so a user running Pi in Cindy saw no session of it at all
// (ldl123292 on X). Cindy's Claude Code writes to ~/.claude, read as
// Claude Code's already.

// CindyDirs are Cindy's data folders, Cindy and CindyGlobal (its global
// build), under the user's config folder (~/Library/Application Support,
// %APPDATA%, $XDG_CONFIG_HOME or ~/.config).
func CindyDirs() []string {
	d, err := os.UserConfigDir()
	if err != nil {
		return nil
	}
	return []string{filepath.Join(d, "Cindy"), filepath.Join(d, "CindyGlobal")}
}

func cindySessionDirs() []string {
	var out []string
	for _, d := range CindyDirs() {
		out = append(out, filepath.Join(d, "pi-agent-home", "sessions"))
	}
	return out
}

func cindyFiles() []file { return cindyFilesIn(cindySessionDirs()) }

// cindyFilesIn are the Pi sessions in Cindy's session folders.
func cindyFilesIn(dirs []string) []file {
	if !Reads("cindy") {
		return nil
	}
	var paths []string
	for _, d := range dirs {
		more, _ := filepath.Glob(filepath.Join(d, "*.jsonl"))
		paths = append(paths, more...)
	}
	return piNamed("cindy", paths)
}

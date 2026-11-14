package appdir

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// ZCodeHome is the home folder ZCode (Zhipu's desktop app) reads, given
// home, the user's home as Go reads it (os.UserHomeDir). ZCode, a Node
// app, takes $HOME before os.homedir() on every OS: its data folder
// (getDataBaseDir: process.env.HOME?.trim() || homedir()), its settings
// (resolveUserHomeDir: HOME, then USERPROFILE) and the skills and MCP
// servers it reads. On a Mac or Linux that is Go's home too. On Windows
// Go's is USERPROFILE and HOME is mostly unset, but one the user set (for
// Git Bash, MSYS2 or Emacs) is where ZCode keeps everything, and uni-switch
// wrote its provider under USERPROFILE, where ZCode never looked
// (Gearclickist on Discord: connected, no error, and no uni-switch in ZCode).
func ZCodeHome(home string) string { return ZCodeHomeOn(runtime.GOOS, home) }

// ZCodeHomeOn is ZCodeHome on goos, so a test can ask for Windows'. A
// HOME that isn't rooted is left out. Only the user's own home is moved:
// a folder that isn't USERPROFILE (a test's, a WSL distro's) is ZCode's
// as given.
func ZCodeHomeOn(goos, home string) string {
	if goos != "windows" {
		return home
	}
	h := strings.TrimSpace(os.Getenv("HOME"))
	if h == "" || !rooted(h) || !strings.EqualFold(filepath.Clean(home), filepath.Clean(os.Getenv("USERPROFILE"))) {
		return home
	}
	return h
}

// ZCodeDataIn is the folder ZCode (Zhipu's desktop app) keeps its .zcode
// data in, with home as the user's home folder: the home folder, unless the
// user moved it in ZCode's settings (数据存储路径, "data storage path").
// ZCode then copies its .zcode/v2 there and reads it from there from its
// next start, providers (provider_config.json, config.json) and sign-in
// (credentials.json) included; the choice stays in the home folder's
// .zcode/v2/setting.json as dataBaseDir, which ZCode's main process reads
// before anything else (readBootstrapDataBaseDirFromDisk). A value that
// isn't an absolute path is ignored, as Getenv ignores one.
//
// Skills, AGENTS.md and the CLI's config.json stay under the home folder
// whatever this says: ZCode reads those from HOME/USERPROFILE.
func ZCodeDataIn(home string) string {
	var s struct {
		DataBaseDir any `json:"dataBaseDir"`
	}
	b, err := os.ReadFile(filepath.Join(home, ".zcode", "v2", "setting.json"))
	if err != nil || json.Unmarshal(b, &s) != nil {
		return home
	}
	d, _ := s.DataBaseDir.(string)
	if d = strings.TrimSpace(d); d == "" || !rooted(d) {
		return home
	}
	return d
}

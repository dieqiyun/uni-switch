package appdir

import (
	"bufio"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// A user folder: Downloads, Desktop or Documents, as the system keeps it.
const (
	DownloadsFolder = "Downloads"
	DesktopFolder   = "Desktop"
	DocumentsFolder = "Documents"
)

// xdgNames are the user-dirs.dirs variables of each folder.
var xdgNames = map[string]string{
	DownloadsFolder: "XDG_DOWNLOAD_DIR",
	DesktopFolder:   "XDG_DESKTOP_DIR",
	DocumentsFolder: "XDG_DOCUMENTS_DIR",
}

// UserFolder is where the system keeps the user's folder kind (Downloads,
// Desktop or Documents), which the user or the system may have moved off
// home\kind: Windows' known folder (FOLDERID_Downloads, which OneDrive or
// the folder's Location tab moves to another drive), the XDG user folder
// on Linux and the BSDs ($XDG_CONFIG_HOME/user-dirs.dirs, written in the
// user's language, ~/下载 on a Chinese desktop), and ~/kind on a Mac,
// where it can't be moved. A failed lookup is home\kind, as before; ""
// with no usable home folder.
func UserFolder(kind string) string {
	h, err := Home()
	if err != nil {
		return ""
	}
	return userFolderOn(runtime.GOOS, h, kind)
}

// Downloads is UserFolder(DownloadsFolder).
func Downloads() string { return UserFolder(DownloadsFolder) }

func userFolderOn(goos, home, kind string) string {
	switch goos {
	case "darwin":
	case "windows":
		if p := windowsFolder(home, kind); p != "" {
			return p
		}
	default:
		if p := xdgUserDir(home, kind); p != "" {
			return p
		}
	}
	return filepath.Join(home, kind)
}

// knownFolder is Windows' path of the known folder kind and of the profile
// it belongs to ("" off Windows, or when it can't be read). Tests change it.
var knownFolder = osKnownFolder

// windowsFolder is the known folder kind when home is the profile Windows
// keeps it for. A home uni-switch was given of its own (USERPROFILE set to
// another folder, as a test's or a portable setup's) keeps its own
// home\kind: the known folder is another home's.
func windowsFolder(home, kind string) string {
	p, profile := knownFolder(kind)
	if p == "" || profile == "" || !strings.EqualFold(filepath.Clean(profile), filepath.Clean(home)) {
		return ""
	}
	return p
}

// xdgUserDir is kind's folder from user-dirs.dirs, as xdg-user-dir reads
// it: a quoted "$HOME/…" or absolute path. A file that can't be read, or
// has no usable line for it, is "".
func xdgUserDir(home, kind string) string {
	name := xdgNames[kind]
	if name == "" {
		return ""
	}
	cfg := Getenv("XDG_CONFIG_HOME")
	if cfg == "" {
		cfg = filepath.Join(home, ".config")
	}
	f, err := os.Open(filepath.Join(cfg, "user-dirs.dirs"))
	if err != nil {
		return ""
	}
	defer f.Close()
	got := ""
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		k, v, ok := strings.Cut(line, "=")
		if !ok || strings.TrimSpace(k) != name {
			continue
		}
		if p := xdgValue(home, strings.TrimSpace(v)); p != "" {
			got = p // the last one wins, as the shell sourcing it has it
		}
	}
	if sc.Err() != nil {
		return ""
	}
	return got
}

// xdgValue is a user-dirs.dirs value as a path: "$HOME/x" under home,
// "/x" as it is, with a backslash keeping the character after it. Anything
// else (unquoted, relative, another variable) is "", as the spec allows
// only these two.
func xdgValue(home, v string) string {
	if len(v) < 2 || v[0] != '"' || v[len(v)-1] != '"' {
		return ""
	}
	var b strings.Builder
	for i := 1; i < len(v)-1; i++ {
		if v[i] == '\\' && i+1 < len(v)-1 {
			i++
		}
		b.WriteByte(v[i])
	}
	s := b.String()
	switch {
	case s == "$HOME":
		return home
	case strings.HasPrefix(s, "$HOME/"):
		return filepath.Join(home, filepath.FromSlash(s[len("$HOME/"):]))
	case strings.HasPrefix(s, "/"):
		return filepath.Clean(s)
	}
	return ""
}

package update

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// The Windows app is linked as a GUI program, so a double click opens no
// console window; but a shell doesn't wait for a GUI program either. In
// PowerShell or cmd, `uni-switch plugin login` gave the prompt back at once,
// printed after it, and the shell and uni-switch both read the keys typed:
// a hidden password never reached uni-switch (TomXPRIME, #3737). A shell looks
// for `uni-switch` by PATHEXT, which lists .COM before .EXE, so uni-switch writes
// uni-switch.com beside the app: a small console program (./launcher) that
// the shell waits for, which runs the app with its console and exits with
// the app's code. Double clicks and the Start menu still open uni-switch.exe.
//
// It is written wherever a terminal runs uni-switch from: when the app's
// folder is on PATH, where Settings › Command line puts it, or a uni-switch.com
// or uni-switch.cmd is there already. On the Desktop or in Downloads only Add
// writes it, and a start only keeps one there up to date. The app writes it again as it starts
// when its bytes differ, so an update brings it along. They differ only
// when the launcher does: it is built without the release's commit in it
// (the Makefile's -buildvcs=false), or every update wrote a new uni-switch.com
// (lucky_eagle_38410 on Discord, TestLauncherSameAtEveryCommit).

// ComEnv is set by uni-switch.com for the app it runs: the standard handles it
// hands over are the terminal's console or a redirection, and its console
// is the one to borrow (cli's console_windows.go).
const ComEnv = "UNI_SWITCH_COM"

// comName is the companion's file name; it runs as `uni-switch`.
const comName = "uni-switch.com"

// comMagic ends uni-switch.com; the app it runs is named just before it,
// after a NUL (launcher's targetName).
const comMagic = "\x00uni-switch-com-target"

// comLauncher is the launcher program, built for this system and put in
// the Windows app by `make release-windows` (com_embed_windows_*.go); nil
// in any other build, which writes no uni-switch.com.
var comLauncher []byte

// comText is the uni-switch.com that runs the program exe in its folder.
func comText(launcher []byte, exe string) []byte {
	name := filepath.Base(strings.ReplaceAll(exe, `\`, "/"))
	out := make([]byte, 0, len(launcher)+1+len(name)+len(comMagic))
	out = append(out, launcher...)
	out = append(out, 0)
	out = append(out, name...)
	return append(out, comMagic...)
}

// comTarget is the program name in a uni-switch.com's last bytes, "" when it
// has none.
func comTarget(b []byte) string {
	if !bytes.HasSuffix(b, []byte(comMagic)) {
		return ""
	}
	b = b[:len(b)-len(comMagic)]
	return string(b[bytes.LastIndexByte(b, 0)+1:])
}

// comRuns says whether cmd is a uni-switch.com beside exe that runs it.
func comRuns(cmd, exe string) bool {
	if !strings.EqualFold(filepath.Base(cmd), comName) || !strings.EqualFold(filepath.Dir(cmd), filepath.Dir(exe)) {
		return false
	}
	f, err := os.Open(cmd)
	if err != nil {
		return false
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil || st.Size() < int64(len(comMagic)) {
		return false
	}
	n := min(st.Size(), 1024)
	b := make([]byte, n)
	if _, err := f.ReadAt(b, st.Size()-n); err != nil {
		return false
	}
	return strings.EqualFold(comTarget(b), filepath.Base(exe))
}

// comOwner says whether exe is the program a uni-switch.com in its folder
// should run: a uni-switch.exe there is what `uni-switch` ran before, so another
// copy beside it (uni-switch-windows-amd64 (1).exe) leaves it be.
func comOwner(exe string) bool {
	if strings.EqualFold(filepath.Base(exe), "uni-switch.exe") {
		return true
	}
	st, err := os.Stat(filepath.Join(filepath.Dir(exe), "uni-switch.exe"))
	return err != nil || st.IsDir()
}

// EnsureCompanion writes the uni-switch.com that runs exe beside it when a
// terminal runs uni-switch from that folder (on PATH, or a uni-switch.com or
// uni-switch.cmd there), or brings one there up to this version. Nothing in
// a build without the launcher, or when it is as it should be already.
func EnsureCompanion(exe string) error {
	// a download that opens the installed uni-switch (Settle) leaves the
	// command to that one, in its folder on PATH
	if comLauncher == nil || !comOwner(exe) || forwards(exe) {
		return nil
	}
	dir := filepath.Dir(exe)
	if downloadFolder(dir) {
		// the Desktop or Downloads, which the user sees: one there is
		// kept up to date, never put back once deleted (nianshou5 on
		// Discord: it came back on every start, the Desktop being on
		// PATH). Settings › Command line's Add still writes it.
		if !exists(filepath.Join(dir, comName)) {
			return nil
		}
	} else if !onPath(dir, os.Getenv("PATH")) && !exists(filepath.Join(dir, comName)) && !exists(filepath.Join(dir, shimName)) {
		return nil
	}
	_, err := writeCom(comLauncher, exe)
	return err
}

// writeCom writes the uni-switch.com that runs exe beside it, unless it is
// there already, and is its path. A uni-switch.com running now (a `uni-switch
// serve` in a terminal) can't be written over, but can be moved aside,
// as an update moves the running app; the next run clears it.
func writeCom(launcher []byte, exe string) (string, error) {
	p := filepath.Join(filepath.Dir(exe), comName)
	want := comText(launcher, exe)
	// the one moved aside last time, running then, is let go of now
	RemoveOld(p)
	if b, err := os.ReadFile(p); err == nil && bytes.Equal(b, want) {
		return p, nil
	}
	f, err := os.CreateTemp(filepath.Dir(p), ".uni-switch-com-*")
	if err != nil {
		return "", fmt.Errorf("writing %s, which runs %s as uni-switch: %w", p, filepath.Base(exe), err)
	}
	tmp := f.Name()
	_, err = f.Write(want)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		if err = os.Rename(tmp, p); err != nil && exists(p) {
			if aerr := os.Rename(p, oldName(p)); aerr == nil {
				err = os.Rename(tmp, p)
			}
		}
	}
	if err != nil {
		os.Remove(tmp)
		return "", fmt.Errorf("writing %s, which runs %s as uni-switch: %w", p, filepath.Base(exe), err)
	}
	return p, nil
}

// onPath is whether dir is one of path's (Windows', ;-separated) folders.
func onPath(dir, path string) bool {
	dir = strings.TrimRight(dir, `\/`)
	for _, d := range strings.Split(path, string(os.PathListSeparator)) {
		if d = strings.TrimRight(strings.TrimSpace(d), `\/`); d != "" && strings.EqualFold(d, dir) {
			return true
		}
	}
	return false
}

func exists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

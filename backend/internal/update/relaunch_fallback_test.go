//go:build !windows

package update

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// fakeBundle makes an app at dir/uni-switch.app whose Info.plist names exe as
// its program, a script that writes the arguments it was run with to
// dir/started: the new version, started.
func fakeBundle(t *testing.T, exe string) (bundle, started string) {
	t.Helper()
	dir := t.TempDir()
	bundle = filepath.Join(dir, "uni-switch.app")
	started = filepath.Join(dir, "started")
	macos := filepath.Join(bundle, "Contents", "MacOS")
	if err := os.MkdirAll(macos, 0o755); err != nil {
		t.Fatal(err)
	}
	plist := `<?xml version="1.0" encoding="UTF-8"?>
<plist version="1.0"><dict>
	<key>CFBundleExecutable</key>
	<string>` + exe + `</string>
	<key>CFBundleIdentifier</key><string>io.uni-switch.desktop</string>
</dict></plist>
`
	if err := os.WriteFile(filepath.Join(bundle, "Contents", "Info.plist"), []byte(plist), 0o644); err != nil {
		t.Fatal(err)
	}
	script := "#!/bin/sh\necho \"$@\" > '" + started + "'\n"
	if err := os.WriteFile(filepath.Join(macos, exe), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return bundle, started
}

// waitFor waits for path to have something in it, and says what.
func waitFor(t *testing.T, path, what string) string {
	t.Helper()
	for end := time.Now().Add(30 * time.Second); time.Now().Before(end); time.Sleep(50 * time.Millisecond) {
		if b, err := os.ReadFile(path); err == nil && len(b) > 0 {
			return strings.TrimSpace(string(b))
		}
	}
	t.Fatalf("after 30s, %s never happened", what)
	return ""
}

var shortWaits = []string{"0.1", "0.1", "0.1"}

// The report after fb23bca5c (molvqingtai): with the screen locked, uni-switch
// updated itself and still never came back, though open was tried for
// 13s. Every open is refused here, as LaunchServices refused them; once
// the tries are spent the bundle's own program is run, by its path, with
// tray when no window was up, and the window's gui when one was.
func TestRelaunchStartsTheBundleWhenOpenNeverWorks(t *testing.T) {
	for _, c := range []struct {
		window bool
		args   string
	}{{false, "tray"}, {true, "gui"}} {
		dir := fakeOpen(t, 1000)
		bundle, started := fakeBundle(t, "uni-switch")
		log := filepath.Join(t.TempDir(), relaunchLogName)
		if err := runAfterExit(gonePid(t), relaunchScript(bundle, c.window, false, log, shortWaits)); err != nil {
			t.Fatal(err)
		}
		if got := waitFor(t, started, "the new version starting"); got != c.args {
			t.Errorf("window %v: started with %q, want %q", c.window, got, c.args)
		}
		if n := tries(dir); n != len(shortWaits)+1 {
			t.Errorf("window %v: open tried %d times before the fallback, want %d", c.window, n, len(shortWaits)+1)
		}
		if _, err := os.Stat(filepath.Join(dir, "opened")); err == nil {
			t.Errorf("window %v: the fake open says it opened", c.window)
		}
	}
}

// runScript runs the relaunch's shell to its end, as runAfterExit's shell
// would once uni-switch is gone, and says how it exited.
func runScript(t *testing.T, script string) error {
	t.Helper()
	c := exec.Command("/bin/sh", "-c", script)
	if err := c.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- c.Wait() }()
	select {
	case err := <-done:
		return err
	case <-time.After(30 * time.Second):
		c.Process.Kill()
		<-done
		t.Fatal("the relaunch shell was still running after 30s")
	}
	return nil
}

// Each try is written down, open's exit status and what it said with it,
// and the start it fell back to: the next report says what happened.
func TestRelaunchLogsEachTry(t *testing.T) {
	fakeOpen(t, 1000)
	bundle, started := fakeBundle(t, "uni-switch")
	log := filepath.Join(t.TempDir(), relaunchLogName)
	if err := runScript(t, relaunchScript(bundle, false, false, log, shortWaits)); err != nil {
		t.Fatalf("the shell: %v", err)
	}
	waitFor(t, started, "the new version starting")
	b, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	got := string(b)
	for _, want := range []string{
		"opening " + bundle,
		"open try 1: exit 1 _LSOpenURLsWithCompletionHandler() failed for the application " + bundle + " with error -600.",
		"open try 4: exit 1 ",
		"open gave up after 4 tries; starting '" + filepath.Join(bundle, "Contents", "MacOS", "uni-switch") + "' 'tray'",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("the log has no %q:\n%s", want, got)
		}
	}
}

// An open that works is the whole relaunch: the program isn't run beside
// it as well, and the log says the open went through.
func TestRelaunchOpenedStartsNothingElse(t *testing.T) {
	dir := fakeOpen(t, 1)
	bundle, started := fakeBundle(t, "uni-switch")
	log := filepath.Join(t.TempDir(), relaunchLogName)
	if err := runScript(t, relaunchScript(bundle, false, false, log, shortWaits)); err != nil {
		t.Fatalf("opened on the second try, and the shell said %v", err)
	}
	if _, err := os.Stat(started); err == nil {
		t.Error("open worked and the bundle's program was run as well")
	}
	if n := tries(dir); n != 2 {
		t.Errorf("open tried %d times, want 2", n)
	}
	if b, _ := os.ReadFile(log); !strings.Contains(string(b), "open try 2: exit 0") {
		t.Errorf("the log doesn't say the second open worked:\n%s", b)
	}
}

// A bundle with no program where it should be is written down, not a
// shell that says nothing.
func TestRelaunchNoProgramIsLogged(t *testing.T) {
	fakeOpen(t, 1000)
	bundle, _ := fakeBundle(t, "uni-switch")
	exe := filepath.Join(bundle, "Contents", "MacOS", "uni-switch")
	os.Remove(exe)
	log := filepath.Join(t.TempDir(), relaunchLogName)
	if err := runScript(t, relaunchScript(bundle, false, false, log, shortWaits)); err == nil {
		t.Error("nothing started, and the shell said it did")
	}
	if b, _ := os.ReadFile(log); !strings.Contains(string(b), "no executable at "+exe) {
		t.Errorf("the log doesn't name the missing program:\n%s", b)
	}
}

// The program is the one the bundle's Info.plist names, even where it
// isn't uni-switch, and a quote in the bundle's path stays in it.
func TestRelaunchRunsTheProgramInfoPlistNames(t *testing.T) {
	fakeOpen(t, 1000)
	bundle, started := fakeBundle(t, "uni-switch dev")
	moved := filepath.Join(filepath.Dir(bundle), "it's uni-switch.app")
	if err := os.Rename(bundle, moved); err != nil {
		t.Fatal(err)
	}
	log := filepath.Join(t.TempDir(), relaunchLogName)
	if err := runScript(t, relaunchScript(moved, true, false, log, shortWaits)); err != nil {
		t.Fatalf("the shell: %v", err)
	}
	if got := waitFor(t, started, "the program Info.plist names starting"); got != "gui" {
		t.Errorf("started with %q, want gui", got)
	}
}

// The log is in uni-switch's own folder, where the rest of its state is, and
// is kept small: past 64KB the last one is moved aside, not added to.
func TestRelaunchLogInUniSwitchsFolder(t *testing.T) {
	cfg := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", cfg)
	want := filepath.Join(cfg, "uni-switch", relaunchLogName)
	if got := relaunchLog(); got != want {
		t.Fatalf("log at %q, want %q", got, want)
	}
	if err := os.WriteFile(want, make([]byte, 65<<10), 0o600); err != nil {
		t.Fatal(err)
	}
	relaunchLog()
	if _, err := os.Stat(want); err == nil {
		t.Error("a log past 64KB is still there to be added to")
	}
	if fi, err := os.Stat(want + ".old"); err != nil || fi.Size() != 65<<10 {
		t.Errorf("the last log wasn't kept as .old: %v", err)
	}
}

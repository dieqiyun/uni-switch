package shortcut

import (
	"os"
	"path/filepath"
	"testing"
)

// fakeSystem is a Start menu in a temp folder: writeLink makes the file and
// counts, register records the App Paths entry.
type fakeSystem struct {
	lnk    string
	writes int
	paths  []string // App Paths entries written
	before bool     // an older uni-switch registered itself
}

func fake(t *testing.T) (*fakeSystem, string) {
	t.Helper()
	dir := t.TempDir()
	f := &fakeSystem{lnk: filepath.Join(dir, "Start Menu", "Programs", Name+".lnk")}
	os.MkdirAll(filepath.Dir(f.lnk), 0o755)
	oldMenu, oldWrite, oldReg, oldRegd := startMenu, writeLink, register, registered
	t.Cleanup(func() { startMenu, writeLink, register, registered = oldMenu, oldWrite, oldReg, oldRegd })
	startMenu = func() (string, error) { return f.lnk, nil }
	writeLink = func(lnk, exe string) (bool, error) {
		f.writes++
		return true, os.WriteFile(lnk, []byte(exe), 0o644)
	}
	register = func(exe string) { f.paths = append(f.paths, exe) }
	registered = func() bool { return f.before }
	return f, filepath.Join(dir, "uni-switch", StateName)
}

func exists(p string) bool { _, err := os.Stat(p); return err == nil }

// #508: the shortcut is made on the first start, kept while it is there,
// and not made again once the user deleted it.
func TestEnsureOnce(t *testing.T) {
	f, state := fake(t)
	exe := `C:\Tools\uni-switch.exe`

	if err := ensure(exe, "", state); err != nil {
		t.Fatal(err)
	}
	if f.writes != 1 || !exists(f.lnk) || !exists(state) {
		t.Fatalf("first start: %d writes, shortcut %v, state %v", f.writes, exists(f.lnk), exists(state))
	}
	// still there: kept pointed at this uni-switch (write leaves a good one be)
	if err := ensure(exe, "", state); err != nil {
		t.Fatal(err)
	}
	if f.writes != 2 {
		t.Fatalf("second start with the shortcut there: %d writes, want 2", f.writes)
	}
	// the user deletes it
	os.Remove(f.lnk)
	for i := 0; i < 3; i++ {
		if err := ensure(exe, "", state); err != nil {
			t.Fatal(err)
		}
	}
	if exists(f.lnk) || f.writes != 2 {
		t.Fatalf("deleted shortcut came back: shortcut %v, %d writes", exists(f.lnk), f.writes)
	}
	if len(f.paths) != 5 {
		t.Errorf("App Paths written %d times, want at each installed start (5)", len(f.paths))
	}
}

// A user who deleted the shortcut under a uni-switch from before the state
// file (it registered App Paths at every start) doesn't get it back on
// updating; one who kept it has it kept.
func TestEnsureAfterOlderUniSwitch(t *testing.T) {
	f, state := fake(t)
	f.before = true
	if err := ensure(`C:\Tools\uni-switch.exe`, "", state); err != nil {
		t.Fatal(err)
	}
	if exists(f.lnk) || f.writes != 0 || !exists(state) {
		t.Fatalf("deleted before the update: shortcut %v, %d writes, state %v", exists(f.lnk), f.writes, exists(state))
	}

	f, state = fake(t)
	f.before = true
	os.WriteFile(f.lnk, []byte("old"), 0o644)
	if err := ensure(`C:\Tools\uni-switch.exe`, "", state); err != nil {
		t.Fatal(err)
	}
	if f.writes != 1 || !exists(state) {
		t.Fatalf("kept before the update: %d writes, state %v", f.writes, exists(state))
	}
}

// #508: a portable uni-switch makes no shortcut, registers nothing and keeps
// no state.
func TestEnsurePortable(t *testing.T) {
	f, state := fake(t)
	for i := 0; i < 2; i++ {
		if err := ensure(`E:\uni-switch\uni-switch.exe`, `E:\uni-switch\data`, state); err != nil {
			t.Fatal(err)
		}
	}
	if f.writes != 0 || exists(f.lnk) || len(f.paths) != 0 || exists(state) {
		t.Fatalf("portable: %d writes, shortcut %v, App Paths %v, state %v", f.writes, exists(f.lnk), f.paths, exists(state))
	}
}

// uni-switch uninstall takes the Start menu shortcut and the App Paths entry
// out, for the exe in place of one an update moved aside.
func TestRemove(t *testing.T) {
	dir := t.TempDir()
	lnk := filepath.Join(dir, "uni-switch.lnk")
	if err := os.WriteFile(lnk, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	oldMenu, oldUn := startMenu, unregister
	t.Cleanup(func() { startMenu, unregister = oldMenu, oldUn })
	startMenu = func() (string, error) { return lnk, nil }
	var asked string
	unregister = func(exe string) error { asked = exe; return nil }
	if err := Remove(`C:\Tools\uni-switch.exe.old`); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(lnk); !os.IsNotExist(err) {
		t.Error("the shortcut is still there")
	}
	if asked != `C:\Tools\uni-switch.exe` {
		t.Errorf("App Paths asked for %q", asked)
	}
	// none there, or no Start menu: nothing to say
	if err := Remove(`C:\Tools\uni-switch.exe`); err != nil {
		t.Fatal(err)
	}
	startMenu = func() (string, error) { return "", errNoStartMenu }
	if err := Remove("/usr/local/bin/uni-switch"); err != nil {
		t.Fatal(err)
	}
}

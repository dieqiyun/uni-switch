package update

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The other install tests put text in for the new version, which Windows
// won't start: they are about moving files, so the try is left out.
func init() { canStart = func(string) error { return nil } }

// #894: a download Windows won't start isn't put in; the running exe
// stays where it is and the download goes.
func TestInstallBinaryRefusedStaysOut(t *testing.T) {
	was := canStart
	canStart = func(string) error { return &os.PathError{Op: "fork/exec", Err: errIntegrityPolicy} }
	t.Cleanup(func() { canStart = was })
	exe, staged := rollbackFiles(t)
	old, err := InstallBinaryAside(staged, exe)
	var be *BlockedError
	if !errors.As(err, &be) || !be.Policy() || old != "" {
		t.Fatalf("InstallBinaryAside = %q, %v", old, err)
	}
	wantInstallFile(t, exe, "old version")
	for _, p := range []string{staged, exe + ".old"} {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Errorf("%s: %v", filepath.Base(p), err)
		}
	}
}

// trialStart starts a real program, under the .new name a download has,
// without running it, and fails on what isn't one.
func TestTrialStart(t *testing.T) {
	ping := filepath.Join(os.Getenv("SystemRoot"), "System32", "PING.EXE")
	b, err := os.ReadFile(ping)
	if err != nil {
		t.Skip("no PING.EXE:", err)
	}
	dir := t.TempDir()
	good := filepath.Join(dir, "uni-switch.exe.new")
	os.WriteFile(good, b, 0o755)
	if err := trialStart(good); err != nil {
		t.Fatalf("a program: %v", err)
	}
	bad := filepath.Join(dir, "text.exe.new")
	os.WriteFile(bad, []byte("not a program"), 0o755)
	if err := trialStart(bad); err == nil {
		t.Fatal("text started")
	}
	// ended, not left running: its file can go, once whatever checks a
	// program as it starts (an antivirus) has let go, as moveIn waits (#2272)
	for i := 0; ; i++ {
		err := os.Remove(good)
		if err == nil {
			break
		}
		if i == len(asideWaits) {
			t.Fatalf("still held: %v", err)
		}
		time.Sleep(asideWaits[i])
	}
}

// #2272: a download something still holds a moment after the trial (an
// antivirus that checks a program as it starts) goes in once it is let
// go, rather than failing the install.
func TestInstallBinaryWaitsForHeldDownload(t *testing.T) {
	exe, staged := rollbackFiles(t)
	release := holdForRollbackTest(t, staged)
	if err := os.Rename(staged, staged+".probe"); !errors.Is(err, errSharingViolation) {
		t.Fatalf("fixture: renaming the held download = %v, want sharing violation", err)
	}
	go func() { time.Sleep(150 * time.Millisecond); release() }()
	old, err := InstallBinaryAside(staged, exe)
	if err != nil {
		t.Fatal("install while the download is held a moment:", err)
	}
	wantInstallFile(t, exe, "new version")
	wantInstallFile(t, old, "old version")
	if _, err := os.Stat(staged); !os.IsNotExist(err) {
		t.Errorf("download left: %v", err)
	}
}

// #2272: the install as an update runs it, the real trial start first and
// the move straight after, on a real program under the .new name a
// download has, its bytes new to this computer.
func TestInstallBinaryRightAfterTheTrial(t *testing.T) {
	ping := filepath.Join(os.Getenv("SystemRoot"), "System32", "PING.EXE")
	b, err := os.ReadFile(ping)
	if err != nil {
		t.Skip("no PING.EXE:", err)
	}
	was := canStart
	canStart = trialStart
	t.Cleanup(func() { canStart = was })
	exe, staged := rollbackFiles(t)
	prog := append(b, []byte(t.Name()+time.Now().String())...) // a file no scanner has seen
	if err := os.WriteFile(staged, prog, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := InstallBinaryAside(staged, exe); err != nil {
		t.Fatal("install after the trial:", err)
	}
	if got, _ := os.ReadFile(exe); string(got) != string(prog) {
		t.Fatal("the new version is not in place")
	}
}

// The restart for real: a new version that can't be started, one that
// quits at once and one that runs on without saying it is up are each
// ended, their file let go of, and the running exe put back.
func TestStartCheckedPutsBackForReal(t *testing.T) {
	ping := filepath.Join(os.Getenv("SystemRoot"), "System32", "PING.EXE")
	b, err := os.ReadFile(ping)
	if err != nil {
		t.Skip("no PING.EXE:", err)
	}
	was := startWait
	startWait = 2 * time.Second
	t.Cleanup(func() { startWait = was })
	for _, c := range []struct {
		name string
		prog []byte
		args []string
	}{
		{"not a program", []byte("not a program"), nil},
		{"quits at once", b, []string{"-n", "1", "127.0.0.1"}},
		{"never says it is up", b, []string{"-n", "60", "127.0.0.1"}},
	} {
		t.Run(c.name, func(t *testing.T) {
			dir := t.TempDir()
			exe, old := filepath.Join(dir, "uni-switch.exe"), filepath.Join(dir, "uni-switch.exe.old")
			os.WriteFile(exe, c.prog, 0o755)
			os.WriteFile(old, []byte("running version"), 0o755)
			err := startChecked(exe, old, c.args, os.Environ())
			var be *BlockedError
			if !errors.As(err, &be) {
				t.Fatalf("err = %v", err)
			}
			t.Log(err)
			wantInstallFile(t, exe, "running version")
			if _, err := os.Stat(old); !os.IsNotExist(err) {
				t.Errorf("old left: %v", err)
			}
		})
	}
}

// 白菜 on Discord: a download an antivirus holds longer than the short
// pauses after the trial start goes in once it is let go, and a restart
// that failed anyway, tried again, doesn't start the same download again
// (which had the antivirus hold it again) but does a new one.
func TestInstallBinaryWaitsOutALongHold(t *testing.T) {
	waits, held := asideWaits, heldFor
	asideWaits, heldFor = []time.Duration{time.Millisecond, 20 * time.Millisecond}, 5*time.Second
	t.Cleanup(func() { asideWaits, heldFor = waits, held })
	exe, staged := rollbackFiles(t)
	release := holdForRollbackTest(t, staged)
	go func() { time.Sleep(600 * time.Millisecond); release() }()
	if _, err := InstallBinaryAside(staged, exe); err != nil {
		t.Fatal("install while the download is held past the short pauses:", err)
	}
	wantInstallFile(t, exe, "new version")
}

func TestInstallBinaryTrialsADownloadOnce(t *testing.T) {
	waits, held, was := asideWaits, heldFor, canStart
	asideWaits, heldFor = []time.Duration{time.Millisecond}, 0
	trials := 0
	canStart = func(string) error { trials++; return nil }
	t.Cleanup(func() { asideWaits, heldFor, canStart = waits, held, was })
	exe, staged := rollbackFiles(t)
	release := holdForRollbackTest(t, staged)
	_, err := InstallBinaryAside(staged, exe)
	if err == nil || !strings.Contains(err.Error(), "another program still has the download open") {
		t.Fatalf("held for good: %v", err)
	}
	wantInstallFile(t, exe, "old version")
	release()
	if _, err := InstallBinaryAside(staged, exe); err != nil {
		t.Fatal("tried again:", err)
	}
	wantInstallFile(t, exe, "new version")
	if trials != 1 {
		t.Fatalf("%d trial starts of one download, want 1", trials)
	}

	// a new download is tried
	os.WriteFile(staged, []byte("newer version"), 0o755)
	if _, err := InstallBinaryAside(staged, exe); err != nil {
		t.Fatal(err)
	}
	if trials != 2 {
		t.Fatalf("%d trial starts after a new download, want 2", trials)
	}
}

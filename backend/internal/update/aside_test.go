package update

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// fakeRename has renameFile fail with err the first n times, then rename.
func fakeRename(t *testing.T, n int, err error) *int {
	calls := 0
	waits, rename, held := asideWaits, renameFile, heldFor
	asideWaits = []time.Duration{time.Millisecond, time.Millisecond, time.Millisecond}
	heldFor = 0
	renameFile = func(from, to string) error {
		calls++
		if calls <= n {
			return &os.LinkError{Op: "rename", Old: from, New: to, Err: err}
		}
		return os.Rename(from, to)
	}
	t.Cleanup(func() { asideWaits, renameFile, heldFor = waits, rename, held })
	return &calls
}

// Something holding the running exe open for a moment (an antivirus scan,
// OneDrive) doesn't fail the update: moving it aside is tried again.
func TestMoveAsideTriesAgain(t *testing.T) {
	exe := filepath.Join(t.TempDir(), "uni-switch-windows-amd64.exe")
	os.WriteFile(exe, []byte("v1"), 0o755)
	calls := fakeRename(t, 2, errSharingViolation)
	if _, err := moveAside(exe); err != nil {
		t.Fatal(err)
	}
	if *calls != 3 {
		t.Fatalf("%d tries, want 3", *calls)
	}
	if b, _ := os.ReadFile(exe + ".old"); string(b) != "v1" {
		t.Fatal("not moved to .old")
	}
}

// One that keeps it says why and what to do, without the paths the
// version row has no room for.
func TestMoveAsideSaysWhy(t *testing.T) {
	exe := filepath.Join(t.TempDir(), "uni-switch-windows-amd64.exe")
	os.WriteFile(exe, []byte("v1"), 0o755)
	calls := fakeRename(t, 100, errSharingViolation)
	_, err := moveAside(exe)
	if err == nil {
		t.Fatal("moved")
	}
	if *calls != 4 {
		t.Fatalf("%d tries, want 4", *calls)
	}
	msg := err.Error()
	for _, want := range []string{"couldn't move uni-switch-windows-amd64.exe aside", strings.TrimRight(errSharingViolation.Error(), ". 。"), "another program has it open", "download the new version"} {
		if !strings.Contains(msg, want) {
			t.Errorf("%q lacks %q", msg, want)
		}
	}
	if strings.Contains(msg, exe) {
		t.Errorf("%q has the path", msg)
	}

	// a folder uni-switch may not change says so
	fakeRename(t, 100, os.ErrPermission)
	_, err = moveAside(exe)
	if msg := err.Error(); !strings.Contains(msg, "doesn't let uni-switch change files in "+filepath.Dir(exe)) {
		t.Errorf("%q", msg)
	}

	// gone is not tried again
	calls = fakeRename(t, 100, os.ErrNotExist)
	moveAside(exe)
	if *calls != 1 {
		t.Fatalf("%d tries of a missing exe", *calls)
	}
}

// An antivirus checking a new program can hold a file longer than the
// short pauses (白菜 on Discord: every restart failed the same way): a
// file held open is waited for on, up to heldFor, rather than given up on
// after three seconds.
func TestMoveAsideWaitsOutALongHold(t *testing.T) {
	exe := filepath.Join(t.TempDir(), "uni-switch-windows-amd64.exe")
	os.WriteFile(exe, []byte("v1"), 0o755)
	calls := fakeRename(t, 10, errSharingViolation)
	heldFor = 5 * time.Second
	if _, err := moveAside(exe); err != nil {
		t.Fatal(err)
	}
	if *calls != 11 {
		t.Fatalf("%d tries, want 11", *calls)
	}

	// what isn't a file held open isn't waited for past the short pauses
	os.WriteFile(exe, []byte("v2"), 0o755)
	calls = fakeRename(t, 100, os.ErrPermission)
	heldFor = 5 * time.Second
	if _, err := moveAside(exe); err == nil {
		t.Fatal("moved")
	}
	if *calls != 4 {
		t.Fatalf("%d tries of a refused move, want 4", *calls)
	}
}

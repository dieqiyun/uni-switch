package update

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

func rollbackFiles(t *testing.T) (string, string) {
	t.Helper()
	exe := filepath.Join(t.TempDir(), "uni-switch.exe")
	staged := exe + ".new"
	for path, content := range map[string]string{exe: "old version", staged: "new version"} {
		if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return exe, staged
}

// shortWaits has moveIn and moveAside give up on a file held for good
// at once.
func shortWaits(t *testing.T) {
	was, held := asideWaits, heldFor
	asideWaits, heldFor = []time.Duration{time.Millisecond}, 0
	t.Cleanup(func() { asideWaits, heldFor = was, held })
}

func holdForRollbackTest(t *testing.T, path string) func() {
	t.Helper()
	h := holdOpen(t, path)
	close := sync.OnceFunc(func() {
		if err := syscall.CloseHandle(h); err != nil {
			t.Error("close held file:", err)
		}
	})
	t.Cleanup(close)
	return close
}

func wantInstallFile(t *testing.T, path, content string) {
	t.Helper()
	if got, err := os.ReadFile(path); err != nil || string(got) != content {
		t.Errorf("%s = %q (%v), want %q", filepath.Base(path), got, err, content)
	}
}

// A scanner holding the download must not leave the installed path missing.
// An earlier update's held .old means the actual backup is .old-2 instead.
func TestInstallBinaryRestoresAfterStagedRenameFails(t *testing.T) {
	for _, heldOld := range []bool{false, true} {
		name := "first update"
		if heldOld {
			name = "earlier old still held"
		}
		t.Run(name, func(t *testing.T) {
			shortWaits(t)
			exe, staged := rollbackFiles(t)
			aside := exe + ".old"
			if heldOld {
				if err := os.WriteFile(aside, []byte("earlier version"), 0o755); err != nil {
					t.Fatal(err)
				}
				holdForRollbackTest(t, aside)
				if err := os.Remove(aside); !errors.Is(err, errSharingViolation) {
					t.Fatalf("fixture: removing held .old = %v, want sharing violation", err)
				}
				aside = exe + ".old-2"
			}
			closeStaged := holdForRollbackTest(t, staged)
			if err := os.Rename(staged, staged+".probe"); !errors.Is(err, errSharingViolation) {
				t.Fatalf("fixture: renaming held download = %v, want sharing violation", err)
			}
			err := InstallBinary(staged, exe)
			var renameErr *os.LinkError
			if !errors.Is(err, errSharingViolation) || !errors.As(err, &renameErr) || renameErr.Old != staged || renameErr.New != exe {
				t.Fatalf("InstallBinary = %v, want held staged-to-exe rename failure", err)
			}
			wantInstallFile(t, exe, "old version")
			wantInstallFile(t, staged, "new version")
			if _, err := os.Stat(aside); !os.IsNotExist(err) {
				t.Errorf("restored backup remains at %s: %v", filepath.Base(aside), err)
			}
			if heldOld {
				wantInstallFile(t, exe+".old", "earlier version")
			}

			closeStaged()
			if err := InstallBinary(staged, exe); err != nil {
				t.Fatal("retry after the scanner closes:", err)
			}
			wantInstallFile(t, exe, "new version")
			wantInstallFile(t, aside, "old version")
			if _, err := os.Stat(staged); !os.IsNotExist(err) {
				t.Errorf("download remains after successful retry: %v", err)
			}
			if heldOld {
				wantInstallFile(t, exe+".old", "earlier version")
			}
		})
	}
}

// If a scanner takes the old file just after it moves aside, restoration
// can fail too. Report both failures and keep both versions for recovery.
func TestInstallBinaryReportsFailedRestore(t *testing.T) {
	shortWaits(t)
	exe, staged := rollbackFiles(t)
	holdForRollbackTest(t, staged)
	rename := renameFile
	t.Cleanup(func() { renameFile = rename })
	var aside string
	renameFile = func(from, to string) error {
		err := os.Rename(from, to)
		if err == nil && from == exe {
			aside = to
			holdForRollbackTest(t, aside)
		}
		return err
	}
	err := InstallBinary(staged, exe)
	if err == nil || aside == "" {
		t.Fatalf("fixture did not reach the second rename: %v, backup %q", err, aside)
	}
	wantInstallFile(t, aside, "old version")
	wantInstallFile(t, staged, "new version")
	if !strings.Contains(err.Error(), "restore") {
		t.Errorf("failure does not explain that restoring the executable failed: %v", err)
	}
	var both interface{ Unwrap() []error }
	if !errors.As(err, &both) {
		t.Fatalf("failure does not retain both causes: %v", err)
	}
	var installFailed, restoreFailed bool
	for _, cause := range both.Unwrap() {
		var linkErr *os.LinkError
		if errors.As(cause, &linkErr) && errors.Is(cause, errSharingViolation) {
			installFailed = installFailed || linkErr.Old == staged && linkErr.New == exe
			restoreFailed = restoreFailed || linkErr.Old == aside && linkErr.New == exe
		}
	}
	if !installFailed || !restoreFailed {
		t.Errorf("missing typed install/restore failure: %v", err)
	}
}

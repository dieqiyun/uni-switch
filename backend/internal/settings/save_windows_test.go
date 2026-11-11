//go:build windows

package settings

import (
	"bytes"
	"errors"
	"os"
	"testing"

	"golang.org/x/sys/windows"
)

func TestSaveKeepsReadBlockedSettings(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	if err := os.MkdirAll(Dir(), 0o755); err != nil {
		t.Fatal(err)
	}
	original := []byte(`{"theme":"dark","githubToken":"SYNTHETIC_PRIVATE_TOKEN"}`)
	if err := os.WriteFile(Path(), original, 0o600); err != nil {
		t.Fatal(err)
	}
	name, err := windows.UTF16PtrFromString(Path())
	if err != nil {
		t.Fatal(err)
	}
	// Writes remain possible, but a reader cannot open this existing file.
	h, err := windows.CreateFile(name, windows.GENERIC_WRITE,
		windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE, nil,
		windows.OPEN_EXISTING, windows.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		t.Fatal(err)
	}
	closed := false
	t.Cleanup(func() {
		if !closed {
			windows.CloseHandle(h)
		}
	})
	if _, err := os.ReadFile(Path()); !errors.Is(err, windows.ERROR_SHARING_VIOLATION) {
		t.Fatalf("the read restriction was not established: %v", err)
	}
	saved := Save(Settings{Theme: "light"})
	if err := windows.CloseHandle(h); err != nil {
		t.Fatal(err)
	}
	closed = true
	if !errors.Is(saved, windows.ERROR_SHARING_VIOLATION) {
		t.Errorf("Save returned %v, want the read-sharing error", saved)
	}
	if after, err := os.ReadFile(Path()); err != nil || !bytes.Equal(after, original) {
		t.Errorf("the read-blocked settings were overwritten: %v", err)
	}
}

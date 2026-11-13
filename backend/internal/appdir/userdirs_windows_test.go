package appdir

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/windows/registry"
)

// The known folder read is Windows' own: the user's Downloads as Explorer
// keeps it in User Shell Folders (where a move to another drive or
// OneDrive is written), read only. Windows expands its %USERPROFILE% once
// a process, with the environment of the first call, so the check runs in
// a child of its own that makes that call here.
func TestKnownFolderIsExplorers(t *testing.T) {
	if os.Getenv("UNI_SWITCH_KNOWN_FOLDER_CHILD") != "1" {
		cmd := exec.Command(os.Args[0], "-test.run=^TestKnownFolderIsExplorers$", "-test.v")
		cmd.Env = append(os.Environ(), "UNI_SWITCH_KNOWN_FOLDER_CHILD=1")
		out, err := cmd.CombinedOutput()
		t.Logf("%s", out)
		if err != nil {
			t.Fatal(err)
		}
		return
	}
	path, profile := osKnownFolder(DownloadsFolder)
	if path == "" || profile == "" {
		t.Fatalf("no known folder: %q, %q", path, profile)
	}
	k, err := registry.OpenKey(registry.CURRENT_USER, `Software\Microsoft\Windows\CurrentVersion\Explorer\User Shell Folders`, registry.QUERY_VALUE)
	if err != nil {
		t.Skip("no User Shell Folders:", err)
	}
	defer k.Close()
	v, _, err := k.GetStringValue("{374DE290-123F-4565-9164-39C4925E467B}")
	if err != nil {
		t.Skip("Downloads not in User Shell Folders:", err)
	}
	if v, err = registry.ExpandString(v); err != nil {
		t.Fatal(err)
	}
	if !strings.EqualFold(filepath.Clean(v), filepath.Clean(path)) {
		t.Errorf("known folder %q, Explorer's %q", path, v)
	}
	t.Logf("Downloads %s, profile %s", path, profile)
}

package agent

import (
	"os/exec"
	"os/user"
	"path/filepath"
	"testing"
)

// A folder this user may not make files in, as Node.js's own under
// Program Files is to one not running as administrator (新v学员 on
// Discord), isn't writable: npm install -g there fails with EPERM. Before,
// every folder was taken as writable on Windows.
func TestWritableSeesDeniedFolder(t *testing.T) {
	u, err := user.Current()
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(t.TempDir(), "nodejs", "node_modules")
	if err := exec.Command("cmd", "/c", "mkdir", dir).Run(); err != nil {
		t.Fatal(err)
	}
	if !writable(dir) {
		t.Fatal("a folder of the user's own isn't writable")
	}
	if out, err := exec.Command("icacls", dir, "/deny", "*"+u.Uid+":(OI)(CI)(W)").CombinedOutput(); err != nil {
		t.Fatalf("icacls /deny: %v %s", err, out)
	}
	t.Cleanup(func() { exec.Command("icacls", dir, "/remove:d", "*"+u.Uid).Run() })
	if writable(dir) {
		t.Error("a folder the user can't make files in is writable")
	}
}

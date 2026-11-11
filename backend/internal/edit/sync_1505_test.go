package edit

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/steady"
)

// WriteAtomic (settings, profiles, caller keys, agents' configs) flushes
// the new file before renaming it in (#1505): one that fails to flush
// leaves the old file.
func TestWriteAtomicSyncsBeforeRename(t *testing.T) {
	p := filepath.Join(t.TempDir(), "settings.json")
	os.WriteFile(p, []byte(`{"old":1}`), 0o600)
	defer func() { steady.Sync = (*os.File).Sync }()
	steady.Sync = func(*os.File) error { return errors.New("the disk went away") }
	if err := WriteAtomic(p, []byte(`{"new":1}`)); err == nil {
		t.Fatal("written without a flush")
	}
	if b, _ := os.ReadFile(p); string(b) != `{"old":1}` {
		t.Fatalf("a failed flush left %q", b)
	}
	if left, _ := filepath.Glob(filepath.Join(filepath.Dir(p), "*")); len(left) != 1 {
		t.Fatalf("left %v", left)
	}
}

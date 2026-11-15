package usage

import (
	"os"
	"path/filepath"
	"testing"
)

// usage-computer.json holds the id this computer's synced days are under.
// One that doesn't parse was written over with a new id and no copy
// (#2263): it is copied aside first.
func TestUnreadComputerKeptAside(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	p := filepath.Join(dir(), "usage-computer.json")
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		t.Fatal(err)
	}
	was := `{"id":"0123456789abcdef","name":"desk`
	if err := os.WriteFile(p, []byte(was), 0o600); err != nil {
		t.Fatal(err)
	}
	if id, _ := Computer(); id == "" {
		t.Fatal("no id")
	}
	bad, _ := filepath.Glob(p + ".bad-*")
	if len(bad) != 1 {
		t.Fatalf("kept aside: %v", bad)
	}
	if b, _ := os.ReadFile(bad[0]); string(b) != was {
		t.Fatalf("kept aside as %q, want %q", b, was)
	}
}

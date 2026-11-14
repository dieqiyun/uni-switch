package provider

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// An agent's credentials file linked in from elsewhere (#323): uni-switch's
// writes go to the file it points at and the link stays, its mode 0600.
func TestPrivateWritesKeepSymlink(t *testing.T) {
	d := t.TempDir()
	for name, write := range map[string]func(string, []byte) error{
		"writePrivate": writePrivate, "writeFileAtomic": writeFileAtomic,
	} {
		target := filepath.Join(d, name+"-real.json")
		if err := os.WriteFile(target, []byte("{}"), 0o600); err != nil {
			t.Fatal(err)
		}
		p := filepath.Join(d, name+".json")
		if err := os.Symlink(target, p); err != nil {
			t.Skip("no symlinks here:", err)
		}
		if err := write(p, []byte(`{"a":1}`)); err != nil {
			t.Fatal(name, err)
		}
		if fi, err := os.Lstat(p); err != nil || fi.Mode()&os.ModeSymlink == 0 {
			t.Errorf("%s: the link was replaced (%v)", name, err)
		}
		b, _ := os.ReadFile(target)
		if string(b) != `{"a":1}` {
			t.Errorf("%s: target %q", name, b)
		}
		if st, _ := os.Stat(target); runtime.GOOS != "windows" && st.Mode().Perm() != 0o600 {
			t.Errorf("%s: mode %v", name, st.Mode().Perm())
		}
	}
}

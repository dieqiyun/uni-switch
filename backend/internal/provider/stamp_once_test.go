package provider

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestStampLooksOnceARequest: a request holding the catalog looks at
// StampNow's files once, not once for each provider it lists, and the
// next request sees a file another process wrote between them.
func TestStampLooksOnceARequest(t *testing.T) {
	isolate(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
	defer func(d time.Duration) { heldFor = d }(heldFor)
	heldFor = time.Hour // a loaded machine's pause isn't the bound
	if err := Save(Provider{ID: "relay", Name: "Relay", Key: "k", Chat: "http://127.0.0.1:1/v1", Models: []string{"m1"}}); err != nil {
		t.Fatal(err)
	}
	release := Hold()
	looks := stampLooks.Load()
	first := StampNow()
	for range 50 {
		if StampNow() != first {
			t.Fatal("the stamp moved within a request")
		}
	}
	if n := stampLooks.Load() - looks; n != 1 {
		t.Fatalf("51 stamps in one request looked %d times, want 1", n)
	}
	release()

	// another process rewrites the providers' file: a size of its own
	b, err := os.ReadFile(Path())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(Path(), append(b, '\n', '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	defer Hold()()
	if StampNow() == first {
		t.Fatal("the next request's stamp missed another process's write")
	}
}

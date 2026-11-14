package provider

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// What a request or a background look asks of one provider (its proxy,
// whether it is left alone) is read from providers.json as parsed once,
// not a parse of the whole file each time: 3.6 GB of a live uni-switch's 33 GB
// in 53 minutes. A save, and another process's edit, are seen at once.
func TestStoredParsedOnceUntilTheFileChanges(t *testing.T) {
	isolate(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
	var models []string
	for i := range 100 {
		models = append(models, fmt.Sprintf("m-%d", i))
	}
	for i := range 30 {
		id := fmt.Sprintf("r%d", i)
		if err := Save(Provider{ID: id, Name: id, Key: "k", Chat: "http://127.0.0.1:1/v1", Models: models, Proxy: "http://127.0.0.1:9/"}); err != nil {
			t.Fatal(err)
		}
	}
	// written long enough ago that its time tells a later write apart
	old := time.Now().Add(-time.Hour)
	if err := os.Chtimes(Path(), old, old); err != nil {
		t.Fatal(err)
	}
	if p, ok := storedPicks("r7"); !ok || p.Proxy == "" {
		t.Fatalf("r7 not read: %+v %v", p, ok)
	}
	parsed := testing.AllocsPerRun(5, func() { load() })
	for name, f := range map[string]func(){
		"storedPicks": func() { storedPicks("r29") },
		"leftAlone":   func() { leftAlone("r29") },
	} {
		if n := testing.AllocsPerRun(20, f); n > 40 || n*20 > parsed {
			t.Errorf("%s with the file unchanged: %.0f allocations, a parse %.0f", name, n, parsed)
		}
	}

	p, _ := storedPicks("r3")
	p.Off = true
	if err := Save(p); err != nil {
		t.Fatal(err)
	}
	if !leftAlone("r3") {
		t.Fatal("a provider switched off since is not left alone")
	}

	// another process's edit, of the same size and an older time: the
	// time alone tells it apart
	b, err := os.ReadFile(Path())
	if err != nil {
		t.Fatal(err)
	}
	edited := strings.Replace(string(b), `"id": "r29"`, `"id": "r9x"`, 1)
	if edited == string(b) {
		t.Fatal("r29 not in the file as written")
	}
	if err := os.WriteFile(Path(), []byte(edited), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(Path(), old.Add(time.Minute), old.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if _, ok := storedPicks("r29"); ok {
		t.Fatal("another process's edit is not seen")
	}
	if _, ok := storedPicks("r9x"); !ok {
		t.Fatal("the provider another process renamed is not found")
	}

	// a file that doesn't decode is read as load reads it: its last good
	// generation, not none
	if err := os.WriteFile(Path(), make([]byte, len(edited)), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, ok := storedPicks("r3"); !ok {
		t.Fatal("a broken providers.json is read as no providers")
	}
}

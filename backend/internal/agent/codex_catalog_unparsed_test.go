package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// A uni-switch-models.json that doesn't parse is kept aside before uni-switch
// writes its list over the file or takes the file away: Keep (#1450) can
// hold nothing of a file it can't read, so the keys the user added by hand
// would end with no copy — the rule the other state files were given
// (#2263), which this file missed.
func TestUnparsedCodexCatalogKeptAside(t *testing.T) {
	home, _ := codexHome(t, "", "")
	list := filepath.Join(home, ".codex", "uni-switch-models.json")
	cx := codex(home)
	if err := cx.Fields[0].Set("fake/m1"); err != nil {
		t.Fatal(err)
	}
	// the file as it stands, broken the way a half-save or a sync conflict
	// leaves one: a cut before the last brace, no longer a catalog
	b := readFile(list)
	writeFile(t, list, b[:len(b)-2])

	// a provider added: the sync writes the list again
	if err := provider.Save(provider.Provider{ID: "added", Name: "Added", Key: "k", Chat: "http://127.0.0.1:1/v1", Models: []string{"m2"}}); err != nil {
		t.Fatal(err)
	}
	SyncCatalog()
	if !strings.Contains(readFile(list), `"added/m2"`) {
		t.Fatal("sync didn't write the new model")
	}
	if bad, _ := filepath.Glob(list + ".bad-*"); len(bad) == 0 {
		t.Fatalf("the unparsed list was written over with no copy kept")
	}

	// the list broken again, and uni-switch steps out: the file goes, but a
	// copy of what it held stays
	writeFile(t, list, b[:len(b)-2])
	if err := cx.Fields[0].Set(""); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(list); err == nil {
		t.Error("a list uni-switch steps out of stayed")
	}
	if bad, _ := filepath.Glob(list + ".bad-*"); len(bad) == 0 {
		t.Fatalf("the unparsed list was removed with no copy kept")
	}
}

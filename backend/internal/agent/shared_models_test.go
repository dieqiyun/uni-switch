package agent

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// A look that only ranges over uni-switch's models reads the list kept, not a
// copy: a copy per look was 37 MB of what forty window polls allocated
// with real accounts, most of it Pi's and Claude Code's thinking levels.
func TestModelReadersShareTheList(t *testing.T) {
	syncHome(t)
	if err := provider.Save(provider.Provider{ID: "oa", Name: "OA", Key: "k", Chat: "http://127.0.0.1:1/v1", Models: []string{"gpt-5.5", "gpt-5.4"}}); err != nil {
		t.Fatal(err)
	}
	for range 5 {
		got := sharedModels("pi")
		builtModels.Lock()
		kept := builtModels.m["pi"].list
		builtModels.Unlock()
		if len(got) == 0 || &got[0] != &kept[0] {
			t.Fatal("a shared look copies the kept list")
		}
	}

	// every look that ranges over the list, or counts it, takes it shared
	reads := regexp.MustCompile(`(range|len\()\s*uniSwitchModels\(`)
	files, _ := filepath.Glob("*.go")
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		if m := reads.Find(b); m != nil {
			t.Errorf("%s reads uniSwitchModels' copy only to range over it: %s", f, m)
		}
	}
}

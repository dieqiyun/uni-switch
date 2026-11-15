package agent

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// A config.toml with one table for each project Codex was opened in grows
// to megabytes (10,932 tables, 1.5 MB on one user's Mac). A sync that finds
// nothing to change decodes only the few tables it reads, not the whole
// file once for each of them: 21 MB a sync here before, about 5 MB after.
func TestCodexSyncDecodesTheConfigOnce(t *testing.T) {
	var b strings.Builder
	b.WriteString("model = \"gpt-5.5\"\n\n")
	for i := range 10000 {
		fmt.Fprintf(&b, "[projects.\"/Users/someone/workspace/projects/project-%d/sub\"]\ntrust_level = \"trusted\"\n\n", i)
	}
	home, _ := codexHome(t, "", b.String())
	if err := codex(home).Fields[0].Set("fake/m1"); err != nil {
		t.Fatal(err)
	}
	SyncCatalog()
	// Written an hour ago, so a read is the steady one, not a just-written
	// file read again to compare.
	old := time.Now().Add(-time.Hour)
	cfg := filepath.Join(home, ".codex", "config.toml")
	for _, f := range []string{cfg, filepath.Join(home, ".codex", "uni-switch-models.json")} {
		if err := os.Chtimes(f, old, old); err != nil {
			t.Fatal(err)
		}
	}
	st, err := os.Stat(cfg)
	if err != nil {
		t.Fatal(err)
	}
	const syncs = 5
	var m0, m1 runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&m0)
	for range syncs {
		SyncCatalog()
	}
	runtime.ReadMemStats(&m1)
	per := (m1.TotalAlloc - m0.TotalAlloc) / syncs
	if limit := uint64(12 * st.Size()); per > limit {
		t.Errorf("a sync of a %d KB config allocates %d KB, over %d KB", st.Size()>>10, per>>10, limit>>10)
	}
	if after, _ := os.Stat(cfg); !after.ModTime().Equal(st.ModTime()) {
		t.Errorf("a sync with nothing to change wrote config.toml")
	}
}

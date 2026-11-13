package codexcat

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/catalog"
)

// TakesEffort is asked per model each window poll; it reads Codex's
// models_cache.json again only when the file changes, and still says what
// the cache says: its level for Codex's own and a ChatGPT account's model,
// none for one uni-switch added, the levels' default for one not in it.
func TestTakesEffortReadsTheCacheOnChange(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("CODEX_HOME", "")
	os.MkdirAll(filepath.Join(home, ".codex"), 0o755)
	path := filepath.Join(home, ".codex", "models_cache.json")
	write := func(level string, at time.Time) {
		var b strings.Builder
		fmt.Fprintf(&b, `{"etag":"e","models":[{"slug":"gpt-6.1-sol","default_reasoning_level":%q},{"slug":"uni-switch/x","description":"X via uni-switch","default_reasoning_level":"high"}`, level)
		for i := range 300 {
			fmt.Fprintf(&b, `,{"slug":"m-%d","description":"%s","default_reasoning_level":"medium"}`, i, strings.Repeat("words ", 40))
		}
		b.WriteString("]}")
		if err := os.WriteFile(path, []byte(b.String()), 0o600); err != nil {
			t.Fatal(err)
		}
		os.Chtimes(path, at, at)
	}
	write("low", time.Now().Add(-time.Hour))
	efforts := []string{"low", "medium", "high"}
	ms := []catalog.Model{{ID: "gpt-6.1-sol", Efforts: efforts}, {ID: "codex/gpt-6.1-sol", Efforts: efforts}, {ID: "uni-switch/x", Efforts: efforts}, {ID: "other", Efforts: efforts}}
	if got := TakesEffort(ms, "gpt-6.1-sol"); got != "low" {
		t.Errorf("Codex's own: %q, want low", got)
	}
	if got := TakesEffort(ms, "codex/gpt-6.1-sol"); got != "low" {
		t.Errorf("a ChatGPT account's: %q, want low", got)
	}
	if got, want := TakesEffort(ms, "uni-switch/x"), DefaultEffort(efforts); got != want {
		t.Errorf("one uni-switch added: %q, want the default %q", got, want)
	}
	if got, want := TakesEffort(ms, "other"), DefaultEffort(efforts); got != want {
		t.Errorf("not in the cache: %q, want %q", got, want)
	}
	if n := testing.AllocsPerRun(20, func() { TakesEffort(ms, "gpt-6.1-sol") }); n > 20 {
		t.Errorf("a look at an unchanged cache: %.0f allocations, want a few", n)
	}
	write("xhigh", time.Now().Add(time.Minute))
	if got := TakesEffort(ms, "gpt-6.1-sol"); got != "xhigh" {
		t.Errorf("after Codex wrote the cache: %q, want xhigh", got)
	}
}

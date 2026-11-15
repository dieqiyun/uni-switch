package usage

import (
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/catalog"
)

// silverzhou on Discord: OpenAI made Codex's Auto-review free for users
// signed in through a ChatGPT account, and the Usage page still counted
// it at GPT-5.6 Luna's price. A codex-auto-review call through the Codex
// subscription is now priced at nothing, rows logged before the change
// too (they are priced as they are read); the same model through an API
// key, and a review that followed the session to another model, are
// priced as before. The key limits' cost counts the same (NewCoster).
func TestAutoReviewFreeOnChatGPT(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
	os.MkdirAll(filepath.Dir(catalog.CachePath()), 0o755)
	if err := os.WriteFile(catalog.CachePath(), []byte(`{"openai":{"id":"openai","models":{`+
		`"gpt-5.6-luna":{"id":"gpt-5.6-luna","cost":{"input":3,"output":7,"cache_read":1}},`+
		`"gpt-5.6":{"id":"gpt-5.6","cost":{"input":5,"output":20,"cache_read":0.5}}}}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	catalog.Reset()
	t.Cleanup(catalog.Reset)
	in, out, cached := 40_000, 2_000, 100_000
	luna := (float64(in)*3 + float64(out)*7 + float64(cached)*1) / 1e6
	gpt := (float64(in)*5 + float64(out)*20 + float64(cached)*0.5) / 1e6
	now := time.Now()
	calls := []struct {
		prov, model, asked string
		want               float64
	}{
		{"codex", "codex-auto-review", "codex-auto-review", 0},
		{"openai", "codex-auto-review", "codex-auto-review", luna},
		{"codex", "gpt-5.6", "codex-auto-review", gpt}, // followed the session
		{"codex", "gpt-5.6-luna", "gpt-5.6-luna", luna},
	}
	for i, c := range calls {
		Append(Record{Time: now.Add(-time.Duration(len(calls)-i) * time.Minute), Agent: "codex", Provider: c.prov, Model: c.model, Requested: c.asked,
			Kind: "guardian", Input: in, Output: out, CacheRead: cached, Status: 200})
	}
	rows, sum, _ := Ledger(Month, Filter{})
	if len(rows) != len(calls) {
		t.Fatalf("rows %d", len(rows))
	}
	var total float64
	for i, c := range calls {
		r := rows[len(calls)-1-i]
		if r.Provider != c.prov || r.Model != c.model || !r.Priced || math.Abs(r.Cost-c.want) > 1e-9 {
			t.Errorf("%s/%s: priced=%v cost=%v, want %v", c.prov, c.model, r.Priced, r.Cost, c.want)
		}
		total += c.want
	}
	if s := Summarize(Month); s.Unpriced != 0 || math.Abs(s.Cost-total) > 1e-9 || math.Abs(sum.Cost-total) > 1e-9 {
		t.Errorf("summary %v (unpriced %d), ledger %v; want %v", s.Cost, s.Unpriced, sum.Cost, total)
	}
	cost := NewCoster()
	if c, ok := cost(Record{Provider: "codex", Model: "codex-auto-review", Input: in, Output: out}); !ok || c != 0 {
		t.Errorf("key limit counts a free review at %v (%v)", c, ok)
	}
	if c, ok := cost(Record{Provider: "openai", Model: "codex-auto-review", Input: in, Output: out}); !ok || c == 0 {
		t.Errorf("key limit counts an API key's review at %v (%v)", c, ok)
	}
}

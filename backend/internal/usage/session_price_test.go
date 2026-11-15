package usage

import (
	"encoding/csv"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/catalog"
	"github.com/dieqiyun/uni-switch/backend/internal/sessions"
)

// A local session's usage entry names the provider the call went through
// (omp's model_usage: provider openrouter, model typesafe/jev-1.14,
// #4278), which its billing provider — the unknown one — never says. That
// name prices only what the catalog itself lists under it: the very model
// id, at that provider, with a price. A provider the catalog doesn't know,
// a model it lists that provider without, and a listing with no price are
// all left unpriced rather than priced at another entry's guess.
func TestUnknownProviderPricedBySessionProvider(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
	os.MkdirAll(filepath.Dir(catalog.CachePath()), 0o755)
	os.WriteFile(catalog.CachePath(), []byte(`{`+
		`"openrouter":{"id":"openrouter","models":{"typesafe/jev-1.14":{"id":"typesafe/jev-1.14","cost":{"input":0.042,"output":0}}}},`+
		`"nano-gpt":{"id":"nano-gpt","models":{"typesafe/jev-1.13":{"id":"typesafe/jev-1.13","cost":{"input":0.042,"output":0}}}},`+
		`"vivgrid":{"id":"vivgrid","models":{"typesafe/jev-1.15":{"id":"typesafe/jev-1.15"}}}}`), 0o644)
	catalog.Reset()
	t.Cleanup(catalog.Reset)

	now := holdClock(t, time.Date(2026, 9, 30, 12, 0, 0, 0, time.Local))
	old := LogCalls
	LogCalls = func(time.Time) []sessions.Call {
		return []sessions.Call{
			// the reporter's: the file names a provider the catalog lists
			// this very model under, at its input price
			{Time: now.Add(-time.Hour), Agent: "omp", Session: "o1", Model: "typesafe/jev-1.14", Upstream: "openrouter",
				Tokens: sessions.Tokens{Input: 100000, Output: 20000}},
			// the file names a provider the catalog knows, but not this model of it
			{Time: now.Add(-2 * time.Hour), Agent: "omp", Session: "o2", Model: "typesafe/jev-1.14", Upstream: "nano-gpt",
				Tokens: sessions.Tokens{Input: 100000, Output: 20000}},
			// the file names a provider the catalog doesn't know at all
			{Time: now.Add(-3 * time.Hour), Agent: "omp", Session: "o3", Model: "typesafe/jev-1.14", Upstream: "home-relay",
				Tokens: sessions.Tokens{Input: 100000, Output: 20000}},
			// the catalog lists the model at that provider without a price
			{Time: now.Add(-4 * time.Hour), Agent: "omp", Session: "o4", Model: "typesafe/jev-1.15", Upstream: "vivgrid",
				Tokens: sessions.Tokens{Input: 100000, Output: 20000}},
		}
	}
	t.Cleanup(func() { LogCalls = old })

	rows, _, _ := Ledger(Month, Filter{})
	if len(rows) != 4 {
		t.Fatalf("rows %d", len(rows))
	}
	bySession := map[string]Row{}
	for _, r := range rows {
		if r.Source != "log" || r.Provider != UnknownProvider {
			t.Fatalf("not a session-unknown log row: %+v", r.Record)
		}
		bySession[r.Session] = r
	}
	priced := bySession["o1"]
	if priced.SessionProvider != "openrouter" {
		t.Fatalf("the file's provider isn't the row's: %+v", priced.Record)
	}
	if !priced.Priced {
		t.Fatalf("the named route stayed unpriced: %+v", priced.Record)
	}
	if want := (100000*0.042 + 20000*0) / 1e6; priced.Cost != want {
		t.Fatalf("cost %v, want %v", priced.Cost, want)
	}
	for _, s := range []string{"o2", "o3", "o4"} {
		if r := bySession[s]; r.Priced {
			t.Fatalf("%s priced at a guess: %+v", s, r.Record)
		}
	}

	// the CSV says the same: a cost where the catalog affirmed the pair,
	// empty where it didn't (#4278's cost_usd)
	var b strings.Builder
	if err := WriteCSV(&b, rows); err != nil {
		t.Fatal(err)
	}
	rec, err := csv.NewReader(strings.NewReader(b.String())).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	cost := -1
	for i, h := range rec[0] {
		if h == "cost_usd" {
			cost = i
		}
	}
	session := -1
	for i, h := range rec[0] {
		if h == "session" {
			session = i
		}
	}
	if cost < 0 || session < 0 {
		t.Fatalf("no cost_usd or session column: %v", rec[0])
	}
	for _, r := range rec[1:] {
		id, got := r[session], r[cost]
		want := ""
		if id == "o1" {
			want = "0.004200"
		}
		if got != want {
			t.Fatalf("%s cost_usd %q, want %q", id, got, want)
		}
	}
}

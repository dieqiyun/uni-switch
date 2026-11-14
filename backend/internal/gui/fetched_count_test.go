package gui

import (
	"os"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/catalog"
	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// A refetch's "{p}: {n} models" counts the models the editor lists, not
// the vendor's ids: Antigravity's ids of one model at several levels are
// one chip, and the toast said 23 over 17 chips (#2069).
func TestFetchedCountIsTheEditorsModels(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("USERPROFILE", os.Getenv("HOME"))
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	ids := []catalog.Model{
		{ID: "gemini-3.7-flash-high", Name: "Gemini 3.7 Flash (High)"},
		{ID: "gemini-3.7-flash-low", Name: "Gemini 3.7 Flash (Low)"},
		{ID: "gemini-3.7-flash-medium", Name: "Gemini 3.7 Flash (Medium)"},
		{ID: "gemini-3.1-pro-high", Name: "Gemini 3.1 Pro (High)"},
		{ID: "gemini-3.1-pro-low", Name: "Gemini 3.1 Pro (Low)"},
		{ID: "gpt-oss-120b-medium", Name: "GPT-OSS 120B (Medium)"},
	}
	if err := catalog.SaveLive("antigravity", "", ids); err != nil {
		t.Fatal(err)
	}
	p := provider.Provider{ID: "antigravity"}
	if n := len(p.Available()); n != 3 {
		t.Fatalf("the editor lists %d models, want 3: %+v", n, p.Available())
	}
	if got := fetchedCount(p, len(ids)); got != 3 {
		t.Fatalf("toast counts %d, want the editor's 3", got)
	}
	// a provider that offers nothing says the ids it was given
	if got := fetchedCount(provider.Provider{ID: "nothing-here"}, 4); got != 4 {
		t.Fatalf("empty provider: %d", got)
	}
}

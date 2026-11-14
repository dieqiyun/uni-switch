package agent

import (
	"path/filepath"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/catalog"
	"github.com/dieqiyun/uni-switch/backend/internal/edit"
	"github.com/dieqiyun/uni-switch/backend/internal/provider"
	"github.com/dieqiyun/uni-switch/backend/internal/settings"
)

// TestClaudeContextWindow: Claude Code takes a model it doesn't know for a
// 200K one, so uni-switch tells it the product's 256K default through
// CLAUDE_CODE_MAX_CONTEXT_TOKENS, including unknown models. Explicit [1m]
// choices and values the user set themselves remain authoritative.
func TestClaudeContextWindow(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
	if err := provider.Save(provider.Provider{ID: "v", Name: "V", Chat: "https://example.test/v1", Key: "k",
		Models: []string{"small", "mid", "big", "plain"}}); err != nil {
		t.Fatal(err)
	}
	if err := catalog.SaveLive("v", "https://example.test/v1", []catalog.Model{
		{ID: "small", Context: 128000}, {ID: "mid", Context: 400000}, {ID: "big", Context: 1000000}, {ID: "plain"},
	}); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(home, ".claude", "settings.json")
	writeFile(t, path, `{"theme":"dark"}`)
	a := claude(home)
	window := func() string { v, _ := edit.GetJSON(path, "env."+claudeContextEnv); return v }
	set := func(key, v string) {
		t.Helper()
		if err := a.Field(key).Set(v); err != nil {
			t.Fatal(err)
		}
	}

	set("model", "v/small")
	if window() != "256000" {
		t.Fatalf("small: %q", window())
	}
	set("model", "v/mid")
	if window() != "256000" {
		t.Fatalf("switched to mid: %q", window())
	}
	set("model", "v/plain")
	if window() != "256000" {
		t.Fatalf("a model of no known window should use 256K: %q", window())
	}
	// Larger windows now require an explicit provider/model adjustment.
	p, err := provider.Find("v")
	if err != nil {
		t.Fatal(err)
	}
	p.Contexts = map[string]int{"big": 1000000}
	if err := provider.Save(*p); err != nil {
		t.Fatal(err)
	}
	// a 1M model is marked [1m], which Claude Code takes for 1M whatever
	// the window says; beside a tier that isn't, the window told is the
	// main's own, and the tier compacts by its own (多喝热水 on Discord:
	// the tier's read as the main's), under Full window and without it
	set("model", "v/big[1m]")
	if window() != "" {
		t.Fatalf("[1m]: %q", window())
	}
	budget := func(id string) string { v, _ := edit.GetJSON(path, budgetPath(id)); return v }
	compact := func() (env, top string) {
		env, _ = edit.GetJSON(path, "env."+claudeCompactEnv)
		top, _ = edit.GetJSON(path, "autoCompactWindow")
		return env, top
	}
	set("haiku", "v/small")
	if e, top := compact(); window() != "1000000" || budget("v/small") != "256000" || budget("v/big[1m]") != "1000000" || e != "" || top != "272000" {
		t.Fatalf("haiku on small beside a [1m] model: window %q, small %q, big %q, compact env %q, top-level %q",
			window(), budget("v/small"), budget("v/big[1m]"), e, top)
	}
	if err := settings.Save(settings.Settings{FullContext: true}); err != nil {
		t.Fatal(err)
	}
	if err := a.Sync(); err != nil {
		t.Fatal(err)
	}
	if e, top := compact(); window() != "1000000" || budget("v/small") != "256000" || e != "" || top != "" {
		t.Fatalf("full window: window %q, small %q, compact env %q, top-level %q", window(), budget("v/small"), e, top)
	}
	if err := settings.Save(settings.Settings{}); err != nil {
		t.Fatal(err)
	}
	set("haiku", "")
	if e, top := compact(); window() != "" || budget("v/small") != "" || e != "" || top != "" {
		t.Fatalf("the tier gone: window %q, small %q, compact env %q, top-level %q", window(), budget("v/small"), e, top)
	}
	set("model", "v/mid")
	if window() != "256000" {
		t.Fatalf("the main model's window wins: %q", window())
	}
	// back to Claude's own model, and to Claude Code as installed
	set("model", "claude-opus-4-8")
	if window() != "" {
		t.Fatalf("Claude's own model kept uni-switch's window: %q", window())
	}
	set("model", "v/mid")
	set("model", "")
	if window() != "" {
		t.Fatalf("reset kept uni-switch's window: %q", window())
	}
	if v, _ := edit.GetJSON(path, "theme"); v != "dark" {
		t.Fatal("theme lost")
	}

	// the user's own value is theirs, set before uni-switch or over uni-switch's
	if err := edit.SetJSON(path, edit.KV{Path: "env." + claudeContextEnv, Value: "64000"}); err != nil {
		t.Fatal(err)
	}
	for _, v := range []string{"v/small", "v/plain", "v/mid", "claude-opus-4-8", ""} {
		set("model", v)
		if window() != "64000" {
			t.Fatalf("the user's value after %q: %q", v, window())
		}
	}
	if err := edit.DelJSON(path, "env."+claudeContextEnv); err != nil {
		t.Fatal(err)
	}
	set("model", "v/small")
	if err := edit.SetJSON(path, edit.KV{Path: "env." + claudeContextEnv, Value: "100000"}); err != nil {
		t.Fatal(err)
	}
	set("model", "v/mid")
	set("model", "")
	if window() != "100000" {
		t.Fatalf("a value the user changed uni-switch's to: %q", window())
	}
	// and one the same as uni-switch would write, but not written by it
	set("model", "v/small")
	if window() != "100000" {
		t.Fatalf("user value replaced: %q", window())
	}
	if err := edit.SetJSON(path, edit.KV{Path: "env." + claudeContextEnv, Value: "400000"}); err != nil {
		t.Fatal(err)
	}
	set("model", "")
	if window() != "400000" {
		t.Fatalf("the user's 400000 was taken for uni-switch's: %q", window())
	}
}

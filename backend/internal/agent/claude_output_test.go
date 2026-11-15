package agent

import (
	"path/filepath"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/catalog"
	"github.com/dieqiyun/uni-switch/backend/internal/edit"
	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// TestClaudeOutputTokens: Claude Code asks a model it doesn't know for a
// reply of 32000 tokens at most, so a reasoning model stopped at "Claude's
// response exceeded the 32000 output token maximum" (H20, DeepSeek through
// WorkBuddy). uni-switch tells it the longest reply the models it runs on can
// write (CLAUDE_CODE_MAX_OUTPUT_TOKENS): the least of theirs, none when one
// isn't known, is no more than 32000 or the main model is Claude's own, and
// never over a value the user set.
func TestClaudeOutputTokens(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
	if err := provider.Save(provider.Provider{ID: "v", Name: "V", Chat: "https://example.test/v1", Key: "k",
		Models: []string{"deep", "mid", "short", "plain"}}); err != nil {
		t.Fatal(err)
	}
	if err := catalog.SaveLive("v", "https://example.test/v1", []catalog.Model{
		{ID: "deep", Context: 1000000, Output: 384000}, {ID: "mid", Context: 400000, Output: 64000},
		{ID: "short", Context: 128000, Output: 8192}, {ID: "plain"},
	}); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(home, ".claude", "settings.json")
	writeFile(t, path, `{"theme":"dark"}`)
	a := claude(home)
	output := func() string { v, _ := edit.GetJSON(path, "env."+claudeOutputEnv); return v }
	set := func(key, v string) {
		t.Helper()
		if err := a.Field(key).Set(v); err != nil {
			t.Fatal(err)
		}
	}

	set("model", "v/mid")
	if output() != "64000" {
		t.Fatalf("mid: %q", output())
	}
	set("model", "v/deep")
	if output() != "384000" {
		t.Fatalf("switched to deep: %q", output())
	}
	// one value serves every model Claude Code runs on: a tier's smaller
	// limit bounds it
	set("haiku", "v/mid")
	if output() != "64000" {
		t.Fatalf("haiku on mid beside deep: %q", output())
	}
	// no more than Claude Code asks for anyway, or not known: left out
	set("haiku", "v/short")
	if output() != "" {
		t.Fatalf("haiku on an 8K model: %q", output())
	}
	set("haiku", "v/plain")
	if output() != "" {
		t.Fatalf("haiku on a model of no known limit: %q", output())
	}
	set("haiku", "v/deep")
	set("model", "v/plain")
	if output() != "" {
		t.Fatalf("a main model of no known limit: %q", output())
	}
	set("model", "v/deep")
	if output() != "384000" {
		t.Fatalf("back on deep: %q", output())
	}
	// Claude's own model, which Claude Code knows, and Claude Code as
	// installed
	set("model", "claude-opus-4-8")
	if output() != "" {
		t.Fatalf("Claude's own model kept uni-switch's value: %q", output())
	}
	set("model", "v/deep")
	set("model", "")
	if output() != "" {
		t.Fatalf("reset kept uni-switch's value: %q", output())
	}
	if v, _ := edit.GetJSON(path, "theme"); v != "dark" {
		t.Fatal("theme lost")
	}

	// the user's own value is theirs, set before uni-switch or over uni-switch's
	if err := edit.SetJSON(path, edit.KV{Path: "env." + claudeOutputEnv, Value: "64000"}); err != nil {
		t.Fatal(err)
	}
	for _, v := range []string{"v/deep", "v/plain", "v/mid", "claude-opus-4-8", ""} {
		set("model", v)
		if output() != "64000" {
			t.Fatalf("the user's value after %q: %q", v, output())
		}
	}
	if err := edit.DelJSON(path, "env."+claudeOutputEnv); err != nil {
		t.Fatal(err)
	}
	set("model", "v/deep")
	if err := edit.SetJSON(path, edit.KV{Path: "env." + claudeOutputEnv, Value: "100000"}); err != nil {
		t.Fatal(err)
	}
	set("model", "v/mid")
	set("model", "")
	if output() != "100000" {
		t.Fatalf("a value the user changed uni-switch's to: %q", output())
	}
	// and one the same as uni-switch would write, but not written by it
	if err := edit.SetJSON(path, edit.KV{Path: "env." + claudeOutputEnv, Value: "64000"}); err != nil {
		t.Fatal(err)
	}
	set("model", "v/mid")
	set("model", "")
	if output() != "64000" {
		t.Fatalf("the user's 64000 was taken for uni-switch's: %q", output())
	}
}

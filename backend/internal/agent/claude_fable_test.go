package agent

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/edit"
	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// On a Claude model that is no Fable, Claude Code's fable tier is left
// unset rather than following the main model: Claude Code takes the model
// ANTHROPIC_DEFAULT_FABLE_MODEL names for a Fable model, so Opus written
// there was Fable to it, and a Claude Pro sign-in without Fable asked for
// usage credits and fell back to Haiku (Zhenzhen on Discord, uni-switch claude
// claude/claude-opus-5-5). So it is on another vendor's model (#2090). On a
// Fable it follows as before; a value of the user's own stays theirs.
func TestClaudeFableTierOnClaude(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
	for _, p := range []provider.Provider{
		{ID: "anth", Name: "Anthropic", Chat: "https://api.anthropic.com/v1", Key: "k", Models: []string{"claude-opus-5-5", "claude-sonnet-5", "claude-fable-5-1", "claude-haiku-4-5"}},
		{ID: "deepseek", Name: "DeepSeek", Chat: "https://api.deepseek.com/v1", Key: "k", Models: []string{"pro", "flash"}},
		{ID: "cx", Name: "Codex models", Chat: "https://codex.invalid/v1", Key: "k", Models: []string{"gpt-6.1-sol"}},
	} {
		if err := provider.Save(p); err != nil {
			t.Fatal(err)
		}
	}
	path := filepath.Join(home, ".claude", "settings.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	// the user's own fable model, from before uni-switch
	if err := os.WriteFile(path, []byte(`{"env":{"ANTHROPIC_DEFAULT_FABLE_MODEL":"my-fable"}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	a := claude(home)
	fable := func() (string, bool) { return edit.GetJSON(path, "env.ANTHROPIC_DEFAULT_FABLE_MODEL") }
	env := func(k string) string { v, _ := edit.GetJSON(path, "env."+k); return v }
	noFable := func(when string) {
		t.Helper()
		if v, has := fable(); has {
			t.Fatalf("%s: fable tier written as %q, want it left out:\n%s", when, v, readFile(path))
		}
	}

	if err := a.Field("model").Set("anth/claude-opus-5-5"); err != nil {
		t.Fatal(err)
	}
	noFable("on Opus")
	if env("ANTHROPIC_DEFAULT_OPUS_MODEL") != "anth/claude-opus-5-5" || env("ANTHROPIC_DEFAULT_SONNET_MODEL") != "anth/claude-opus-5-5" || env("ANTHROPIC_DEFAULT_HAIKU_MODEL") != "anth/claude-haiku-4-5" {
		t.Fatalf("the other tiers should follow as before:\n%s", readFile(path))
	}
	if a.Field("fable").Get() != "" {
		t.Fatalf("fable shown as %q, want following", a.Field("fable").Get())
	}
	// Claude Code's own fable, asked of the gateway, runs on the main model
	if got := claudeStandInAt(here(""), path, "claude-fable-5-1"); got != "anth/claude-opus-5-5" {
		t.Fatalf("claude-fable-5-1 stands in as %q", got)
	}
	// another Claude model that is no Fable
	if err := a.Field("model").Set("anth/claude-sonnet-5"); err != nil {
		t.Fatal(err)
	}
	noFable("on Sonnet")
	// a Fable: the fable tier follows it
	if err := a.Field("model").Set("anth/claude-fable-5-1"); err != nil {
		t.Fatal(err)
	}
	if v, _ := fable(); v != "anth/claude-fable-5-1" {
		t.Fatalf("on Fable, fable is %q", v)
	}
	if err := a.Field("model").Set("anth/claude-opus-5-5"); err != nil {
		t.Fatal(err)
	}
	noFable("back from Fable")
	// another vendor's model is no Fable either: GPT-6.1 Sol written there
	// was Fable to Claude Code, which asked for usage credits before Sol's
	// first turn (#2090)
	for _, m := range []string{"cx/gpt-6.1-sol", "deepseek/pro"} {
		if err := a.Field("model").Set(m); err != nil {
			t.Fatal(err)
		}
		noFable("on " + m)
		if env("ANTHROPIC_DEFAULT_OPUS_MODEL") != m || env("ANTHROPIC_DEFAULT_SONNET_MODEL") != m || env("ANTHROPIC_DEFAULT_HAIKU_MODEL") != m {
			t.Fatalf("on %s the other tiers should follow:\n%s", m, readFile(path))
		}
		if got := claudeStandInAt(here(""), path, "claude-fable-5-1"); got != m {
			t.Fatalf("on %s claude-fable-5-1 stands in as %q", m, got)
		}
	}
	// the reporter's settings.json, the fable tier on Sol, from 0.1.1229:
	// Follow takes it out. 0.1.1229 wrote it there following the main
	// model on Sol; a fable tier on Sol before the main model moved onto
	// it is one given Sol, which stays (#2257,
	// TestClaudeOwnTierOnTheNewMainModelStays)
	if err := a.Field("model").Set("cx/gpt-6.1-sol"); err != nil {
		t.Fatal(err)
	}
	if err := edit.SetJSON(path, edit.KV{Path: "env.ANTHROPIC_DEFAULT_FABLE_MODEL", Value: "cx/gpt-6.1-sol"}); err != nil {
		t.Fatal(err)
	}
	if err := a.Follow(); err != nil {
		t.Fatal(err)
	}
	noFable("Sol's older wiring after Follow")
	if err := a.Field("model").Set("anth/claude-opus-5-5"); err != nil {
		t.Fatal(err)
	}
	noFable("back from another vendor")

	// the user's pick of the main model for fable is theirs: it stays
	if err := a.Field("fable").Set("anth/claude-opus-5-5"); err != nil {
		t.Fatal(err)
	}
	if v, _ := fable(); v != "anth/claude-opus-5-5" || a.Field("fable").Get() != "anth/claude-opus-5-5" {
		t.Fatalf("own fable pick: %q, shown %q", v, a.Field("fable").Get())
	}
	if err := a.Field("model").Set("anth/claude-sonnet-5"); err != nil {
		t.Fatal(err)
	}
	if err := a.Follow(); err != nil {
		t.Fatal(err)
	}
	if v, _ := fable(); v != "anth/claude-opus-5-5" {
		t.Fatalf("own fable pick lost: %q", v)
	}
	if err := a.Field("fable").Set(""); err != nil {
		t.Fatal(err)
	}
	noFable("fable back to following")

	// an effort picked for the fable tier keeps it on the main model at that
	// effort, as the user asked
	if err := a.Field("fable_effort").Set("low"); err != nil {
		t.Fatal(err)
	}
	if v, _ := fable(); v != "anth/claude-sonnet-5:low" {
		t.Fatalf("fable effort: %q", v)
	}
	if err := a.Field("fable_effort").Set(""); err != nil {
		t.Fatal(err)
	}
	noFable("fable effort taken away")

	// one the user wrote themselves while routed, not a model of uni-switch's,
	// stays as written when the main model moves
	if err := edit.SetJSON(path, edit.KV{Path: "env.ANTHROPIC_DEFAULT_FABLE_MODEL", Value: "claude-fable-5-1"}); err != nil {
		t.Fatal(err)
	}
	if err := a.Field("model").Set("anth/claude-opus-5-5"); err != nil {
		t.Fatal(err)
	}
	if v, _ := fable(); v != "claude-fable-5-1" {
		t.Fatalf("the user's own fable written over: %q", v)
	}
	if err := edit.DelJSON(path, "env.ANTHROPIC_DEFAULT_FABLE_MODEL"); err != nil {
		t.Fatal(err)
	}

	// an older uni-switch wrote fable on the main model: Follow takes it out
	if err := edit.SetJSON(path, edit.KV{Path: "env.ANTHROPIC_DEFAULT_FABLE_MODEL", Value: "anth/claude-opus-5-5"}); err != nil {
		t.Fatal(err)
	}
	if err := a.Follow(); err != nil {
		t.Fatal(err)
	}
	noFable("older wiring after Follow")

	// uni-switch steps out: the user's own fable model is back
	if err := a.Field("model").Set(""); err != nil {
		t.Fatal(err)
	}
	if v, _ := fable(); v != "my-fable" {
		t.Fatalf("the user's own fable not given back: %q\n%s", v, readFile(path))
	}
}

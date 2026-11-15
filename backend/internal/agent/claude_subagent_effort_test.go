package agent

import (
	"path/filepath"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/catalog"
	"github.com/dieqiyun/uni-switch/backend/internal/edit"
	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// A subagent effort with no subagent model ("Claude Code picks") writes no
// CLAUDE_CODE_SUBAGENT_MODEL: the main model at that effort there pinned
// every subagent, an Explore asking for haiku too, to the main model
// (KorenKrita on Discord). The effort is kept, not applied, and goes with a
// model the subagents are given later; a model and effort together are
// written as before. One an older uni-switch wrote is taken out by Sync.
func TestClaudeSubagentEffortNeedsAModel(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
	if err := provider.Save(provider.Provider{ID: "v", Name: "V", Chat: "https://example.test/v1", Key: "k",
		Models: []string{"glm", "flash"}}); err != nil {
		t.Fatal(err)
	}
	if err := catalog.SaveLive("v", "https://example.test/v1", []catalog.Model{
		{ID: "glm", Context: 200000, Efforts: []string{"low", "medium", "high", "max"}},
		{ID: "flash", Context: 128000, Efforts: []string{"low", "high"}},
	}); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(home, ".claude", "settings.json")
	writeFile(t, path, `{}`)
	a := claude(home)
	sub := func() (string, bool) { return edit.GetJSON(path, "env.CLAUDE_CODE_SUBAGENT_MODEL") }
	set := func(key, v string) {
		t.Helper()
		if err := a.Field(key).Set(v); err != nil {
			t.Fatalf("%s=%s: %v", key, v, err)
		}
	}
	set("model", "v/glm")

	// no model of their own: the effort writes nothing
	set("subagent_effort", "high")
	if v, has := sub(); has {
		t.Fatalf("an effort with no subagent model wrote CLAUDE_CODE_SUBAGENT_MODEL=%q", v)
	}
	if a.Field("subagent").Get() != "" || a.Field("subagent_effort").Get() != "" {
		t.Fatalf("read back: %v", a.Values())
	}
	// nor does a new main model
	set("model", "v/flash")
	if v, has := sub(); has {
		t.Fatalf("a new main model wrote CLAUDE_CODE_SUBAGENT_MODEL=%q", v)
	}

	// a model of their own takes the effort kept
	set("subagent", "v/glm")
	if v, _ := sub(); v != "v/glm:high" || a.Field("subagent_effort").Get() != "high" {
		t.Fatalf("subagents on glm with the effort kept: %q %v", v, a.Values())
	}
	// a model and an effort set together, as before
	set("subagent_effort", "low")
	if v, _ := sub(); v != "v/glm:low" || a.Field("subagent").Get() != "v/glm" || a.Field("subagent_effort").Get() != "low" {
		t.Fatalf("subagents on glm at low: %q %v", v, a.Values())
	}
	// back to Claude Code's pick: the variable goes, the effort is kept
	set("subagent", "")
	if v, has := sub(); has {
		t.Fatalf("subagents back to Claude Code's pick left CLAUDE_CODE_SUBAGENT_MODEL=%q", v)
	}
	set("subagent", "v/flash")
	if v, _ := sub(); v != "v/flash:low" {
		t.Fatalf("subagents on flash with the effort kept: %q", v)
	}
	set("subagent", "")

	// the main model at an effort, as an older uni-switch wrote it: Sync takes
	// it out and keeps the effort for a later model
	set("subagent_effort", "")
	if err := edit.SetJSON(path, edit.KV{Path: "env.CLAUDE_CODE_SUBAGENT_MODEL", Value: "v/flash:high"}); err != nil {
		t.Fatal(err)
	}
	if err := a.Sync(); err != nil {
		t.Fatal(err)
	}
	if v, has := sub(); has {
		t.Fatalf("Sync left an older uni-switch's CLAUDE_CODE_SUBAGENT_MODEL=%q", v)
	}
	set("subagent", "v/glm")
	if v, _ := sub(); v != "v/glm:high" {
		t.Fatalf("the older uni-switch's effort wasn't kept: %q", v)
	}
}

package agent

import (
	"path/filepath"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/edit"
	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// Default picked for Claude Code on uni-switch takes out what uni-switch wrote, its
// tiers and subagent model too: uni-switch's own doing, not a change from
// outside, so no drift is left behind to keep the row up among the
// connected ones while it says Not connected (EZN7L2C3, #834).
func TestClaudeDefaultLeavesNoDrift(t *testing.T) {
	home, _ := codexHome(t, "", "")
	if err := provider.Save(provider.Provider{ID: "aaa", Name: "AAA", Chat: "https://a.example/v1", Key: "k", Models: []string{"first", "claude-opus-5-5"}}); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(home, ".claude", "settings.json")
	writeFile(t, path, `{"model": "opus"}`)
	c := claude(home)
	if err := c.Connect(); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"opus", "haiku", "subagent"} {
		if err := c.Apply(k, "aaa/first"); err != nil {
			t.Fatal(err)
		}
	}
	if err := c.Apply("model", ""); err != nil {
		t.Fatal(err)
	}
	if c.Wired() {
		t.Fatalf("still connected:\n%s", readFile(path))
	}
	if d := c.Drift(); d != nil {
		t.Fatalf("drift left: %+v", *d)
	}

	// a tier changed outside uni-switch before is still said to be
	home2, _ := codexHome(t, "", "")
	if err := provider.Save(provider.Provider{ID: "aaa", Name: "AAA", Chat: "https://a.example/v1", Key: "k", Models: []string{"first", "claude-opus-5-5"}}); err != nil {
		t.Fatal(err)
	}
	path2 := filepath.Join(home2, ".claude", "settings.json")
	writeFile(t, path2, `{"model": "opus"}`)
	c2 := claude(home2)
	if err := c2.Connect(); err != nil {
		t.Fatal(err)
	}
	if err := c2.Apply("opus", "aaa/first"); err != nil {
		t.Fatal(err)
	}
	if err := edit.SetJSON(path2, edit.KV{Path: "env.ANTHROPIC_DEFAULT_OPUS_MODEL", Value: "claude-opus-5-5"}); err != nil {
		t.Fatal(err)
	}
	if err := c2.Apply("effort", "high"); err != nil {
		t.Fatal(err)
	}
	if d := c2.Drift(); d == nil || d.Kind != "replaced" || d.Field != "opus" {
		t.Fatalf("a change from outside isn't said: %+v", d)
	}
}

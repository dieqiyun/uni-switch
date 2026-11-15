package gui

import (
	"os"
	"slices"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/agent"
	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// Kylin_Cloud on X: models left out of an agent's own list on the Agents
// page looked ticked in the provider's editor, as if the two were one
// setting. They are two (the picks are every agent's), so the editor is
// told, by agent, which of the provider's models each list leaves out.
// A WSL copy of the agent shares its list and is told once.
func TestProviderInfoSaysAnAgentsHiddenModels(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("USERPROFILE", os.Getenv("HOME"))
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	p := provider.Provider{ID: "relay", Name: "Relay", Chat: "http://127.0.0.1:1/v1", Key: "key", Models: []string{"a", "b", "c"}}
	if err := provider.Save(p); err != nil {
		t.Fatal(err)
	}
	got, err := provider.Find("relay")
	if err != nil {
		t.Fatal(err)
	}
	codex := &agent.Agent{ID: "codex", Name: "Codex", Bin: "codex"}
	wsl := &agent.Agent{ID: "codex@wsl:Ubuntu", Name: "Codex (WSL)", Bin: "codex"}
	claude := &agent.Agent{ID: "claude", Name: "Claude Code", Bin: "claude"}
	agents := []*agent.Agent{codex, wsl, claude}
	hides := func() map[string][]string {
		t.Helper()
		out := map[string][]string{}
		for _, a := range providerInfo(*got, agentUses(agents)).Agents {
			if a.Hides != nil {
				out[a.ID] = a.Hides
			}
		}
		return out
	}
	if h := hides(); len(h) != 0 {
		t.Fatalf("nothing is hidden yet: %v", h)
	}
	if err := provider.SetHiddenModels("codex", []string{"relay/b", "other/b"}); err != nil {
		t.Fatal(err)
	}
	if h := hides(); len(h) != 1 || !slices.Equal(h["codex"], []string{"b"}) {
		t.Fatalf("Codex leaves out relay's b, told once: %v", h)
	}
	if err := provider.SetOnlyPicked("claude", true); err != nil {
		t.Fatal(err)
	}
	if err := provider.SetPickedModels("claude", []string{"relay/a"}); err != nil {
		t.Fatal(err)
	}
	if h := hides(); !slices.Equal(slices.Sorted(slices.Values(h["claude"])), []string{"b", "c"}) {
		t.Fatalf("Claude Code's list picks only a: %v", h)
	}
}

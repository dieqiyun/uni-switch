package agent

import (
	"path/filepath"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// A catalog model with a fast mode says, in an agent's picker, which agent
// it is switched fast for and whether it is now (#954); a GPT model on a
// provider the user added by its address has one too (#1615), and a model
// without (a relay's GLM) says nothing.
func TestFastPickOptions(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
	for _, p := range []provider.Provider{
		{ID: "oa", Name: "OpenAI", Key: "k", Responses: "https://api.openai.com/v1", Models: []string{"gpt-6.1-sol"}},
		{ID: "rl", Name: "Relay", Key: "k", Chat: "https://relay.example/v1", Models: []string{"gpt-6.1-sol", "glm-5"}},
	} {
		if err := provider.Save(p); err != nil {
			t.Fatal(err)
		}
	}
	if err := provider.SetFastPick("codex", "oa/gpt-6.1-sol", true); err != nil {
		t.Fatal(err)
	}
	of := func(agent string) map[string]Option {
		out := map[string]Option{}
		for _, o := range viaUniSwitch(agent, "uni-switch/") {
			out[o.Ref] = o
		}
		return out
	}
	codex, other := of("codex"), of("opencode")
	if o := codex["oa/gpt-6.1-sol"]; o.FastFor != "codex" || !o.Fast {
		t.Fatalf("codex's fast pick: %+v", o)
	}
	if o := other["oa/gpt-6.1-sol"]; o.FastFor != "opencode" || o.Fast {
		t.Fatalf("another agent's: %+v", o)
	}
	if o := codex["oa/gpt-6.1-sol"]; o.OwnFast {
		t.Fatalf("OpenAI's own fast mode said to be the provider's: %+v", o)
	}
	if o, ok := codex["rl/gpt-6.1-sol"]; !ok || o.FastFor != "codex" || o.Fast || !o.OwnFast {
		t.Fatalf("a relay's GPT model: %+v %v", o, ok)
	}
	if o, ok := codex["rl/glm-5"]; !ok || o.FastFor != "" || o.Fast {
		t.Fatalf("a relay's model with no fast mode: %+v %v", o, ok)
	}
}

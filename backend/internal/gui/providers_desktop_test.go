package gui

import (
	"os"
	"strings"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/agent"
	"github.com/dieqiyun/uni-switch/backend/internal/gateway"
	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// The number Claude Desktop's menu says beside a Claude model it names
// after its own catalog (#2578) is said on the Providers page too, on the
// model's own chip, so a menu row there can be matched to its provider:
// the number in the id Desktop is shown (gateway.DesktopID). No other
// model carries one, nor one Desktop isn't shown, nor any model when
// Claude Desktop isn't installed.
func TestProviderModelsSayDesktopNumber(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("USERPROFILE", os.Getenv("HOME"))
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	p := provider.Provider{ID: "relay", Name: "Relay", Key: "k", Chat: "http://127.0.0.1:1/v1",
		Models: []string{"claude-opus-5-5", "claude-sonnet-5-5", "m2", "claude-haiku-5"}}
	if err := provider.Save(p); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"relay/claude-opus-5-5", "relay/m2", "relay/claude-haiku-5"} {
		if err := provider.SetModelEfforts(id, []string{"low", "high"}); err != nil {
			t.Fatal(err)
		}
	}
	if err := provider.SetHiddenModels("claude-desktop", []string{"relay/claude-haiku-5"}); err != nil {
		t.Fatal(err)
	}
	desktop, err := agent.Find("claude-desktop")
	if err != nil {
		t.Fatal(err)
	}
	said := func(uses []agentUse) map[string]string {
		out := map[string]string{}
		for _, m := range providerInfo(p, uses).Models {
			out[m.ID] = m.Desktop
		}
		return out
	}
	// the number in the id Claude Desktop is shown the Opus model by
	var opus string
	for _, e := range provider.Catalog() {
		if e.ID == "relay/claude-opus-5-5" {
			id := gateway.DesktopID(e)
			rest, cut := strings.CutPrefix(id, "uni-switch-")
			n, _, ok := strings.Cut(rest, ".anthropic.claude-opus-5-5")
			if !cut || !ok || n == "" {
				t.Fatalf("Desktop is shown %s, want uni-switch-<n>.anthropic.claude-opus-5-5", id)
			}
			opus = n
		}
	}
	got := said([]agentUse{{Agent: desktop}})
	want := map[string]string{
		"claude-opus-5-5": opus,
		// its own id: Desktop names it "Sonnet 5.5", with no number
		"claude-sonnet-5-5": "",
		// mythos-uni-switch-<n>: Desktop names it by display_name, which
		// says the provider
		"m2": "",
		// not shown to Claude Desktop
		"claude-haiku-5": "",
	}
	for id, w := range want {
		if got[id] != w {
			t.Errorf("%s: the page says desktop %q, want %q", id, got[id], w)
		}
	}
	// Claude Desktop not installed, another agent is: no chip carries a
	// number
	codex, err := agent.Find("codex")
	if err != nil {
		t.Fatal(err)
	}
	for id, n := range said([]agentUse{{Agent: codex}}) {
		if n != "" {
			t.Errorf("without Claude Desktop, %s says desktop %q", id, n)
		}
	}
}

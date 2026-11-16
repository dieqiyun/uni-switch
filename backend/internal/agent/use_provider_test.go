package agent

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/edit"
	"github.com/dieqiyun/uni-switch/backend/internal/gateway"
	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

func TestUseProviderFailedWriteRestoresPreviousSelection(t *testing.T) {
	home, read := codexHome(t, "", "")
	cx := codex(home)
	if err := cx.UseProvider("fake", "fake/m1"); err != nil {
		t.Fatal(err)
	}
	if err := provider.Save(provider.Provider{ID: "other", Name: "Other", Key: "synthetic", Chat: "http://127.0.0.1:1/v1", Models: []string{"m2"}}); err != nil {
		t.Fatal(err)
	}
	before := read()
	sync := cx.Sync
	cx.Sync = func() error { cx.Sync = sync; return errors.New("synthetic write failure") }
	if err := cx.UseProvider("other", "other/m2"); err == nil {
		t.Fatal("failed apply succeeded")
	}
	if provider.AgentProvider("codex") != "fake" || cx.Field("model").Get() != "fake/m1" || !strings.Contains(read(), "requires_openai_auth = false") {
		t.Fatalf("rollback lost selection:\nbefore %s\nafter %s", before, read())
	}
}

func TestUseProviderSwitchesCatalogAndCanSwitchAgain(t *testing.T) {
	home, read := codexHome(t, "", "")
	if err := provider.Save(provider.Provider{ID: "other", Name: "Other", Key: "synthetic", Chat: "http://127.0.0.1:1/v1", Models: []string{"m2"}}); err != nil {
		t.Fatal(err)
	}
	cx := codex(home)
	for _, pair := range [][2]string{{"fake", "fake/m1"}, {"other", "other/m2"}, {"fake", "fake/m1"}} {
		if err := cx.UseProvider(pair[0], pair[1]); err != nil {
			t.Fatal(err)
		}
		if provider.AgentProvider("codex") != pair[0] || !strings.Contains(read(), `model = "`+pair[1]+`"`) {
			t.Fatal(read())
		}
		shown, _ := provider.CatalogFor("codex")
		for _, e := range shown {
			if e.Provider.ID != pair[0] {
				t.Fatalf("other provider in client catalog: %s", e.ID)
			}
		}
		b, err := os.ReadFile(filepath.Join(home, ".codex", "uni-switch-models.json"))
		if err != nil || !strings.Contains(string(b), pair[1]) {
			t.Fatalf("model list %s: %v", b, err)
		}
	}
	before := read()
	if err := cx.UseProvider("other", "fake/m1"); err == nil || before != read() || provider.AgentProvider("codex") != "fake" {
		t.Fatal("invalid choice changed the client")
	}
}

func TestUseProviderConfiguresClaudeCLIAndDesktop(t *testing.T) {
	home, p := desktopSandbox(t)
	for _, id := range []string{"a", "b"} {
		if err := provider.Save(provider.Provider{ID: id, Name: id, Key: "synthetic", Chat: "http://127.0.0.1:1/v1", Models: []string{"m1"}}); err != nil {
			t.Fatal(err)
		}
	}
	cli := claude(home)
	for _, id := range []string{"a", "b"} {
		if err := cli.UseProvider(id, id+"/m1"); err != nil {
			t.Fatal(err)
		}
		if !cli.Wired() || !claudeDesktop(home).Wired() {
			t.Fatal("CLI or Desktop not connected")
		}
		if got := cli.Field("model").Get(); got != id+"/m1" {
			t.Fatal(got)
		}
		for _, target := range []string{"claude", "claude-desktop"} {
			if provider.AgentProvider(target) != id {
				t.Fatal(target + " did not select " + id)
			}
		}
		for _, tier := range gateway.DesktopTierNames {
			if gateway.DesktopTiers()[tier] != id+"/m1" {
				t.Fatalf("%s: %v", tier, gateway.DesktopTiers())
			}
		}
		if _, ok := edit.GetJSON(p.prof, "inferenceGatewayBaseUrl"); !ok {
			t.Fatal("no native Desktop gateway profile")
		}
		raw, _ := edit.GetJSONRaw(p.prof, "inferenceModels")
		if raw == "" {
			t.Fatal("Desktop has no inference models")
		}
		shown, _ := provider.CatalogFor("claude-desktop")
		for _, e := range shown {
			if e.Provider.ID != id {
				t.Fatalf("Desktop lists %s", e.ID)
			}
		}
	}
}

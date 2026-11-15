package agent

import (
	"os"
	"runtime"
	"strings"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/edit"
	"github.com/dieqiyun/uni-switch/backend/internal/gateway"
	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// The Codex app on Windows with its agent in WSL reads Windows'
// config.toml from WSL (cjyrainbow, #816): a C:\ catalog path is no file
// there, and 127.0.0.1 is the distro's own. The user names the catalog
// relative to config.toml and the gateway by Windows' address as WSL sees
// it; uni-switch took that for a config changed away from it ("Codex 已不经过
// uni-switch") and Reconnect wrote the two back. Now it is connected so, and
// a model picked again, a sync and a reconnect keep both.
func TestCodexWSLAppAddressKept(t *testing.T) {
	home, read := codexHome(t, `{"OPENAI_API_KEY":"sk-x"}`, "")
	if err := provider.Save(provider.Provider{ID: "fake", Name: "Fake", Key: "k", Chat: "http://127.0.0.1:1/v1", Models: []string{"m1", "m2"}}); err != nil {
		t.Fatal(err)
	}
	cx := codex(home)
	if err := cx.Fields[0].Set("fake/m1"); err != nil {
		t.Fatal(err)
	}
	// on Windows uni-switch names the catalog as the app's WSL agent can read it
	if c, _ := edit.GetTOMLTop(cx.Path, "model_catalog_json"); runtime.GOOS == "windows" && c != "uni-switch-models.json" {
		t.Fatalf("catalog named %q", c)
	}
	if c := cx.Check(); c != "" {
		t.Fatal(c)
	}

	// the user's edit: Windows as WSL reaches it, the catalog by its name
	wsl := "http://172.28.96.1:" + gateway.Port()
	if err := edit.SetTOMLTop(cx.Path, edit.KV{Path: "model_catalog_json", Value: "uni-switch-models.json"}, edit.KV{Path: "openai_base_url", Value: wsl + gateway.CodexPath}); err != nil {
		t.Fatal(err)
	}
	if err := edit.SetTOMLKey(cx.Path, "model_providers."+uniSwitchID, "base_url", wsl+"/v1"); err != nil {
		t.Fatal(err)
	}
	cx = codex(home)
	if c := cx.Check(); c != "" {
		t.Fatalf("the user's working config is taken for a broken one: %s", c)
	}
	if !cx.Routed() {
		t.Fatal("not routed")
	}
	keep := func(when string) {
		t.Helper()
		cfg := read()
		if !strings.Contains(cfg, `openai_base_url = "`+wsl+gateway.CodexPath+`"`) || !strings.Contains(cfg, `base_url = "`+wsl+`/v1"`) ||
			strings.Contains(cfg, "127.0.0.1:"+gateway.Port()) {
			t.Fatalf("%s: the address was not kept\n%s", when, cfg)
		}
		if c := codex(home).Check(); c != "" {
			t.Fatalf("%s: %s", when, c)
		}
	}
	if err := cx.Fields[0].Set("fake/m2"); err != nil {
		t.Fatal(err)
	}
	keep("another model")
	if err := cx.Sync(); err != nil {
		t.Fatal(err)
	}
	keep("sync")
	if _, err := os.Stat(home + "/.codex/uni-switch-models.json"); err != nil {
		t.Fatal(err)
	}

	// taken out, the address goes with uni-switch
	if err := cx.Unwire(); err != nil {
		t.Fatal(err)
	}
	if cfg := read(); strings.Contains(cfg, "openai_base_url") || strings.Contains(cfg, "model_catalog_json") || strings.Contains(cfg, `model_provider = "uni-switch"`) {
		t.Fatalf("unwired:\n%s", cfg)
	}
}

// Another server's address is not taken for uni-switch's: a relay's base URL
// is not the Codex path, and uni-switch's own address stays 127.0.0.1.
func TestCodexKeptGatewayOnlyUniSwitchs(t *testing.T) {
	home, _ := codexHome(t, "", "openai_base_url = \"https://relay.example/v1\"\n")
	if got := codexKeptGateway(home + "/.codex/config.toml"); got != gateway.URL() {
		t.Fatalf("kept %q", got)
	}
}

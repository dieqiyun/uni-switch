package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// Signed in with an API key — a relay's, with the relay's own provider
// table — Codex never asks for the model list, so a base URL alone left its
// picker with the models it was built with and the one set. uni-switch is its
// provider instead, with every uni-switch model in the catalog, and stepping
// back to Codex's own model brings the relay's table back (#322).
func TestCodexAPIKeyUsesProvider(t *testing.T) {
	home, read := codexHome(t, `{"OPENAI_API_KEY":"sk-relay"}`,
		"model = \"gpt-5.5\"\nmodel_provider = \"relay\"\n\n[model_providers.relay]\nname = \"relay\"\nbase_url = \"https://relay.example/v1\"\nwire_api = \"responses\"\nrequires_openai_auth = true\n")
	if err := provider.Save(provider.Provider{ID: "fake", Name: "Fake", Key: "k", Chat: "http://127.0.0.1:1/v1", Models: []string{"m1", "m2"}}); err != nil {
		t.Fatal(err)
	}
	cx := codex(home)
	if err := cx.Fields[0].Set("fake/m1"); err != nil {
		t.Fatal(err)
	}
	cfg := read()
	if !strings.Contains(cfg, `model_provider = "uni-switch"`) || !strings.Contains(cfg, "model_catalog_json") ||
		!strings.Contains(cfg, `openai_base_url = "`+codexGatewayURL()+`"`) || !strings.Contains(cfg, "[model_providers.relay]") {
		t.Fatalf("\n%s", cfg)
	}
	b, err := os.ReadFile(filepath.Join(home, ".codex", "uni-switch-models.json"))
	if err != nil || !strings.Contains(string(b), `"fake/m1"`) || !strings.Contains(string(b), `"fake/m2"`) {
		t.Fatalf("catalog: %v %s", err, b)
	}
	if c := cx.Check(); c != "" {
		t.Fatal(c)
	}
	if err := cx.Fields[0].Set("gpt-5.4"); err != nil {
		t.Fatal(err)
	}
	if cfg = read(); !strings.Contains(cfg, `model_provider = "relay"`) || strings.Contains(cfg, "model_catalog_json") ||
		strings.Contains(cfg, "openai_base_url") || !strings.Contains(cfg, `model = "gpt-5.4"`) {
		t.Fatalf("back:\n%s", cfg)
	}
}

// A Codex an older uni-switch routed by the base URL alone while it was signed
// in with an API key moves to uni-switch as its provider on the next sync.
func TestCodexAPIKeyBaseURLMoved(t *testing.T) {
	home, read := codexHome(t, `{"OPENAI_API_KEY":"sk-relay"}`,
		"model = \"fake/m1\"\nopenai_base_url = \""+codexGatewayURL()+"\"\n")
	cx := codex(home)
	if err := cx.Sync(); err != nil {
		t.Fatal(err)
	}
	if cfg := read(); !strings.Contains(cfg, `model_provider = "uni-switch"`) || !strings.Contains(cfg, "model_catalog_json") ||
		!strings.Contains(cfg, `model = "fake/m1"`) {
		t.Fatalf("\n%s", cfg)
	}
	if c := cx.Check(); c != "" {
		t.Fatal(c)
	}
}

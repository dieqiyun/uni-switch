package agent

import (
	"strings"
	"testing"
)

// A Codex of the user's own provider (CC Switch's custom, an older
// takeover's): model_provider at the top and its table. Picking a uni-switch
// model beside the ChatGPT sign-in took the line down silently, and every
// thread saved with the user's provider stopped resuming
// (modelProviderMismatch, #4347). uni-switch goes by the provider instead,
// the user's spelling kept in the stash, and stepping out puts it back.
func TestCodexTakeoverKeepsUserProvider(t *testing.T) {
	home, read := codexHome(t, chatGPTAuth,
		"model = \"gpt-5.5\"\nmodel_provider = \"custom\"\n\n[model_providers.custom]\nname = \"Custom\"\nbase_url = \"http://127.0.0.1:9/v1\"\nwire_api = \"responses\"\n")
	cx := codex(home)
	if err := cx.Fields[0].Set("fake/m1"); err != nil {
		t.Fatal(err)
	}
	cfg := read()
	if !strings.Contains(cfg, `model_provider = "uni-switch"`) || !strings.Contains(cfg, `model = "fake/m1"`) ||
		!strings.Contains(cfg, "[model_providers.custom]") || !strings.Contains(cfg, "[model_providers.uni-switch]") {
		t.Fatalf("a uni-switch model over the user's provider:\n%s", cfg)
	}
	// and stepping out puts the user's provider back
	if err := cx.Disconnect(); err != nil {
		t.Fatal(err)
	}
	if cfg = read(); !strings.Contains(cfg, `model_provider = "custom"`) || strings.Contains(cfg, "openai_base_url") {
		t.Fatalf("disconnected, the user's provider not put back:\n%s", cfg)
	}
}

// A reset takes uni-switch's keys out, not the user's: a provider of the
// user's at the top stays, and Codex goes on resuming the threads it
// saved with (#4347).
func TestCodexResetKeepsUserProvider(t *testing.T) {
	home, read := codexHome(t, "",
		"model = \"fake/m1\"\nmodel_provider = \"custom\"\nmodel_catalog_json = \"/x/list.json\"\n\n[model_providers.custom]\nname = \"Custom\"\nbase_url = \"http://127.0.0.1:9/v1\"\nwire_api = \"responses\"\n")
	cx := codex(home)
	if err := cx.Fields[0].Set(""); err != nil {
		t.Fatal(err)
	}
	cfg := read()
	if !strings.Contains(cfg, `model_provider = "custom"`) || !strings.Contains(cfg, "[model_providers.custom]") ||
		strings.Contains(cfg, "model_catalog_json") || strings.Contains(cfg, `[model = "fake/m1"]`) {
		t.Fatalf("reset over the user's provider:\n%s", cfg)
	}
	_ = home
}

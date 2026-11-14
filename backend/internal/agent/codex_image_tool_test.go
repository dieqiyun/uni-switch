package agent

import (
	"strings"
	"testing"
)

// Keep the gateway's actor header while allowing API-key-only clients to
// reach it. Upstream tool access still depends on the selected provider.
func TestCodexUniSwitchProviderCarriesActorAuthorization(t *testing.T) {
	home, read := codexHome(t, `{"OPENAI_API_KEY":"sk-relay"}`,
		"model = \"gpt-5.5\"\nmodel_provider = \"relay\"\n\n[model_providers.relay]\nname = \"relay\"\nbase_url = \"https://relay.example/v1\"\nwire_api = \"responses\"\n")
	cx := codex(home)
	if err := cx.Fields[0].Set("fake/m1"); err != nil {
		t.Fatal(err)
	}
	cfg := read()
	if !strings.Contains(cfg, "[model_providers.uni-switch]") {
		t.Fatalf("no uni-switch provider table:\n%s", cfg)
	}
	if !strings.Contains(cfg, `"x-openai-actor-authorization" = "uni-switch"`) {
		t.Fatalf("uni-switch provider table carries no actor-authorization header:\n%s", cfg)
	}
	if !strings.Contains(cfg, "requires_openai_auth = false") {
		t.Fatalf("API-key-only client must reach the gateway without ChatGPT login:\n%s", cfg)
	}
	if c := cx.Check(); c != "" {
		t.Fatal(c)
	}
}

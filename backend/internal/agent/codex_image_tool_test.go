package agent

import (
	"strings"
	"testing"
)

// Keep the actor header alongside the required OpenAI auth flag. The header
// alone no longer grants the image tool to an API-key-only setup.
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
	if !strings.Contains(cfg, "requires_openai_auth = true") {
		t.Fatalf("uni-switch provider table must require OpenAI auth:\n%s", cfg)
	}
	if c := cx.Check(); c != "" {
		t.Fatal(c)
	}
}

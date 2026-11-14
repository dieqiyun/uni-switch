package agent

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/edit"
)

// Uninstalling takes uni-switch's dead addresses out of Codex's config even
// where no Disconnect could name them (#3866): the marks uni-switch keeps its
// writes by are gone (uni-switch's folder deleted by hand, a fresh install
// beside an old config, a uni-switch from before the marks), so Codex reads as
// not connected, and the base URL at uni-switch's gateway sends every turn to
// a port nothing answers any more. Codex's own model and provider stay,
// and so does uni-switch's provider table, whose threads name it.
func TestCodexLeftoversGoAtUninstallWithoutTheMarks(t *testing.T) {
	home, read := codexHome(t, `{"tokens":{"access_token":"x","id_token":"x.e30.x"}}`,
		"model = \"gpt-5.4\"\nopenai_base_url = \""+codexGatewayURL()+"\"\n\n"+
			"[model_providers.uni-switch]\nname = \"uni-switch\"\nbase_url = \""+codexGatewayURL()+"\"\nwire_api = \"responses\"\n")
	cx := codex(home)
	if cx.Wired() {
		t.Fatalf("read as connected with no marks of uni-switch's:\n%s", read())
	}
	took, err := TakeCodexLeftovers()
	if err != nil || !took {
		t.Fatalf("took %v, err %v", took, err)
	}
	cfg := read()
	if strings.Contains(cfg, "openai_base_url") {
		t.Fatalf("uni-switch's base URL left for a gateway that is gone:\n%s", cfg)
	}
	if !strings.Contains(cfg, `model = "gpt-5.4"`) || strings.Contains(cfg, "model_provider =") || strings.Contains(cfg, "model_catalog_json") {
		t.Fatalf("Codex's own pick disturbed:\n%s", cfg)
	}
	if !strings.Contains(cfg, "[model_providers.uni-switch]") {
		t.Fatal("uni-switch's provider table taken: threads started on it name it")
	}
	// nothing of uni-switch's the second time
	if took, err := TakeCodexLeftovers(); err != nil || took {
		t.Fatalf("took again: %v, %v", took, err)
	}
}

// uni-switch its provider with the marks gone: the pick and the name of
// uni-switch's model list go with it, or Codex is left on a provider of a dead
// gateway (and loading no config at all once its list file is gone).
func TestCodexProviderLeftoversGoAtUninstallWithoutTheMarks(t *testing.T) {
	home, read := codexHome(t, "",
		"model = \"gpt-5.4\"\nmodel_provider = \"uni-switch\"\nopenai_base_url = \""+codexGatewayURL()+"\"\n\n"+
			"[model_providers.uni-switch]\nname = \"uni-switch\"\nbase_url = \""+codexGatewayURL()+"\"\nwire_api = \"responses\"\n\n[projects.\"/x\"]\ntrust_level = \"trusted\"\n")
	path := filepath.Join(home, ".codex", "config.toml")
	// the list uni-switch wrote for its provider, named as Codex reads it
	if err := edit.SetTOMLTop(path, edit.KV{Path: "model_catalog_json", Value: filepath.Join(home, ".codex", "uni-switch-models.json")}); err != nil {
		t.Fatal(err)
	}
	cx := codex(home)
	if cx.Wired() {
		t.Fatalf("read as connected with no marks of uni-switch's:\n%s", read())
	}
	if took, err := TakeCodexLeftovers(); err != nil || !took {
		t.Fatalf("took %v, err %v", took, err)
	}
	cfg := read()
	if strings.Contains(cfg, "openai_base_url") || strings.Contains(cfg, `model_provider = "uni-switch"`) || strings.Contains(cfg, "model_catalog_json") {
		t.Fatalf("uni-switch's provider pick left:\n%s", cfg)
	}
	if !strings.Contains(cfg, `model = "gpt-5.4"`) || !strings.Contains(cfg, `trust_level = "trusted"`) {
		t.Fatalf("Codex's own keys disturbed:\n%s", cfg)
	}
}

// The user's own addresses stay, and nothing of theirs is taken.
func TestCodexLeftoversLeaveTheUsersOwn(t *testing.T) {
	const config = "model = \"gpt-5.4\"\nopenai_base_url = \"https://api.openai.com/v1\"\nmodel_provider = \"mine\"\n\n" +
		"[model_providers.mine]\nname = \"mine\"\nbase_url = \"https://mine.example/v1\"\n"
	_, read := codexHome(t, "", config)
	if took, err := TakeCodexLeftovers(); err != nil || took {
		t.Fatalf("took %v, err %v", took, err)
	}
	if cfg := read(); cfg != config {
		t.Fatalf("the user's own config changed:\n%s", cfg)
	}
}

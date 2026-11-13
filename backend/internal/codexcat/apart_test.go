package codexcat

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/catalog"
)

// An entry with something that tells it apart (catalog.Model.Apart) says
// it in its display_name alone, whether uni-switch makes the entry, its vendor
// lists it, or it is Codex's own: the description, which Codex puts in
// spawn_agent's list of models upstream, is as it was.
func TestCodexApartInDisplayNameOnly(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	os.MkdirAll(filepath.Join(home, ".codex"), 0o755)
	os.WriteFile(filepath.Join(home, ".codex", "models_cache.json"), []byte(`{"models":[
		{"slug":"gpt-6.1-sol","display_name":"GPT-6.1-Sol","description":"Frontier agentic coding model.","priority":3,"visibility":"list"}]}`), 0o644)
	listed := json.RawMessage(`{"slug":"gpt-6.1-sol","display_name":"GPT-6.1-Sol","description":"Frontier agentic coding model.","base_instructions":"x","supported_reasoning_levels":[],"shell_type":"shell_command","visibility":"list"}`)
	var got struct {
		Models []struct {
			Name string `json:"display_name"`
			Desc string `json:"description"`
		} `json:"models"`
	}
	if err := json.Unmarshal(Catalog([]catalog.Model{
		{ID: "codex/gpt-6.1-sol", Name: "GPT-6.1-Sol · Codex", Apart: "codex"},
		{ID: "codex-b/gpt-6.1-sol", Name: "GPT-6.1-Sol · Codex", Apart: "codex-b", Images: true},
		{ID: "relay/gpt-6.1-sol", Name: "GPT-6.1-Sol · Relay", Apart: "relay", Codex: listed},
		{ID: "or/glm-5.3", Name: "GLM-5.3 · OpenRouter", Images: true},
	}), &got); err != nil || len(got.Models) != 4 {
		t.Fatalf("%v %v", err, got)
	}
	want := [][2]string{
		{"GPT-6.1-Sol · Codex · codex", "Frontier agentic coding model."},
		{"GPT-6.1-Sol · Codex · codex-b", "GPT-6.1-Sol · Codex via uni-switch"},
		{"GPT-6.1-Sol · Relay · relay", "Frontier agentic coding model. via uni-switch"},
		{"GLM-5.3 · OpenRouter", "GLM-5.3 · OpenRouter via uni-switch"},
	}
	for i, m := range got.Models {
		if [2]string{m.Name, m.Desc} != want[i] {
			t.Errorf("entry %d: %q, %q; want %q", i, m.Name, m.Desc, want[i])
		}
	}
}

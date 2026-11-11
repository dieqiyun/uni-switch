package agent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/catalog"
	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// #2064: DeepSeek V4.1 Flash with Max output 384k set on its provider
// reached VS Code as 250000 and CodeBuddy and WorkBuddy as 128000. The
// agents' own trims of a vendor's figure stay for a model the user set
// nothing on; the user's number is handed on as it is.
func TestAgentsTakeTheUsersMaxOutput(t *testing.T) {
	home := syncHome(t)
	os.WriteFile(catalog.CachePath(), []byte(`{"zai":{"models":{"glm-4.6":{"id":"glm-4.6","name":"GLM-4.6","limit":{"context":1000000,"output":384000}}}}}`), 0o644)
	catalog.Reset()

	outputs := func() map[string]any {
		got := map[string]any{}
		vs := vscodeGroupJSON(vscodeStable, vscodeEndpoint)["models"].([]any)
		got["vscode"] = vs[0].(map[string]any)["maxOutputTokens"]
		oai := vscodeGroupJSON(vscodeStable, vscodeOAI)["models"].([]any)[0].(map[string]any)
		got["vscode-oai"] = oai["maxOutputTokens"]
		got["vscode-oai-in"] = oai["maxInputTokens"]
		jb := copilotJBModelsJSON(nil)["relay/glm-4.6"].(map[string]any)["modelCapabilities"].(map[string]any)
		got["copilot-jetbrains"] = jb["maxOutputTokens"]
		for _, id := range []string{"workbuddy", "codebuddy"} {
			path := filepath.Join(home, "."+id, "models.json")
			os.MkdirAll(filepath.Dir(path), 0o755)
			if err := buddyWrite(path, id, true); err != nil {
				t.Fatal(err)
			}
			b, _ := os.ReadFile(path)
			var doc any
			if err := json.Unmarshal(b, &doc); err != nil {
				t.Fatalf("%s: %v %s", id, err, b)
			}
			list, _ := doc.([]any)
			if m, ok := doc.(map[string]any); ok {
				list, _ = m["models"].([]any)
			}
			for _, e := range list {
				if e := e.(map[string]any); e["id"] == "relay/glm-4.6" {
					got[id] = e["maxOutputTokens"]
				}
			}
		}
		// a fresh ZCode: one already written keeps the output it holds
		// (zcodeVariants), as a limit the user may have edited there
		os.RemoveAll(filepath.Join(home, ".zcode"))
		os.MkdirAll(filepath.Join(home, ".zcode", "v2"), 0o755)
		zc := zcode(home)
		if err := zc.Field("provider").Set("uni-switch"); err != nil {
			t.Fatal(err)
		}
		b, _ := os.ReadFile(filepath.Join(home, ".zcode", "v2", "config.json"))
		var c struct {
			Provider map[string]struct {
				Models map[string]struct {
					Limit struct{ Output int } `json:"limit"`
				} `json:"models"`
			} `json:"provider"`
		}
		if err := json.Unmarshal(b, &c); err != nil {
			t.Fatalf("zcode: %v %s", err, b)
		}
		got["zcode"] = c.Provider["uni-switch"].Models["relay/glm-4.6"].Limit.Output
		return got
	}

	// nothing set: each agent's own trim of the vendor's 384000
	got := outputs()
	want := map[string]any{"vscode": 250000, "vscode-oai": 250000, "vscode-oai-in": 750000, "copilot-jetbrains": 250000,
		"workbuddy": float64(128000), "codebuddy": float64(128000), "zcode": 128000}
	for k, w := range want {
		if got[k] != w {
			t.Fatalf("nothing set, %s: %v (%T), want %v; all %v", k, got[k], got[k], w, got)
		}
	}

	// Max output 384k on the provider, as the reporter set it
	if err := provider.SetModelOutputs("relay", map[string]int{"*": 384000}); err != nil {
		t.Fatal(err)
	}
	got = outputs()
	want = map[string]any{"vscode": 384000, "vscode-oai": 384000, "vscode-oai-in": 616000, "copilot-jetbrains": 384000,
		"workbuddy": float64(384000), "codebuddy": float64(384000), "zcode": 384000}
	for k, w := range want {
		if got[k] != w {
			t.Fatalf("384k set, %s: %v (%T), want %v; all %v", k, got[k], got[k], w, got)
		}
	}
}

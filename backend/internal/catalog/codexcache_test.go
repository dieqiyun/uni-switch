package catalog

import "testing"

// A models_cache.json Codex wrote from a list handed through uni-switch has
// uni-switch's models in it; they are not Codex's own.
func TestCodexCacheLeavesUniSwitchOut(t *testing.T) {
	b := []byte(`{"etag":"W/\"abc+uni-switch-0a06185a4db4\"","models":[
		{"slug":"gpt-5.5","display_name":"GPT-5.5","visibility":"list","priority":1},
		{"slug":"group/semantic","display_name":"semantic","description":"semantic via uni-switch","visibility":"list","priority":2},
		{"slug":"group/auto-glm-5-3-flash","display_name":"GLM-5.3-Flash","visibility":"list","priority":3},
		{"slug":"codex/gpt-5.4","display_name":"GPT-5.4","visibility":"list","priority":4},
		{"slug":"deepseek/deepseek-v4","display_name":"DeepSeek V4","description":"DeepSeek V4 via uni-switch","visibility":"list","priority":5}]}`)
	ms, err := parseCodex(b)
	if err != nil {
		t.Fatal(err)
	}
	if len(ms) != 1 || ms[0].ID != "gpt-5.5" {
		t.Fatalf("models = %+v, want gpt-5.5 alone", ms)
	}
	// straight from the backend, a slug is taken as it is
	ms, _ = parseCodex([]byte(`{"etag":"W/\"abc\"","models":[{"slug":"org/m","visibility":"list"}]}`))
	if len(ms) != 1 {
		t.Fatalf("models = %+v, want org/m", ms)
	}
}

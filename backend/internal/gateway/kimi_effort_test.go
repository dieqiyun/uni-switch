package gateway

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/catalog"
	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// #333: the new Kimi Code asks for its thinking level inside thinking
// ({"type":"enabled","effort":…}); it reaches the vendor as reasoning_effort,
// fitted to the model's levels as any other agent's is, and thinking keeps
// the rest. A request with no level, or its own reasoning_effort, goes as it
// was.
func TestKimiCodeEffortReachesChatVendor(t *testing.T) {
	f := &fake{t: t, reply: sse(
		`data: {"id":"c1","model":"glm-5.3-flash","choices":[{"delta":{"role":"assistant","content":"hi"}}]}`,
		`data: {"id":"c1","choices":[{"delta":{},"finish_reason":"stop"}]}`,
		`data: [DONE]`)}
	up := setup(t, provider.Chat, f)
	os.MkdirAll(filepath.Dir(catalog.CachePath()), 0o755)
	os.WriteFile(catalog.CachePath(), []byte(`{"zai":{"models":{"glm-5.3-flash":{"id":"glm-5.3-flash","reasoning_options":[{"type":"effort","values":["low","high","max"]}]}}}}`), 0o644)
	catalog.Reset()
	t.Cleanup(catalog.Reset)
	if err := provider.Save(provider.Provider{ID: "volc", Name: "Volc", Key: "k", Chat: up.URL + "/v1", Models: []string{"glm-5.3-flash"}}); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		fields, effort string
		thinking       map[string]any
	}{
		{`"thinking":{"type":"enabled","effort":"medium","keep":"all"}`, "high", map[string]any{"type": "enabled", "keep": "all"}},
		{`"thinking":{"type":"enabled","effort":"max"}`, "max", map[string]any{"type": "enabled"}},
		{`"thinking":{"type":"enabled","keep":"all"}`, "", map[string]any{"type": "enabled", "keep": "all"}},
		{`"thinking":{"type":"disabled"}`, "", map[string]any{"type": "disabled"}},
		{`"reasoning_effort":"low","thinking":{"type":"enabled","effort":"max"}`, "low", map[string]any{"type": "enabled", "effort": "max"}},
	} {
		code, body := post(t, "/v1/chat/completions", `{"model":"volc/glm-5.3-flash","stream":true,"messages":[{"role":"user","content":"hi"}],`+c.fields+`}`)
		if code != 200 {
			t.Fatalf("status %d: %s", code, body)
		}
		var got struct {
			Effort   string         `json:"reasoning_effort"`
			Thinking map[string]any `json:"thinking"`
		}
		json.Unmarshal(f.got, &got)
		if got.Effort != c.effort || !jsonEqual(got.Thinking, c.thinking) {
			t.Errorf("%s: sent %s", c.fields, f.got)
		}
	}
}

func jsonEqual(a, b any) bool {
	x, _ := json.Marshal(a)
	y, _ := json.Marshal(b)
	return string(x) == string(y)
}

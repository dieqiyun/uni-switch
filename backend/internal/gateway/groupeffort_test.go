package gateway

import (
	"encoding/json"

	"strings"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

func sentBody(t *testing.T, u *ruleUp) map[string]any {
	t.Helper()
	u.mu.Lock()
	defer u.mu.Unlock()
	var v map[string]any
	if err := json.Unmarshal([]byte(u.last), &v); err != nil {
		t.Fatalf("%v: %s", err, u.last)
	}
	return v
}

// withFixedEffort asks for the effort in each API's own words, whether or
// not the request asked for reasoning.
func TestWithFixedEffort(t *testing.T) {
	for _, c := range []struct {
		proto      provider.Protocol
		in, effort string
		want       string
	}{
		{provider.Chat, `{"model":"m"}`, "low", `"reasoning_effort":"low"`},
		{provider.Chat, `{"model":"m","reasoning_effort":"high"}`, "none", `"reasoning_effort":"none"`},
		{provider.Responses, `{"model":"m"}`, "high", `"reasoning":{"effort":"high"}`},
		{provider.Responses, `{"model":"m","reasoning":{"effort":"low","summary":"auto"}}`, "xhigh", `"effort":"xhigh"`},
		{provider.Responses, `{"model":"m","reasoning":{"effort":"low","summary":"auto"}}`, "xhigh", `"summary":"auto"`},
		{provider.Anthropic, `{"model":"m","max_tokens":32000}`, "none", `"thinking":{"type":"disabled"}`},
		{provider.Anthropic, `{"model":"m","max_tokens":32000}`, "high", `"type":"enabled"`},
		{provider.Anthropic, `{"model":"m","max_tokens":32000}`, "high", `"budget_tokens":`},
	} {
		got := string(withFixedEffort(c.proto, []byte(c.in), c.effort))
		if !strings.Contains(got, c.want) {
			t.Errorf("%v %s at %s: %s, want %s", c.proto, c.in, c.effort, got, c.want)
		}
	}
	// no room left to think in: left as it is
	if got := string(withFixedEffort(provider.Anthropic, []byte(`{"model":"m","max_tokens":500}`), "high")); strings.Contains(got, "thinking") {
		t.Errorf("small max_tokens: %s", got)
	}
	if got := string(withFixedEffort(provider.Chat, []byte(`{"model":"m"}`), "")); got != `{"model":"m"}` {
		t.Errorf("no effort: %s", got)
	}
	if fitLevel("xhigh", nil) != "high" || fitLevel("xhigh", []string{"low", "xhigh"}) != "xhigh" || fitLevel("max", []string{"low", "medium", "high"}) != "high" {
		t.Error("fitLevel")
	}
}

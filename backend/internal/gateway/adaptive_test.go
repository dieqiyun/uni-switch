package gateway

import (
	"encoding/json"
	"testing"
)

// A Chat client's max tokens is what the model can write — Pi sends the
// model's own output limit — so the thinking budget fits under it rather
// than max_tokens raised past it, which the upstream refuses with a 400.
// With no cap asked, max_tokens still makes room for the budget.
func TestThinkingBudgetFitsTheClientsCap(t *testing.T) {
	for _, c := range []struct {
		max         int
		effort      string
		wantMax     float64
		wantBudget  float64
		wantNoThink bool
	}{
		{16384, "high", 16384, 15360, false},
		{32000, "high", 32000, 24000, false},
		{8192, "xhigh", 8192, 7168, false},
		{0, "high", 28096, 24000, false},
		{2048, "high", 2048, 0, true},
	} {
		body, _ := json.Marshal(map[string]any{"model": "k", "max_completion_tokens": c.max, "reasoning_effort": c.effort,
			"messages": []map[string]any{{"role": "user", "content": "hi"}}})
		r, err := parseChat(body)
		if err != nil {
			t.Fatal(err)
		}
		var out map[string]any
		json.Unmarshal(buildAnthropic(r, "kimi-k3"), &out)
		if out["max_tokens"] != c.wantMax {
			t.Errorf("%d %s: max_tokens %v, want %v", c.max, c.effort, out["max_tokens"], c.wantMax)
		}
		th, _ := out["thinking"].(map[string]any)
		if c.wantNoThink {
			if th != nil {
				t.Errorf("%d %s: thinking %v with no room for it", c.max, c.effort, th)
			}
			continue
		}
		if th["budget_tokens"] != c.wantBudget {
			t.Errorf("%d %s: budget %v, want %v", c.max, c.effort, th["budget_tokens"], c.wantBudget)
		}
	}
}

func TestAdaptiveThinking(t *testing.T) {
	for model, want := range map[string]bool{
		"claude-opus-5-5": true, "claude-opus-5": true, "claude-sonnet-4-6": true,
		"claude-opus-4-7": true, "anthropic.claude-sonnet-4.6-v1": true, "claude-sonnet-5": true,
		"claude-sonnet-4-5-20250929": false, "claude-sonnet-4-20250514": false, "claude-opus-4-1": false,
		"claude-3-7-sonnet-20250219": false, "deepseek-v4": false, "claude-haiku-4-5": false,
		"claude-opus-5.5": true, "opus-5.5": true, "anthropic/claude-opus-5.5": true, "claude-opus-5-5[1m]": true,
		"claude-opus-5-5-20260901": true, "claude-5-5-opus": true, "claude-3-5-sonnet-20241022": false,
		"claude-opus-4-20250514": false, "claude-opus-4-1-20250805": false, "kimi-k3": false, "gpt-5.5": false,
	} {
		if got := adaptiveOnly(model); got != want {
			t.Errorf("adaptiveOnly(%q) = %v", model, got)
		}
	}
	var out map[string]any
	json.Unmarshal(buildAnthropic(&Request{Thinking: true, Effort: "xhigh"}, "claude-opus-5-5"), &out)
	if th, _ := json.Marshal(out["thinking"]); string(th) != `{"display":"summarized","type":"adaptive"}` {
		t.Errorf("thinking = %s", th)
	}
	if oc, _ := json.Marshal(out["output_config"]); string(oc) != `{"effort":"xhigh"}` {
		t.Errorf("output_config = %s", oc)
	}
	json.Unmarshal(buildAnthropic(&Request{Thinking: true, Effort: "xhigh"}, "claude-opus-4-6"), &out)
	if oc, _ := json.Marshal(out["output_config"]); string(oc) != `{"effort":"max"}` {
		t.Errorf("4.6 output_config = %s", oc)
	}
	json.Unmarshal(buildAnthropic(&Request{Thinking: true, Effort: "low"}, "claude-sonnet-4-5"), &out)
	if th, _ := json.Marshal(out["thinking"]); string(th) != `{"budget_tokens":4096,"type":"enabled"}` {
		t.Errorf("old model thinking = %s", th)
	}
}

// Z.ai's GLM-5.2 and GLM-5.3 take their effort in output_config, as ZCode
// sends it, fitted to their levels; a budget alone leaves them at theirs
func TestGLMEffortInOutputConfig(t *testing.T) {
	for model, want := range map[string]bool{
		"GLM-5.3": true, "glm-5.3-flash": true, "GLM-5.2": true, "zai/glm-5.3": true,
		"GLM-5-Turbo": false, "glm-5.1": false, "glm-5.30": false, "claude-opus-5-5": false,
	} {
		if got := effortInOutputConfig.MatchString(model); got != want {
			t.Errorf("effortInOutputConfig(%q) = %v", model, got)
		}
	}
	var out map[string]any
	json.Unmarshal(buildAnthropic(&Request{Thinking: true, Effort: "max"}, "GLM-5.3"), &out)
	if oc, _ := json.Marshal(out["output_config"]); string(oc) != `{"effort":"max"}` {
		t.Errorf("output_config = %s", oc)
	}
	out = nil
	json.Unmarshal(buildAnthropic(&Request{Thinking: true, Effort: "high"}, "GLM-5-Turbo"), &out)
	if out["output_config"] != nil {
		t.Errorf("GLM-5-Turbo asked output_config: %v", out["output_config"])
	}

	levels := []string{"low", "high", "max"}
	for body, want := range map[string]string{
		// Claude Code's budget for medium, the nearest of GLM-5.3's
		`{"thinking":{"type":"enabled","budget_tokens":10000}}`:                          `{"output_config":{"effort":"high"},"thinking":{"budget_tokens":10000,"type":"enabled"}}`,
		`{"thinking":{"type":"adaptive"},"output_config":{"effort":"low","format":"x"}}`: `{"output_config":{"effort":"low","format":"x"},"thinking":{"type":"adaptive"}}`,
		`{"thinking":{"type":"adaptive"},"output_config":{"effort":"xhigh"}}`:            `{"output_config":{"effort":"max"},"thinking":{"type":"adaptive"}}`,
		`{"thinking":{"type":"disabled"}}`:                                               `{"thinking":{"type":"disabled"}}`,
		`{"max_tokens":5}`:                                                               `{"max_tokens":5}`,
	} {
		var v any
		json.Unmarshal(withOutputEffort([]byte(body), levels), &v)
		if got, _ := json.Marshal(v); string(got) != want {
			t.Errorf("%s:\n got  %s\n want %s", body, got, want)
		}
	}
}

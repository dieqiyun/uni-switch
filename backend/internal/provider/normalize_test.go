package provider

import "testing"

func TestNormalizeAnthropicBase(t *testing.T) {
	for in, want := range map[string]string{
		"https://api.stepfun.com/step_plan/v1":          "https://api.stepfun.com/step_plan",
		"https://api.stepfun.com/step_plan/v1/":         "https://api.stepfun.com/step_plan",
		"https://api.stepfun.com/step_plan/v1/messages": "https://api.stepfun.com/step_plan",
		"https://api.anthropic.com/v1":                  "https://api.anthropic.com",
		"https://api.anthropic.com":                     "https://api.anthropic.com",
		"https://api.moonshot.ai/anthropic":             "https://api.moonshot.ai/anthropic",
		"api.example.com/v1":                            "https://api.example.com",
		"https://x/v1beta":                              "https://x/v1beta",
	} {
		p := normalize(Provider{Anthropic: in, Chat: "https://c/v1"})
		if p.Anthropic != want || p.Chat != "https://c/v1" {
			t.Errorf("%s: %s, chat %s", in, p.Anthropic, p.Chat)
		}
	}
}

package gateway

import (
	"net/http"
	"net/url"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
	"github.com/tidwall/gjson"
)

// #2379 (jiz4oh): OpenCode's fast variant of a Codex account's GPT model
// reaches the ChatGPT backend as Codex's /fast asks it, service_tier
// "priority", beside the variant's reasoning effort. OpenCode 2 sends the
// tier from the variant's extraBody, OpenCode 1 sends both of the
// variant's options as they are, extraBody a field of its own: that one
// never reaches the backend. A variant that isn't fast asks no tier.
func TestOpenCodeFastVariantReachesCodex(t *testing.T) {
	codexSignedIn(t)
	d := daybreak(t)
	// at chatgpt.com, the one host buildResponses sends Codex's tiers to,
	// reaching the fake backend
	to, _ := url.Parse(provider.CodexBase)
	provider.CodexBase = "https://chatgpt.com/backend-api/codex"
	s := New()
	s.client = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		r.URL.Scheme, r.URL.Host = to.Scheme, to.Host
		return http.DefaultTransport.RoundTrip(r)
	})}
	const msgs = `"messages":[{"role":"user","content":"Respond with OK."}]`
	for _, c := range []struct{ name, body string }{
		{"OpenCode 2, streamed", `{"model":"codex/gpt-daybreak-blue-latest","stream":true,` + msgs + `,"reasoning_effort":"medium","service_tier":"priority"}`},
		{"OpenCode 2, whole", `{"model":"codex/gpt-daybreak-blue-latest",` + msgs + `,"reasoning_effort":"medium","service_tier":"priority"}`},
		{"OpenCode 1, streamed", `{"model":"codex/gpt-daybreak-blue-latest","stream":true,` + msgs + `,"reasoning_effort":"medium","service_tier":"priority","extraBody":{"service_tier":"priority"}}`},
		{"OpenCode 1, whole", `{"model":"codex/gpt-daybreak-blue-latest",` + msgs + `,"reasoning_effort":"medium","service_tier":"priority","extraBody":{"service_tier":"priority"}}`},
	} {
		r := postFrom(t, s, "opencode", "/v1/chat/completions", c.body)
		if r.Tier != "priority" {
			t.Errorf("%s: the route says tier %q was asked", c.name, r.Tier)
		}
		b := d.last(t)
		if got := gjson.GetBytes(b, "service_tier").String(); got != "priority" {
			t.Errorf("%s: the backend got service_tier %q in %s", c.name, got, b)
		}
		if got := gjson.GetBytes(b, "reasoning.effort").String(); got != "medium" {
			t.Errorf("%s: the backend got effort %q in %s", c.name, got, b)
		}
		if gjson.GetBytes(b, "extraBody").Exists() {
			t.Errorf("%s: extraBody reached the backend: %s", c.name, b)
		}
	}
	postFrom(t, s, "opencode", "/v1/chat/completions", `{"model":"codex/gpt-daybreak-blue-latest",`+msgs+`,"reasoning_effort":"medium"}`)
	if b := d.last(t); gjson.GetBytes(b, "service_tier").Exists() {
		t.Errorf("a variant that isn't fast asked a tier: %s", b)
	}
}

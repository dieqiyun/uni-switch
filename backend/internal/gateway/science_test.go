package gateway

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// Claude Science, started on uni-switch's gateway, sends what its client sends
// once signed in (its bundle's yz(): its Claude sign-in as a bearer,
// X-Anthropic-Surface: operon, anthropic-beta: oauth-2025-04-20). It is
// known by that header, listed every model by a claude-… alias its picker
// keeps, and served the model it picked on a third-party provider, with
// the provider's key: its sign-in goes nowhere. A Claude model it asks for
// by Anthropic's id that uni-switch doesn't serve (its default before a pick,
// its haiku for small tasks) goes to the model it was last on.
func TestClaudeScienceIsServedUniSwitchsModels(t *testing.T) {
	f := &fake{t: t, reply: sse(
		`data: {"id":"c1","model":"m1","choices":[{"delta":{"role":"assistant","content":"ok"}}]}`,
		`data: {"id":"c1","choices":[{"delta":{},"finish_reason":"stop"}]}`,
		`data: {"id":"c1","choices":[],"usage":{"prompt_tokens":3,"completion_tokens":1}}`,
		`data: [DONE]`)}
	up := setup(t, provider.Chat, f)
	p := provider.Provider{ID: "fake", Name: "Fake", Key: "k", Chat: up.URL + "/v1", Models: []string{"m1", "deepseek-v4-pro"}}
	if err := provider.Save(p); err != nil {
		t.Fatal(err)
	}
	signIn := "Bearer sk-ant-oat01-" + strings.Repeat("s", 24) // made up, of a sign-in's shape
	s := New()
	send := func(method, path, body string) *httptest.ResponseRecorder {
		t.Helper()
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("User-Agent", "Anthropic/JS 0.71.0")
		req.Header.Set("Authorization", signIn)
		req.Header.Set("X-Anthropic-Surface", "operon")
		req.Header.Set("Anthropic-Beta", "oauth-2025-04-20,server-side-fallback-2026-06-01")
		req.Header.Set("Anthropic-Version", "2023-06-01")
		if body != "" {
			req.Header.Set("Content-Type", "application/json")
		}
		s.Handler().ServeHTTP(rec, req)
		return rec
	}

	// its list: only ids its picker keeps (claude-…, a name that isn't a
	// slug), each one uni-switch's model by its alias
	rec := send("GET", "/v1/models?limit=1000", "")
	var list struct {
		Data []struct {
			ID          string `json:"id"`
			DisplayName string `json:"display_name"`
		} `json:"data"`
		HasMore bool `json:"has_more"`
	}
	if rec.Code != 200 || json.Unmarshal(rec.Body.Bytes(), &list) != nil || len(list.Data) != 2 || list.HasMore {
		t.Fatalf("models: %d %s", rec.Code, rec.Body.String())
	}
	alias := map[string]string{}
	for _, m := range list.Data {
		if !strings.HasPrefix(m.ID, "claude-") || scienceSlug.MatchString(m.DisplayName) {
			t.Errorf("listed %q named %q, which Claude Science's picker leaves out", m.ID, m.DisplayName)
		}
		real, ok := aliased(m.ID)
		if !ok {
			t.Errorf("%q is no alias of uni-switch's", m.ID)
		}
		alias[real] = m.ID
	}
	m1 := alias["fake/m1"]
	if m1 == "" || alias["fake/deepseek-v4-pro"] == "" {
		t.Fatalf("aliases: %v", alias)
	}
	// one model, as its caps are read, by the id it asked by
	if rec := send("GET", "/v1/models/"+m1, ""); rec.Code != 200 || !strings.Contains(rec.Body.String(), `"id":"`+m1+`"`) {
		t.Errorf("model: %d %s", rec.Code, rec.Body.String())
	}

	turn := func(model string, tools bool) map[string]any {
		t.Helper()
		body := `{"model":"` + model + `","max_tokens":50,"stream":true,"messages":[{"role":"user","content":"hi"}]`
		if tools {
			body += `,"tools":[{"name":"run_python","description":"run a cell","input_schema":{"type":"object"}}]`
		}
		f.got, f.head = nil, nil
		rec := send("POST", "/v1/messages", body+"}")
		if rec.Code != 200 || !strings.Contains(rec.Body.String(), "message_stop") {
			t.Fatalf("%s: %d %s", model, rec.Code, rec.Body.String())
		}
		for k, vs := range f.head {
			for _, v := range vs {
				if strings.Contains(v, "sk-ant-oat") {
					t.Errorf("%s: its sign-in went to the provider in %s", model, k)
				}
			}
		}
		if got := f.head.Get("Authorization"); got != "Bearer k" {
			t.Errorf("%s: the provider was sent %q, want its own key", model, got)
		}
		if got := s.Recent()[0].Agent; got != ScienceID {
			t.Errorf("%s: recorded as %q, want %s", model, got, ScienceID)
		}
		var up map[string]any
		json.Unmarshal(f.got, &up)
		return up
	}
	// before any pick, its default goes to the first model it is listed
	if up := turn("claude-opus-5", true); up["model"] != "m1" {
		t.Errorf("its default went to %v, want m1", up["model"])
	}
	// a turn on a model it picked, which is then the one picked
	if up := turn(alias["fake/deepseek-v4-pro"], true); up["model"] != "deepseek-v4-pro" {
		t.Errorf("its pick went to %v", up["model"])
	}
	// its small task on Anthropic's haiku goes there too
	if up := turn("claude-haiku-4-5-20251001", false); up["model"] != "deepseek-v4-pro" {
		t.Errorf("its haiku went to %v, want the model picked", up["model"])
	}
	// and a request without its header is no Claude Science's
	rec = httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/v1/models", nil)
	req.Header.Set("User-Agent", "Anthropic/JS 0.71.0")
	s.Handler().ServeHTTP(rec, req)
	if strings.Contains(rec.Body.String(), scienceAlias) {
		t.Errorf("another Anthropic SDK client was listed Claude Science's aliases: %s", rec.Body.String())
	}
}

package gateway

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/catalog"
	"github.com/dieqiyun/uni-switch/backend/internal/provider"
	"github.com/dieqiyun/uni-switch/backend/internal/usage"
)

func TestFitEffort(t *testing.T) {
	glm := []string{"low", "high", "max"}
	for _, c := range []struct {
		want   string
		levels []string
		got    string
	}{
		{"medium", glm, "high"}, // a tie goes up
		{"low", glm, "low"},
		{"xhigh", glm, "max"},
		{"xhigh", []string{"high", "max"}, "max"}, // DeepSeek V4: Ultracode's xhigh
		{"max", []string{"low", "medium", "high"}, "high"},
		{"low", []string{"none", "minimal", "medium"}, "medium"},
		{"medium", nil, "medium"}, // levels not known: as asked
		{"medium", []string{"none"}, "medium"},
	} {
		if got := fitEffort(c.want, c.levels); got != c.got {
			t.Errorf("fitEffort(%q, %v) = %q, want %q", c.want, c.levels, got, c.got)
		}
	}
}

// Codex's effort reaches a custom OpenAI-compatible provider as
// reasoning_effort, at a level the model takes: glm-5.3-flash takes low,
// high and max, as models.dev has it from those serving it, so Codex's
// "medium" goes as "high"; a model no one gives levels for gets what was
// asked.
func TestResponsesEffortReachesChatVendor(t *testing.T) {
	f := &fake{t: t, reply: sse(
		`data: {"id":"c1","model":"glm-5.3-flash","choices":[{"delta":{"role":"assistant","content":"hi"}}]}`,
		`data: {"id":"c1","choices":[{"delta":{},"finish_reason":"stop"}]}`,
		`data: [DONE]`)}
	up := setup(t, provider.Chat, f)
	os.MkdirAll(filepath.Dir(catalog.CachePath()), 0o755)
	os.WriteFile(catalog.CachePath(), []byte(`{"zai":{"models":{"glm-5.3-flash":{"id":"glm-5.3-flash","reasoning_options":[{"type":"effort","values":["low","high","max"]}]}}}}`), 0o644)
	catalog.Reset()
	t.Cleanup(catalog.Reset)
	if err := provider.Save(provider.Provider{ID: "volc", Name: "Volc", Key: "k", Chat: up.URL + "/v1", Models: []string{"glm-5.3-flash", "other"}}); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct{ model, effort, sent string }{
		{"volc/glm-5.3-flash", "medium", "high"},
		{"volc/glm-5.3-flash", "max", "max"},
		{"volc/glm-5.3-flash", "low", "low"},
		{"volc/other", "medium", "medium"},
	} {
		code, body := post(t, "/v1/responses", `{"model":"`+c.model+`","stream":true,"input":"hi","reasoning":{"effort":"`+c.effort+`"}}`)
		if code != 200 {
			t.Fatalf("status %d: %s", code, body)
		}
		var got map[string]any
		json.Unmarshal(f.got, &got)
		if got["reasoning_effort"] != c.sent || !strings.HasSuffix(f.path, "/chat/completions") {
			t.Errorf("%s at %s: sent %s", c.model, c.effort, f.got)
		}
	}
}

// A model its maker lists with a thinking switch alone — Xiaomi's
// mimo-v2.6-flash, served here by a relay — is sent no more than high: an
// agent asking max or xhigh of it got Xiaomi's 400 (#214). What it may take
// below that goes as asked, as does max to a model no one lists.
func TestSwitchModelSentNoMoreThanHigh(t *testing.T) {
	f := &fake{t: t, reply: sse(
		`data: {"id":"c1","model":"mimo-v2.6-flash","choices":[{"delta":{"role":"assistant","content":"hi"}}]}`,
		`data: {"id":"c1","choices":[{"delta":{},"finish_reason":"stop"}]}`,
		`data: [DONE]`)}
	up := setup(t, provider.Chat, f)
	os.MkdirAll(filepath.Dir(catalog.CachePath()), 0o755)
	os.WriteFile(catalog.CachePath(), []byte(`{
	  "xiaomi":{"models":{"mimo-v2.6-flash":{"id":"mimo-v2.6-flash","reasoning":true,"reasoning_options":[{"type":"toggle"}]}}},
	  "llmgateway":{"models":{"mimo-v2.6-flash":{"id":"mimo-v2.6-flash","reasoning_options":[{"type":"effort","values":["none","low","medium","high","xhigh","max"]}]}}},
	  "zai":{"models":{"glm-5.3-flash":{"id":"glm-5.3-flash","reasoning_options":[{"type":"effort","values":["low","high","max"]}]}}}}`), 0o644)
	catalog.Reset()
	t.Cleanup(catalog.Reset)
	if err := provider.Save(provider.Provider{ID: "volc", Name: "Volc", Key: "k", Chat: up.URL + "/v1", Models: []string{"mimo-v2.6-flash", "glm-5.3-flash", "other"}}); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct{ model, effort, sent string }{
		{"volc/mimo-v2.6-flash", "max", "high"},
		{"volc/mimo-v2.6-flash", "xhigh", "high"},
		{"volc/mimo-v2.6-flash", "low", "low"},
		{"volc/glm-5.3-flash", "medium", "high"},
		{"volc/other", "max", "max"},
	} {
		code, body := post(t, "/v1/responses", `{"model":"`+c.model+`","stream":true,"input":"hi","reasoning":{"effort":"`+c.effort+`"}}`)
		if code != 200 {
			t.Fatalf("status %d: %s", code, body)
		}
		var got map[string]any
		json.Unmarshal(f.got, &got)
		if got["reasoning_effort"] != c.sent {
			t.Errorf("%s at %s: sent %s", c.model, c.effort, f.got)
		}
	}
}

func TestToggleOffOnlyReachesVerifiedProvider(t *testing.T) {
	noRests()
	t.Cleanup(noRests)
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
	body, err := os.ReadFile(filepath.Join("..", "catalog", "testdata", "models.dev-2026-10-09.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(catalog.CachePath()), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(catalog.CachePath(), body, 0o644); err != nil {
		t.Fatal(err)
	}
	catalog.Reset()
	t.Cleanup(catalog.Reset)

	reply := sse(
		`data: {"id":"c1","model":"deepseek-v4-pro","choices":[{"delta":{"role":"assistant","content":"ok"}}]}`,
		`data: {"id":"c1","choices":[{"delta":{},"finish_reason":"stop"}]}`,
		`data: [DONE]`,
	)
	deepseek := &fake{t: t, reply: reply}
	deepseekUp := httptest.NewServer(deepseek)
	t.Cleanup(deepseekUp.Close)
	relay := &fake{t: t, reply: reply}
	relayUp := httptest.NewServer(relay)
	t.Cleanup(relayUp.Close)
	for _, p := range []provider.Provider{
		{ID: "deepseek", Name: "DeepSeek", Key: "kd", Chat: deepseekUp.URL + "/v1", Catalog: "deepseek", Models: []string{"deepseek-v4-pro"}},
		{ID: "relay", Name: "Relay", Key: "kr", Chat: relayUp.URL + "/v1", Models: []string{"deepseek-v4-pro"}},
	} {
		if err := provider.Save(p); err != nil {
			t.Fatal(err)
		}
	}
	if err := catalog.SaveLive("relay", relayUp.URL+"/v1", []catalog.Model{{ID: "deepseek-v4-pro"}}); err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		model string
		up    *fake
		want  string
	}{
		{"deepseek/deepseek-v4-pro", deepseek, "none"},
		{"relay/deepseek-v4-pro", relay, "low"},
	} {
		request := `{"model":"` + tc.model + `","stream":true,"messages":[{"role":"user","content":"hi"}],"reasoning_effort":"none"}`
		code, response := post(t, "/v1/chat/completions", request)
		if code != 200 {
			t.Fatalf("%s: status %d: %s", tc.model, code, response)
		}
		var sent map[string]any
		if err := json.Unmarshal(tc.up.got, &sent); err != nil {
			t.Fatalf("%s: invalid upstream request %s: %v", tc.model, tc.up.got, err)
		}
		if sent["reasoning_effort"] != tc.want {
			t.Errorf("%s sent reasoning_effort %v, want %q", tc.model, sent["reasoning_effort"], tc.want)
		}
	}
}

// Each request's route keeps the reasoning the agent asked for, and each
// try the reasoning its model was sent at, fitted to the model's levels;
// the usage keeps what the model was sent at, by session.
func TestTraceRecordsTheEffortSent(t *testing.T) {
	f := &fake{t: t, reply: sse(
		`data: {"id":"c1","model":"glm-5.3-flash","choices":[{"delta":{"role":"assistant","content":"hi"}}]}`,
		`data: {"id":"c1","choices":[{"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":3,"completion_tokens":1}}`,
		`data: [DONE]`)}
	up := setup(t, provider.Chat, f)
	os.MkdirAll(filepath.Dir(catalog.CachePath()), 0o755)
	os.WriteFile(catalog.CachePath(), []byte(`{"zai":{"models":{"glm-5.3-flash":{"id":"glm-5.3-flash","reasoning_options":[{"type":"effort","values":["low","high","max"]}]}}}}`), 0o644)
	catalog.Reset()
	t.Cleanup(catalog.Reset)
	if err := provider.Save(provider.Provider{ID: "volc", Name: "Volc", Key: "k", Chat: up.URL + "/v1", Models: []string{"glm-5.3-flash", "other"}}); err != nil {
		t.Fatal(err)
	}
	s := New()
	for _, c := range []struct{ path, body, asked, sent string }{
		{"/v1/responses", `{"model":"volc/glm-5.3-flash","stream":true,"input":"hi","reasoning":{"effort":"medium"}}`, "medium", "high"},
		{"/v1/chat/completions", `{"model":"volc/other","stream":true,"messages":[{"role":"user","content":"hi"}],"reasoning_effort":"xhigh"}`, "xhigh", "xhigh"},
		{"/v1/messages", `{"model":"volc/glm-5.3-flash","stream":true,"max_tokens":32000,"thinking":{"type":"enabled","budget_tokens":20000},"messages":[{"role":"user","content":"hi"}]}`, "high", "high"},
		{"/v1/messages", `{"model":"volc/other","stream":true,"max_tokens":32000,"thinking":{"type":"adaptive"},"output_config":{"effort":"max"},"messages":[{"role":"user","content":"hi"}]}`, "max", "max"},
		{"/v1/chat/completions", `{"model":"volc/other","stream":true,"messages":[{"role":"user","content":"hi"}]}`, "", ""},
	} {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest("POST", c.path, strings.NewReader(c.body))
		req.Header.Set(SessionHeader, "sess-1")
		s.Handler().ServeHTTP(rec, req)
		if rec.Code != 200 {
			t.Fatalf("%s: %d %s", c.body, rec.Code, rec.Body)
		}
		r := lastRoute(s)
		if r.Effort != c.asked || len(r.Tries) != 1 || r.Tries[0].Effort != c.sent || r.Tries[0].Picked {
			t.Errorf("%s: route at %q, tries %+v; want asked %q, sent %q", c.body, r.Effort, r.Tries, c.asked, c.sent)
		}
	}
	var got []string
	for _, rec := range usage.Load(time.Time{}) {
		got = append(got, rec.Effort)
	}
	if strings.Join(got, ",") != "high,xhigh,high,max," {
		t.Errorf("usage efforts %q", got)
	}
	vs := usage.Vias(time.Time{}).Of(usage.AgentOf("") + "|sess-1")
	if len(vs) != 4 || vs[0].Model != "glm-5.3-flash" || vs[0].Effort != "high" || vs[0].Calls != 2 {
		t.Errorf("vias %+v", vs)
	}
}

// A Gemini request's thinkingConfig is the reasoning it asks for, read
// as parseGemini reads it (#4529): its level, or its budget as the level
// it is nearest, by either spelling of the fields' names.
func TestGeminiRequestEffort(t *testing.T) {
	for _, c := range []struct {
		body string
		want string
	}{
		{`{"generationConfig":{"thinkingConfig":{"thinkingLevel":"high"}}}`, "high"},
		{`{"generationConfig":{"thinkingConfig":{"thinking_level":"LOW"}}}`, "low"},
		{`{"generationConfig":{"thinkingConfig":{"thinkingBudget":512}}}`, "low"},
		{`{"generationConfig":{"thinkingConfig":{"thinking_budget":32768}}}`, "xhigh"},
		{`{"generationConfig":{"thinkingConfig":{"thinkingBudget":-1}}}`, "medium"},
		{`{"generationConfig":{"thinkingConfig":{"thinkingBudget":0}}}`, ""},
		{`{"generationConfig":{"thinkingConfig":{"includeThoughts":true}}}`, ""},
		{`{"generationConfig":{}}`, ""},
		{`{"contents":[{"parts":[{"text":"hi"}]}]}`, ""},
	} {
		if got := requestEffort(provider.Gemini, []byte(c.body)); got != c.want {
			t.Errorf("%s: requestEffort %q, want %q", c.body, got, c.want)
		}
		r, err := parseGemini([]byte(c.body))
		if err != nil {
			t.Fatalf("%s: %v", c.body, err)
		}
		if r.Effort != c.want {
			t.Errorf("%s: parseGemini effort %q, want %q", c.body, r.Effort, c.want)
		}
	}
}

// requestEffort reads the reasoning a request asks for in each API's words.
func TestRequestEffort(t *testing.T) {
	for _, c := range []struct {
		proto provider.Protocol
		body  string
		want  string
	}{
		{provider.Chat, `{"reasoning_effort":"low"}`, "low"},
		{provider.Chat, `{"model":"m"}`, ""},
		{provider.Responses, `{"reasoning":{"effort":"xhigh","summary":"auto"}}`, "xhigh"},
		{provider.Responses, `{"reasoning":{"summary":"auto"}}`, ""},
		{provider.Anthropic, `{"thinking":{"type":"adaptive"},"output_config":{"effort":"medium"}}`, "medium"},
		{provider.Anthropic, `{"thinking":{"type":"enabled","budget_tokens":4096}}`, "low"},
		{provider.Anthropic, `{"thinking":{"type":"disabled"},"output_config":{"effort":"high"}}`, ""},
		{provider.Anthropic, `{"output_config":{"effort":"high"}}`, ""},
	} {
		if got := requestEffort(c.proto, []byte(c.body)); got != c.want {
			t.Errorf("%s %s: %q, want %q", c.proto, c.body, got, c.want)
		}
	}
}

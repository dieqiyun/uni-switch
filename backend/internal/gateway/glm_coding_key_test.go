package gateway

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
	"github.com/tidwall/gjson"
)

// zhipuNoBalance is Zhipu's answer to a key whose account has no balance
// for the API asked, as @cleverpigo's editor showed it (429 · 余额不足…).
const zhipuNoBalance = `{"error":{"code":"1113","message":"余额不足或无可用资源包,请充值。"}}`

// glmKeys plays Zhipu's and Z.ai's OpenAI APIs: the pay-as-you-go one
// (/api/paas/v4) serves the keys with a balance, the GLM Coding Plan's
// (/api/coding/paas/v4) the plans' keys, and each refuses the others 1113.
type glmKeys struct {
	mu    sync.Mutex
	asked []string // host + path
	payg  map[string]bool
	plan  map[string]bool
}

func (g *glmKeys) RoundTrip(r *http.Request) (*http.Response, error) {
	body, _ := io.ReadAll(r.Body)
	g.mu.Lock()
	g.asked = append(g.asked, r.URL.Host+r.URL.Path)
	g.mu.Unlock()
	key := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	served := false
	switch {
	case strings.HasPrefix(r.URL.Path, "/api/paas/v4/"):
		served = g.payg[key]
	case strings.HasPrefix(r.URL.Path, "/api/coding/paas/v4/"):
		served = g.plan[key]
	}
	if !served {
		return &http.Response{StatusCode: 429, Status: "429 Too Many Requests", Header: http.Header{"Content-Type": {"application/json"}},
			Body: io.NopCloser(strings.NewReader(zhipuNoBalance)), Request: r}, nil
	}
	if gjson.GetBytes(body, "stream").Bool() {
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"text/event-stream"}},
			Body: io.NopCloser(strings.NewReader("data: {\"id\":\"c\",\"object\":\"chat.completion.chunk\",\"model\":\"glm-5.1\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":\"pong\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")), Request: r}, nil
	}
	return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}},
		Body: io.NopCloser(strings.NewReader(`{"id":"c","object":"chat.completion","model":"glm-5.1","choices":[{"index":0,"message":{"role":"assistant","content":"pong"},"finish_reason":"stop"}],"usage":{"prompt_tokens":3,"completion_tokens":1}}`)), Request: r}, nil
}

func (g *glmKeys) took() []string {
	g.mu.Lock()
	defer g.mu.Unlock()
	out := g.asked
	g.asked = nil
	return out
}

// A GLM Coding Plan's key on the zhipu or zai preset left at its first
// plan, Pay as you go (@cleverpigo on X: "glm coding plan放在codex里显示余额不足"):
// Claude Code works, its Anthropic endpoint being the plan's too, and
// Codex was refused 429 1113 at /api/paas/v4. Its request is asked again
// at /api/coding/paas/v4 and answered, and the provider moves to the
// plan's endpoints, so the next goes there first. A pay-as-you-go key with
// a balance stays at /api/paas/v4 and is never sent to the plan's; one
// with neither is told what to change.
func TestGLMCodingPlanKeyAtPayAsYouGo(t *testing.T) {
	for _, tc := range []struct{ preset, host string }{
		{"zhipu", "open.bigmodel.cn"},
		{"zai", "api.z.ai"},
	} {
		t.Run(tc.preset, func(t *testing.T) {
			t.Setenv("XDG_CONFIG_HOME", t.TempDir())
			t.Setenv("XDG_CACHE_HOME", t.TempDir())
			// ids of their own per plan: a refused key rests by its id
			add := func(id, key string) {
				t.Helper()
				id = tc.preset + "-" + id
				p, err := provider.FromPreset(tc.preset)
				if err != nil {
					t.Fatal(err)
				}
				if p.Chat != "https://"+tc.host+"/api/paas/v4" {
					t.Fatalf("the preset's first plan is %s, not pay as you go", p.Chat)
				}
				p.ID, p.Name, p.Key, p.Models = id, id, key, []string{"glm-5.1"}
				if err := provider.Save(p); err != nil {
					t.Fatal(err)
				}
			}
			add("plan", "plan-key")
			add("payg", "payg-key")
			add("broke", "broke-key")
			fake := &glmKeys{payg: map[string]bool{"payg-key": true}, plan: map[string]bool{"plan-key": true}}
			s := New()
			s.client = &http.Client{Transport: fake}
			ask := func(path, body string) *httptest.ResponseRecorder {
				t.Helper()
				rec := httptest.NewRecorder()
				s.Handler().ServeHTTP(rec, httptest.NewRequest("POST", path, strings.NewReader(body)))
				return rec
			}
			codex := func(id string) *httptest.ResponseRecorder {
				id = tc.preset + "-" + id
				return ask("/v1/responses", `{"model":"`+id+`/glm-5.1","input":"Say pong","stream":false}`)
			}
			stored := func(id string) provider.Provider {
				t.Helper()
				p, err := provider.Find(tc.preset + "-" + id)
				if err != nil || p == nil {
					t.Fatal(id, err)
				}
				return *p
			}

			// the plan's key: answered, by the plan's endpoint
			if rec := codex("plan"); rec.Code != 200 {
				t.Fatalf("plan key: %d %s", rec.Code, rec.Body)
			}
			if got := fake.took(); len(got) != 2 || got[0] != tc.host+"/api/paas/v4/chat/completions" || got[1] != tc.host+"/api/coding/paas/v4/chat/completions" {
				t.Errorf("plan key asked %v, want pay as you go then the plan's endpoint", got)
			}
			p := stored("plan")
			if p.Chat != "https://"+tc.host+"/api/coding/paas/v4" || p.Responses != "https://"+tc.host+"/api/v1" || p.Anthropic != "https://"+tc.host+"/api/anthropic" {
				t.Errorf("plan provider not moved to the Coding Plan's endpoints: chat %s responses %s anthropic %s", p.Chat, p.Responses, p.Anthropic)
			}
			// and from then on the plan's own endpoints are asked first
			if rec := ask("/v1/chat/completions", `{"model":"`+tc.preset+`-plan/glm-5.1","messages":[{"role":"user","content":"Say pong"}]}`); rec.Code != 200 {
				t.Fatalf("plan key again: %d %s", rec.Code, rec.Body)
			}
			if got := fake.took(); len(got) != 1 || got[0] != tc.host+"/api/coding/paas/v4/chat/completions" {
				t.Errorf("plan key asked %v the second time, want the plan's endpoint alone", got)
			}

			// a pay-as-you-go key with a balance: where it was
			if rec := codex("payg"); rec.Code != 200 {
				t.Fatalf("payg key: %d %s", rec.Code, rec.Body)
			}
			if got := fake.took(); len(got) != 1 || got[0] != tc.host+"/api/paas/v4/chat/completions" {
				t.Errorf("payg key asked %v, want pay as you go alone", got)
			}
			if p := stored("payg"); p.Chat != "https://"+tc.host+"/api/paas/v4" || p.Responses != "" {
				t.Errorf("payg provider moved: chat %s responses %s", p.Chat, p.Responses)
			}

			// neither: refused, saying what to change
			rec := codex("broke")
			if rec.Code == 200 {
				t.Fatalf("broke key answered: %s", rec.Body)
			}
			msg := gjson.Get(rec.Body.String(), "error.message").String()
			for _, want := range []string{"余额不足", "https://" + tc.host + "/api/paas/v4", "https://" + tc.host + "/api/coding/paas/v4", "Coding Plan", "top up"} {
				if !strings.Contains(msg, want) {
					t.Errorf("broke key's error %q doesn't say %q", msg, want)
				}
			}
			if p := stored("broke"); p.Chat != "https://"+tc.host+"/api/paas/v4" {
				t.Errorf("broke provider moved to %s", p.Chat)
			}
		})
	}
}

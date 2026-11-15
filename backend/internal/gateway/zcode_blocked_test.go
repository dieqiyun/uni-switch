package gateway

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// countAsked is an upstream that counts tokens, noting each time it's asked.
type countAsked struct {
	mu    sync.Mutex
	paths []string
}

func (c *countAsked) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	c.mu.Lock()
	c.paths = append(c.paths, r.URL.Path)
	c.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	io.WriteString(w, `{"input_tokens":999}`)
}

func (c *countAsked) asked() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]string(nil), c.paths...)
}

func estimated(t *testing.T, rec *httptest.ResponseRecorder) int {
	t.Helper()
	var got struct {
		InputTokens int `json:"input_tokens"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); rec.Code != 200 || err != nil || got.InputTokens == 0 {
		t.Fatalf("count_tokens: %d %s", rec.Code, rec.Body)
	}
	return got.InputTokens
}

// Claude Code asks count_tokens before each turn; ZCode itself never does,
// and zcode.z.ai answers it with an error every time. A ZCode account's
// count is estimated, the vendor not asked.
func TestZCodeCountTokensEstimated(t *testing.T) {
	fresh(t)
	up := &countAsked{}
	srv := httptest.NewServer(up)
	t.Cleanup(srv.Close)
	p := provider.Provider{ID: "zcode", Name: "ZCode", Anthropic: srv.URL + "/api/v1/zcode-plan/anthropic", Models: []string{"glm-5.1"},
		Account: &provider.Account{Agent: "zcode", User: "trial@example.com", Plan: "Start Plan"}}
	body := []byte(`{"model":"glm-5.1","messages":[{"role":"user","content":"hello there"}]}`)
	rec := httptest.NewRecorder()
	New().countOn(rec, httptest.NewRequest("POST", "/v1/messages/count_tokens", nil), p, "glm-5.1", true, body)
	if n := estimated(t, rec); n == 999 || len(up.asked()) > 0 {
		t.Fatalf("zcode.z.ai was asked to count: %v, %d", up.asked(), n)
	}
}

// edgeBlockPage is Alibaba Cloud's ESA block page, as zcode.z.ai served it.
const edgeBlockPage = `<!DOCTYPE html>
<html><head><meta charset="utf-8"><title>405</title></head>
<body><div class="main"><h1>Sorry, your request has been blocked due to unusual activity.</h1>
<p>Request ID: 0bd6a2b417592xxxx</p>
<a href="https://errors.aliyun.com/error/405?code=blocked">errors.aliyun.com</a></div></body></html>`

// A custom Responses relay refusing with 403 and no message of its own
// tells the agent what it was, not a bare "403 Forbidden" (maralica on
// Discord): Cloudflare's block page as a firewall's, another page by its
// title, an empty body as a refusal with no details. Its JSON error still
// reads as its message.
func TestRelayRefusalSaysWhatItWas(t *testing.T) {
	cf := `<!DOCTYPE html>
<html class="no-js" lang="en-US"><head><title>Attention Required! | Cloudflare</title></head>
<body><div id="cf-error-details"><h1 data-translate="block_headline">Sorry, you have been blocked</h1>
<span class="cf-footer-item">Cloudflare Ray ID: <strong>8d1c2f3a4b5c6d7e</strong></span></div></body></html>`
	for _, c := range []struct {
		name, ct, body string
		want           []string
	}{
		{"cloudflare", "text/html; charset=UTF-8", cf, []string{"403 Forbidden: a web page, not an API reply: “Attention Required! | Cloudflare”", provider.BlockedHint}},
		{"page", "text/html", "<html><head><title>403 Forbidden</title></head><body><center><h1>403 Forbidden</h1></center><hr><center>nginx</center></body></html>", []string{"403 Forbidden: a web page, not an API reply: “403 Forbidden”"}},
		{"empty", "", "", []string{"403 Forbidden: " + provider.NoDetails}},
		{"json", "application/json", `{"error":{"message":"this key may not use gpt-5.5","type":"forbidden"}}`, []string{"this key may not use gpt-5.5"}},
	} {
		t.Run(c.name, func(t *testing.T) {
			fresh(t)
			relay := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if c.ct != "" {
					w.Header().Set("Content-Type", c.ct)
				}
				w.WriteHeader(http.StatusForbidden)
				io.WriteString(w, c.body)
			}))
			t.Cleanup(relay.Close)
			if err := provider.Save(provider.Provider{ID: "relay", Name: "Relay", Key: "k", Models: []string{"gpt-5.5"}, Responses: relay.URL + "/v1"}); err != nil {
				t.Fatal(err)
			}
			code, reply := postTo(t, New(), "/v1/responses", `{"model":"relay/gpt-5.5","input":"hi"}`)
			if code != http.StatusForbidden || strings.Contains(reply, "<") || strings.Contains(reply, "&lt;") {
				t.Fatalf("%d %s", code, reply)
			}
			var v struct {
				Error struct {
					Message string `json:"message"`
				} `json:"error"`
			}
			if err := json.Unmarshal([]byte(reply), &v); err != nil {
				t.Fatalf("%v: %s", err, reply)
			}
			if strings.Contains(v.Error.Message, "<") {
				t.Fatalf("markup in %q", v.Error.Message)
			}
			for _, w := range c.want {
				if !strings.Contains(v.Error.Message, w) {
					t.Errorf("missing %q in %q", w, v.Error.Message)
				}
			}
		})
	}
}

// The ZCode plugin's answer to the Start Plan's 405 code 3012 (0.1.27 on,
// plugins#348) is an account ZCode's risk control blocked: it rests two
// hours as restricted, the account rather than one model, and is lifted
// by hand like WorkBuddy's. The words 0.1.26 gave the same 405 read as any
// failure does.
func TestZCodeStartPlanBlockRestsRestricted(t *testing.T) {
	fresh(t)
	s := New()
	p := provider.Provider{ID: "zcode", Name: "ZCode", Account: &provider.Account{Agent: "plugin", User: "u1"}}
	c := candidate{p: p, model: "GLM-5.3-Flash", rest: p.ID + "#u1"}
	body := []byte(`{"type":"error","error":{"type":"permission_error","message":"request has been blocked due to unusual activity. (405, code 3012) — ` + provider.ZCodeRestrictedHint + `"}}`)
	now := time.Now()
	rest := s.restAfter(c, 405, nil, body)
	if rest.Why != failRestricted || rest.By != "restricted" || rest.Key != c.restKey() {
		t.Fatalf("rests %+v", rest)
	}
	if d := restedFor(rest, now); d < restrictedRest-time.Minute || d > restrictedRest+time.Minute {
		t.Fatalf("rests %v", d)
	}
	if !s.Unrest(rest.Key) {
		t.Fatal("its rest can't be lifted")
	}
	old := []byte(`{"type":"error","error":{"type":"permission_error","message":"405 Method Not Allowed — ZCode's Start Plan still turned this request away, though uni-switch sends it as the ZCode app does; it can be a network block of this IP, or ZCode checking for something new. Use an account with a GLM Coding Plan, or add another provider to this group"}}`)
	if f := failureOf(c, 405, old); f == failRestricted {
		t.Fatal("0.1.26's words read as restricted")
	}
	if f := failureOf(c, 200, body); f == failRestricted {
		t.Fatal("a 200 read as restricted")
	}
}

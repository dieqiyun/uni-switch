package gateway

import (
	"encoding/json"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
	"github.com/dieqiyun/uni-switch/backend/internal/usage"
)

// Antigravity CLI (agy) asks as Google's Gemini SDK, its User-Agent
// google-genai-sdk/… with nothing of agy's own, and sends its key as
// x-goog-api-key; the key uni-switch starts it with names it (TokenFor), so its
// calls are recorded as agy's, as Gemini's ?key= names it too. A command
// copied before, with the plain token, still gets an answer, recorded as
// the SDK's as it was.
func TestAgyAgent(t *testing.T) {
	setHome(t, t.TempDir())
	f := &fake{t: t, reply: sse(
		`data: {"id":"c1","choices":[{"delta":{"content":"OK"},"finish_reason":"stop"}],"usage":{"prompt_tokens":5,"completion_tokens":2}}`,
		`data: [DONE]`)}
	setup(t, provider.Chat, f)
	const ua = "google-genai-sdk/1.71.0 gl-go/go1.28-20260721-RC03 cl/951519500"
	body := `{"contents":[{"role":"user","parts":[{"text":"hi"}]}]}`
	for i, c := range []struct{ header, query, want string }{
		{TokenFor("agy"), "", "agy"},
		{"", TokenFor("agy"), "agy"},
		{Token, "", "google-genai-sdk"},
	} {
		path := "/v1beta/models/fake/m1:streamGenerateContent?alt=sse"
		if c.query != "" {
			path += "&key=" + c.query
		}
		rec := httptest.NewRecorder()
		req := httptest.NewRequest("POST", path, strings.NewReader(body))
		req.Header.Set("User-Agent", ua)
		if c.header != "" {
			req.Header.Set("x-goog-api-key", c.header)
		}
		New().Handler().ServeHTTP(rec, req)
		if rec.Code != 200 || !strings.Contains(rec.Body.String(), "OK") {
			t.Fatalf("%+v: %d %s", c, rec.Code, rec.Body.String())
		}
		var got []usage.Record
		for range 100 {
			if got = usage.Load(time.Time{}); len(got) > i {
				break
			}
			time.Sleep(20 * time.Millisecond)
		}
		if len(got) <= i || got[i].Agent != c.want {
			t.Fatalf("%+v: records %+v, want the last %s's", c, got, c.want)
		}
	}
}

// Antigravity CLI sends its tools' results under "model", right after the
// call (captured from agy against a stub): kept there, a conversation that
// ran a tool ends with the model's turn, and Antigravity answers 400
// "Requests ending with a model turn are not supported" (#650) — straight
// from the gateway or through another uni-switch (Anthropic Messages between
// the two). A turn with no tool already ended with the user's.
func TestAgyToolResultsAreTheUsers(t *testing.T) {
	const ask = `{"role":"user","parts":[{"text":"<USER_REQUEST>\nsay hi\n</USER_REQUEST>"}]}`
	const ran = `,{"role":"model","parts":[{"text":"Thinking.","thought":true},{"functionCall":{"id":"c1","name":"run_command","args":{"CommandLine":"echo hi"}},"thoughtSignature":"c2ln"}]}` +
		`,{"role":"model","parts":[{"functionResponse":{"id":"c1","name":"run_command","response":{"output":"hi"}}}]}`
	const answered = `,{"role":"model","parts":[{"text":"Thinking.","thought":true},{"text":"Hello.","thoughtSignature":"c2ln"}]}` +
		`,{"role":"user","parts":[{"text":"<USER_REQUEST>\nsay bye\n</USER_REQUEST>"}]}` +
		`,{"role":"user","parts":[{"text":"<SYSTEM_MESSAGE>notice</SYSTEM_MESSAGE>"}]}`
	for _, c := range []struct {
		name, contents string
		roles          []string
	}{
		{"no tool", ask + answered, []string{"user", "model", "user"}},
		{"first turn ran a tool", ask + ran, []string{"user", "model", "user"}},
		{"second turn ran a tool", ask + ran + answered + ran, []string{"user", "model", "user", "model", "user", "model", "user"}},
	} {
		r, err := parseGemini([]byte(`{"contents":[` + c.contents + `]}`))
		if err != nil {
			t.Fatal(err)
		}
		for _, via := range []provider.Protocol{"", provider.Anthropic, provider.Chat, provider.Responses} {
			q := r
			if via != "" {
				out, _ := build(via, r, "antigravity/gemini-3-flash", "", false)
				if q, err = parse(via, out); err != nil {
					t.Fatal(err)
				}
			}
			var env struct {
				Request struct {
					Contents []struct {
						Role  string           `json:"role"`
						Parts []map[string]any `json:"parts"`
					} `json:"contents"`
				} `json:"request"`
			}
			if err := json.Unmarshal(buildCodeAssist(q, "gemini-3-flash", "antigravity"), &env); err != nil {
				t.Fatal(err)
			}
			var roles []string
			var text string
			for _, ct := range env.Request.Contents {
				roles = append(roles, ct.Role)
				for _, p := range ct.Parts {
					if s, ok := p["text"].(string); ok {
						text += s
					}
				}
			}
			last := env.Request.Contents[len(env.Request.Contents)-1]
			if !slices.Equal(roles, c.roles) {
				t.Fatalf("%s via %q: roles %v, want %v", c.name, via, roles, c.roles)
			}
			if strings.HasSuffix(c.contents, ran) && last.Parts[0]["functionResponse"] == nil {
				t.Fatalf("%s via %q: the last turn is %v, not the tool's result", c.name, via, last.Parts)
			}
			for _, w := range []string{"say hi", "say bye", "Hello."} {
				if strings.Contains(c.contents, w) && !strings.Contains(text, w) {
					t.Fatalf("%s via %q: %q was lost", c.name, via, w)
				}
			}
		}
	}
}

// Goose's HTTP client sends no User-Agent (reqwest's default,
// crates/goose-providers/src/api_client.rs), so its requests are Goose's by
// the key uni-switch writes into its custom provider (#1083).
func TestGooseAgentByKey(t *testing.T) {
	setHome(t, t.TempDir())
	f := &fake{t: t, reply: sse(
		`data: {"id":"c1","choices":[{"delta":{"content":"OK"},"finish_reason":"stop"}],"usage":{"prompt_tokens":5,"completion_tokens":2}}`,
		`data: [DONE]`)}
	setup(t, provider.Chat, f)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(`{"model":"fake/m1","stream":true,"messages":[{"role":"user","content":"hi"}]}`))
	req.Header.Del("User-Agent")
	req.Header.Set("Authorization", "Bearer "+TokenFor("goose"))
	New().Handler().ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("%d %s", rec.Code, rec.Body.String())
	}
	var got []usage.Record
	for range 100 {
		if got = usage.Load(time.Time{}); len(got) > 0 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if len(got) != 1 || got[0].Agent != "goose" {
		t.Fatalf("records %+v, want one of goose's", got)
	}
}

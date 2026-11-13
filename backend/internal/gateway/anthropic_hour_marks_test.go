package gateway

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// hourTurn is a Responses (Codex) turn with a system prompt, a tool call and
// its result, with extra top-level fields spliced in.
func hourTurn(extra string) []byte {
	return []byte(`{"model":"m","instructions":"be brief","stream":true` + extra + `,
	  "tools":[{"type":"function","name":"shell","parameters":{"type":"object"}}],
	  "input":[{"type":"message","role":"user","content":[{"type":"input_text","text":"list files"}]},
	    {"type":"function_call","call_id":"c1","name":"shell","arguments":"{\"cmd\":\"ls\"}"},
	    {"type":"function_call_output","call_id":"c1","output":"a.go b.go"}]}`)
}

// markTTLs lists the ttl of every cache_control in an Anthropic body, ""
// for a mark with none (Anthropic's five minutes).
func markTTLs(t *testing.T, b []byte) []string {
	t.Helper()
	var out struct {
		System []aBlock         `json:"system"`
		Tools  []map[string]any `json:"tools"`
		Msgs   []struct {
			Content []aBlock `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatalf("%v: %s", err, b)
	}
	var ttls []string
	for _, s := range out.System {
		if s.CacheControl != nil {
			ttls = append(ttls, s.CacheControl["ttl"])
		}
	}
	for _, tl := range out.Tools {
		if cc, ok := tl["cache_control"].(map[string]any); ok {
			ttl, _ := cc["ttl"].(string)
			ttls = append(ttls, ttl)
		}
	}
	for _, m := range out.Msgs {
		for _, c := range m.Content {
			if c.CacheControl != nil {
				ttls = append(ttls, c.CacheControl["ttl"])
			}
		}
	}
	if n := strings.Count(string(b), `"cache_control"`); n != len(ttls) {
		t.Fatalf("%d marks, %d read: %s", n, len(ttls), b)
	}
	return ttls
}

// 7eleven (Discord #feedback, 10-28): a Codex turn served by a Claude model
// on Anthropic's Messages had uni-switch's cache marks at the default five
// minutes, so a thread's prompt was written again after every pause longer
// than that. A Responses client's marks last an hour, every one of them (a
// 1h mark after a 5m one is refused), unless the client asked for its
// prompt to be kept in memory only; a Chat client's, and a model that
// isn't Claude's, stay at five minutes.
func TestResponsesToClaudeMarksLastAnHour(t *testing.T) {
	for _, c := range []struct {
		name  string
		parse func() (*Request, error)
		model string
		noSys bool
		want  string
	}{
		{"responses", func() (*Request, error) { return parseResponses(hourTurn("")) }, "claude-sonnet-5", false, "1h"},
		{"responses, tools marked", func() (*Request, error) { return parseResponses(hourTurn("")) }, "claude-sonnet-5", true, "1h"},
		{"responses, a vendor's name", func() (*Request, error) { return parseResponses(hourTurn("")) }, "anthropic/claude-opus-5-5", false, "1h"},
		{"responses, 24h asked", func() (*Request, error) { return parseResponses(hourTurn(`,"prompt_cache_retention":"24h"`)) }, "claude-sonnet-5", false, "1h"},
		{"responses, in_memory asked", func() (*Request, error) { return parseResponses(hourTurn(`,"prompt_cache_retention":"in_memory"`)) }, "claude-sonnet-5", false, ""},
		{"responses, not claude", func() (*Request, error) { return parseResponses(hourTurn("")) }, "glm-5", false, ""},
		{"chat", func() (*Request, error) {
			return parseChat([]byte(`{"model":"m","stream":true,
			  "tools":[{"type":"function","function":{"name":"shell","parameters":{"type":"object"}}}],
			  "messages":[{"role":"system","content":"be brief"},{"role":"user","content":"list files"}]}`))
		}, "claude-sonnet-5", false, ""},
	} {
		t.Run(c.name, func(t *testing.T) {
			r, err := c.parse()
			if err != nil {
				t.Fatal(err)
			}
			if c.noSys {
				r.System = ""
			}
			b := buildAnthropic(r, c.model)
			ttls := markTTLs(t, b)
			if len(ttls) < 2 {
				t.Fatalf("%d marks: %s", len(ttls), b)
			}
			for _, ttl := range ttls {
				if ttl != c.want {
					t.Fatalf("marks %q, want every one %q: %s", ttls, c.want, b)
				}
			}
		})
	}
}

// A Messages upstream (a relay) that knows no ttl in cache_control turns
// the 1h marks away with a 400: the turn is sent again at five minutes,
// and the provider is remembered, so the next turn goes so at once. A 400
// for something else is not taken for it.
func TestHourMarksRefused(t *testing.T) {
	for _, c := range []struct {
		name   string
		refuse func(body []byte) (int, string)
		codes  []int
		calls  []int
		fits   bool
	}{
		{"ttl refused", func(body []byte) (int, string) {
			if bytes.Contains(body, []byte(`"ttl"`)) {
				return 400, `{"type":"error","error":{"type":"invalid_request_error","message":"system.0.cache_control.ttl: Extra inputs are not permitted"}}`
			}
			return 0, ""
		}, []int{200, 200}, []int{2, 1}, false},
		{"refused for something else", func(body []byte) (int, string) {
			return 400, `{"type":"error","error":{"type":"invalid_request_error","message":"prompt is too long"}}`
		}, []int{400, 400}, []int{1, 1}, true},
	} {
		t.Run(c.name, func(t *testing.T) {
			f := &fake{t: t, reply: anthropicAnswer, refuse: c.refuse}
			up := setup(t, provider.Anthropic, f)
			if err := provider.Save(provider.Provider{ID: "fake", Name: "Fake", Key: "k", Models: []string{"claude-sonnet-5"}, Anthropic: up.URL}); err != nil {
				t.Fatal(err)
			}
			srv := New()
			for turn := range 2 {
				calls := f.calls
				code, body := postTo(t, srv, "/v1/responses", strings.Replace(string(hourTurn("")), `"model":"m"`, `"model":"fake/claude-sonnet-5"`, 1))
				if code != c.codes[turn] || f.calls-calls != c.calls[turn] {
					t.Fatalf("turn %d: %d after %d calls, want %d after %d: %s", turn, code, f.calls-calls, c.codes[turn], c.calls[turn], body)
				}
				if c.fits && !bytes.Contains(f.got, []byte(`"ttl":"1h"`)) {
					t.Fatalf("turn %d went without 1h marks: %s", turn, f.got)
				}
				if !c.fits && bytes.Contains(f.got, []byte(`"ttl"`)) {
					t.Fatalf("turn %d's last try kept a ttl: %s", turn, f.got)
				}
			}
			if srv.fits("fake", hourMarksRefused, provider.Anthropic) != c.fits {
				t.Fatalf("fits = %v, want %v", !c.fits, c.fits)
			}
		})
	}
}

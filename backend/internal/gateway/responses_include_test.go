package gateway

import (
	"encoding/json"
	"slices"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// #315: a Responses request rebuilt by uni-switch asks for what its client's
// include did, as prompt_cache_key goes on: a strict Codex relay refuses
// one without reasoning.encrypted_content. uni-switch's own search sources are
// added to it once.
func TestResponsesKeepsInclude(t *testing.T) {
	include := func(body, host string) []string {
		t.Helper()
		r, err := parseResponses([]byte(body))
		if err != nil {
			t.Fatal(err)
		}
		var q struct {
			Include []string `json:"include"`
		}
		json.Unmarshal(buildResponses(r, "m", host, false), &q)
		return q.Include
	}
	for _, c := range []struct {
		name, body, host string
		want             []string
	}{
		{"sealed reasoning", `{"model":"m","input":"hi","reasoning":{"effort":"low"},"include":["reasoning.encrypted_content"]}`, "relay.example", []string{"reasoning.encrypted_content"}},
		{"with search sources", `{"model":"m","input":"hi","reasoning":{"effort":"low"},"include":["reasoning.encrypted_content"],"tools":[{"type":"web_search"}]}`, "relay.example",
			[]string{"reasoning.encrypted_content", "web_search_call.action.sources"}},
		{"sources once", `{"model":"m","input":"hi","include":["web_search_call.action.sources","web_search_call.action.sources"],"tools":[{"type":"web_search"}]}`, "relay.example",
			[]string{"web_search_call.action.sources"}},
		{"xAI keeps the client's only", `{"model":"m","input":"hi","reasoning":{"effort":"low"},"include":["reasoning.encrypted_content"],"tools":[{"type":"web_search"}]}`, "api.x.ai",
			[]string{"reasoning.encrypted_content"}},
		// OpenAI refuses sealed reasoning of a model that isn't reasoning
		{"no reasoning, no sealed reasoning", `{"model":"m","input":"hi","include":["reasoning.encrypted_content","message.output_text.logprobs"]}`, "relay.example",
			[]string{"message.output_text.logprobs"}},
		{"none", `{"model":"m","input":"hi"}`, "relay.example", nil},
	} {
		if got := include(c.body, c.host); !slices.Equal(got, c.want) {
			t.Errorf("%s: include %q, want %q", c.name, got, c.want)
		}
	}
}

// The way #315 met it: Codex offering web search to a Responses provider
// uni-switch doesn't know searches by itself goes through uni-switch's search, and
// the request its provider gets still asks for sealed reasoning.
func TestCodexSearchKeepsInclude(t *testing.T) {
	f := &fake{t: t, reply: sse(
		`data: {"type":"response.created","response":{"id":"resp_1","status":"in_progress","output":[]}}`,
		`data: {"type":"response.output_item.done","output_index":0,"item":{"type":"message","role":"assistant","content":[{"type":"output_text","text":"sunny"}]}}`,
		`data: {"type":"response.completed","response":{"id":"resp_1","status":"completed","output":[],"usage":{"input_tokens":9,"output_tokens":2,"total_tokens":11}}}`)}
	setup(t, provider.Responses, f)
	code, body := codexPost(t, `{"model":"fake/m1","instructions":"You are Codex","stream":true,"store":false,
	  "include":["reasoning.encrypted_content"],"prompt_cache_key":"thread-1","reasoning":{"effort":"medium"},
	  "tools":[{"type":"web_search"}],"input":[{"type":"message","role":"user","content":[{"type":"input_text","text":"weather?"}]}]}`)
	if code != 200 {
		t.Fatalf("%d %s", code, body)
	}
	var q struct {
		Include  []string `json:"include"`
		CacheKey string   `json:"prompt_cache_key"`
	}
	json.Unmarshal(f.got, &q)
	if !slices.Contains(q.Include, "reasoning.encrypted_content") || q.CacheKey != "thread-1" {
		t.Errorf("provider got %s", f.got)
	}
}

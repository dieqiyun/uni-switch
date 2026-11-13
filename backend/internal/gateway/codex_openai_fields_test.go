package gateway

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// Codex whose provider is named "OpenAI" (signed in, it keeps its built-in
// one and comes through openai_base_url; CC Switch's table is so named with
// its remote compaction on) sends what it sends OpenAI alone: each message's
// internal_chat_message_metadata_passthrough, a call's
// encrypted_function_args, stream_options, configuration_update items. Under
// any other name it leaves them out itself. A Responses relay that checks
// Codex's requests turned them away ("invalid codex request", #292, with
// Anyrouter's gpt-6-astra); a vendor's Responses API gets the request as
// Codex would send it to a provider of its own, everything else as it was.
func TestCodexOpenAIFieldsStayWithOpenAI(t *testing.T) {
	const sent = `{"model":"fake/m1","instructions":"You are Codex","stream":true,"store":false,
	  "include":["reasoning.encrypted_content"],"prompt_cache_key":"thread-1","tool_choice":"auto","parallel_tool_calls":true,
	  "stream_options":{"reasoning_summary_delivery":"sequential_cutoff"},
	  "tools":[{"type":"function","name":"shell","parameters":{"type":"object"}}],
	  "input":[
	    {"type":"message","role":"developer","content":[{"type":"input_text","text":"<permissions>"}],"internal_chat_message_metadata_passthrough":{"content_item_kinds":["developer.permissions"]}},
	    {"type":"message","role":"user","content":[{"type":"input_text","text":"fix the bug"}],"internal_chat_message_metadata_passthrough":{"turn_id":"t1","content_item_kinds":["user.text"]}},
	    {"type":"configuration_update","reasoning":{"effort":"high"}},
	    {"type":"function_call","name":"shell","arguments":"{\"cmd\":\"ls\"}","call_id":"c1","encrypted_function_args":[]},
	    {"type":"function_call_output","call_id":"c1","output":"a.go","internal_chat_message_metadata_passthrough":{"turn_id":"t1"}}
	  ]}`
	openaiOnly := []string{"internal_chat_message_metadata_passthrough", "encrypted_function_args", "stream_options", "configuration_update"}
	f := &fake{t: t, reply: sse(
		`data: {"type":"response.created","response":{"id":"resp_r1","status":"in_progress","output":[]}}`,
		`data: {"type":"response.output_item.done","output_index":0,"item":{"type":"message","role":"assistant","content":[{"type":"output_text","text":"OK"}]}}`,
		`data: {"type":"response.completed","response":{"id":"resp_r1","status":"completed","output":[]}}`)}
	f.refuse = func(body []byte) (int, string) {
		for _, k := range openaiOnly {
			if strings.Contains(string(body), `"`+k+`"`) {
				return 400, `{"error":{"code":null,"message":"invalid codex request (request id: 1)","param":null,"type":"invalid_request_error"}}`
			}
		}
		return 0, ""
	}
	setup(t, provider.Responses, f)
	chatgpt(t, func(w http.ResponseWriter, r *http.Request) {
		t.Error("a vendor's model reached OpenAI")
		w.WriteHeader(400)
	})
	for _, c := range []struct {
		name string
		send func() (int, string)
	}{
		{"signed in", func() (int, string) { return codexPost(t, sent) }},
		{"its own provider", func() (int, string) { return post(t, "/v1/responses", sent) }},
	} {
		code, body := c.send()
		if code != 200 {
			t.Fatalf("%s: %d %s\nrelay got %s", c.name, code, body, f.got)
		}
		var q struct {
			Instructions   string            `json:"instructions"`
			PromptCacheKey string            `json:"prompt_cache_key"`
			Tools          []any             `json:"tools"`
			Input          []json.RawMessage `json:"input"`
		}
		if err := json.Unmarshal(f.got, &q); err != nil {
			t.Fatal(err)
		}
		// the rest as Codex sent it: every other item, in its order
		if q.Instructions != "You are Codex" || q.PromptCacheKey != "thread-1" || len(q.Tools) != 1 || len(q.Input) != 4 {
			t.Fatalf("%s: relay got %s", c.name, f.got)
		}
		for i, want := range []string{`"text":"<permissions>"`, `"text":"fix the bug"`, `"arguments":"{\"cmd\":\"ls\"}"`, `"output":"a.go"`} {
			if !strings.Contains(string(q.Input[i]), want) {
				t.Errorf("%s: item %d: %s, want %s", c.name, i, q.Input[i], want)
			}
		}
	}

	// OpenAI's own API and the ChatGPT backend take them
	for _, p := range []provider.Provider{
		{Responses: "https://api.openai.com/v1"},
		{Account: &provider.Account{Agent: "codex"}},
	} {
		if got := forVendor(p, []byte(sent), false); string(got) != sent {
			t.Errorf("%+v: %s", p, got)
		}
	}
	// another client's stream_options stay
	vendor := provider.Provider{Responses: "https://relay.example/v1"}
	other := `{"model":"m1","input":"hi","stream":true,"stream_options":{"include_obfuscation":false}}`
	if got := forVendor(vendor, []byte(other), false); string(got) != other {
		t.Errorf("another client's: %s", got)
	}
	both := `{"stream_options":{"include_obfuscation":false,"reasoning_summary_delivery":"sequential_cutoff"}}`
	if got := forVendor(vendor, []byte(both), false); string(got) != `{"stream_options":{"include_obfuscation":false}}` {
		t.Errorf("both: %s", got)
	}
}

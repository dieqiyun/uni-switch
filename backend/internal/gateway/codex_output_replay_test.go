package gateway

import (
	"encoding/json"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// copilotRefusesOutputReplay is Copilot's /responses turning away output
// token replay's include, word for word as echo_ts met it on Discord: a
// Codex conversation begun on gpt-6-astra and carried on on a Copilot
// model.
const copilotRefusesOutputReplay = `{"error":{"message":"Invalid value: 'output.encrypted_content'. Supported values are: 'file_search_call.results', 'web_search_call.results', 'web_search_call.action.sources', 'message.input_image.image_url', 'computer_call_output.output.image_url', 'code_interpreter_call.outputs', 'reasoning.encrypted_content', and 'message.output_text.logprobs'.","type":"invalid_request_error","param":"include","code":"invalid_value"}}`

// Codex with output token replay on asks a provider named "OpenAI" (its
// built-in one, which reaches uni-switch signed in) for
// output.encrypted_content, and sends back each earlier message's and
// call's sealed copy in its encrypted_content. Under any other name it
// leaves both out (client.rs: include only when is_openai, and
// clear_encrypted_content on every item but reasoning and compactions). A
// vendor's model gets the request as Codex would have sent it there;
// OpenAI's own API and the ChatGPT backend get it as it came.
func TestCodexOutputReplayStaysWithOpenAI(t *testing.T) {
	const sent = `{"model":"fake/m1","instructions":"You are Codex","stream":true,"store":false,
	  "include":["reasoning.encrypted_content","output.encrypted_content"],"prompt_cache_key":"thread-1","reasoning":{"effort":"medium"},
	  "tools":[{"type":"function","name":"shell","parameters":{"type":"object"}}],
	  "input":[
	    {"type":"message","role":"user","content":[{"type":"input_text","text":"fix the bug"}]},
	    {"type":"reasoning","id":"rs_1","summary":[],"encrypted_content":"gAAAA-astra-reasoning"},
	    {"type":"message","role":"assistant","phase":"commentary","content":[{"type":"output_text","text":"looking"}],"encrypted_content":"gAAAA-astra-commentary"},
	    {"type":"function_call","name":"shell","arguments":"{\"cmd\":\"ls\"}","call_id":"c1","encrypted_content":"gAAAA-astra-call"},
	    {"type":"function_call_output","call_id":"c1","output":"a.go"},
	    {"type":"message","role":"user","content":[{"type":"input_text","text":"go on"}]}
	  ]}`
	f := &fake{t: t, reply: sse(
		`data: {"type":"response.created","response":{"id":"resp_r1","status":"in_progress","output":[]}}`,
		`data: {"type":"response.output_item.done","output_index":0,"item":{"type":"message","role":"assistant","content":[{"type":"output_text","text":"OK"}]}}`,
		`data: {"type":"response.completed","response":{"id":"resp_r1","status":"completed","output":[]}}`)}
	f.refuse = func(body []byte) (int, string) {
		if strings.Contains(string(body), "output.encrypted_content") {
			return 400, copilotRefusesOutputReplay
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
			t.Fatalf("%s: %d %s\nupstream got %s", c.name, code, body, f.got)
		}
		var q struct {
			Include []string         `json:"include"`
			Input   []map[string]any `json:"input"`
		}
		if err := json.Unmarshal(f.got, &q); err != nil {
			t.Fatal(err)
		}
		if !slices.Equal(q.Include, []string{"reasoning.encrypted_content"}) {
			t.Errorf("%s: include %q", c.name, q.Include)
		}
		var calls int
		for _, it := range q.Input {
			switch it["type"] {
			case "reasoning":
				// sealed reasoning still goes; one the vendor can't read is
				// taken out once refused (foreignReasoning)
				if it["encrypted_content"] != "gAAAA-astra-reasoning" {
					t.Errorf("%s: reasoning %v", c.name, it)
				}
			case "function_call":
				calls++
				if _, ok := it["encrypted_content"]; ok || it["arguments"] != `{"cmd":"ls"}` {
					t.Errorf("%s: call %v", c.name, it)
				}
			case "message":
				if _, ok := it["encrypted_content"]; ok {
					t.Errorf("%s: message %v", c.name, it)
				}
			}
		}
		// what was said and done stays
		if calls != 1 || !strings.Contains(string(f.got), `"text":"looking"`) {
			t.Errorf("%s: upstream got %s", c.name, f.got)
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
	// include holding it alone goes without include
	if got := forVendor(provider.Provider{Responses: "https://api.githubcopilot.com"}, []byte(`{"include":["output.encrypted_content"],"input":[]}`), false); string(got) != `{"input":[]}` {
		t.Errorf("alone: %s", got)
	}
}

// A request uni-switch builds again for a Responses upstream asks for output
// token replay only of OpenAI's API and the ChatGPT backend.
func TestResponsesOutputReplayOnlyToOpenAI(t *testing.T) {
	r, err := parseResponses([]byte(`{"model":"m","input":"hi","reasoning":{"effort":"low"},"include":["reasoning.encrypted_content","output.encrypted_content"]}`))
	if err != nil {
		t.Fatal(err)
	}
	for host, want := range map[string][]string{
		"api.githubcopilot.com": {"reasoning.encrypted_content"},
		"relay.example":         {"reasoning.encrypted_content"},
		"api.x.ai":              {"reasoning.encrypted_content"},
		"api.openai.com":        {"reasoning.encrypted_content", "output.encrypted_content"},
		"chatgpt.com":           {"reasoning.encrypted_content", "output.encrypted_content"},
	} {
		var q struct {
			Include []string `json:"include"`
		}
		json.Unmarshal(buildResponses(r, "m", host, false), &q)
		if !slices.Equal(q.Include, want) {
			t.Errorf("%s: include %q, want %q", host, q.Include, want)
		}
	}
}

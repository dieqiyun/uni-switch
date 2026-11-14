package gateway

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// azureStoreless plays Azure OpenAI's Responses as it answers a request
// with store false: an item given by id alone, with nothing sealed in it,
// is looked up among stored items, and there are none, so the whole
// request is turned away with a 400 (#1008's text, word for word; uni-switch names the provider before it).
type azureStoreless struct{ inputs [][]map[string]any }

func (a *azureStoreless) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	var q struct {
		Store *bool            `json:"store"`
		Input []map[string]any `json:"input"`
	}
	json.Unmarshal(body, &q)
	a.inputs = append(a.inputs, q.Input)
	if r.URL.Path != "/openai/v1/responses" || r.Header.Get("api-key") != "azure-key" {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	for _, it := range q.Input {
		enc, _ := it["encrypted_content"].(string)
		if it["type"] == "reasoning" && enc == "" && (q.Store == nil || !*q.Store) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			io.WriteString(w, `{"error":{"code":null,"message":"Item with id '`+it["id"].(string)+`' not found. Items are not persisted when `+"`store`"+` is set to false. Try again with `+"`store`"+` set to true, or remove this item from your input.","param":"input","type":"invalid_request_error"}}`)
			return
		}
	}
	w.Header().Set("Content-Type", "text/event-stream")
	io.WriteString(w, sse(
		`data: {"type":"response.created","response":{"id":"resp_az","status":"in_progress"}}`,
		`data: {"type":"response.output_item.done","output_index":0,"item":{"type":"message","id":"m1","role":"assistant","content":[{"type":"output_text","text":"SUMMARY"}]}}`,
		`data: {"type":"response.completed","response":{"id":"resp_az","status":"completed","output":[],"usage":{"input_tokens":5,"output_tokens":2}}}`))
}

// Codex on an Azure OpenAI deployment of a GPT model, as in the issue:
// earlier turns went out translated (Codex offers web search on every
// turn), and their replies gave Codex reasoning items uni-switch made, an id
// (rs_ and newID) with a summary and nothing sealed. Codex hands them back
// on every turn, and its compaction, which offers no tools, is relayed as
// it is: Azure, keeping nothing, answered "Item with id 'rs_…' not found"
// and Codex reconnected 5 times and gave up. They don't go to Azure, nor to
// OpenAI, which answers the same.
func TestAzureCompactionWithoutUniSwitchsReasoning(t *testing.T) {
	fresh(t)
	up := &azureStoreless{}
	srv := httptest.NewServer(up)
	t.Cleanup(srv.Close)
	p, err := provider.FromPreset(provider.AzurePreset)
	if err != nil {
		t.Fatal(err)
	}
	p.Key = "azure-key"
	p.Chat = srv.URL + "/openai/v1"
	p.Responses = p.Chat
	p.Models = []string{"gpt-5.4"}
	if err := provider.Save(p); err != nil {
		t.Fatal(err)
	}
	const conversation = `{"type":"message","role":"user","content":[{"type":"input_text","text":"fix the login bug"}]},
	  {"type":"reasoning","id":"rs_18dbe693b3dce4800000003c","summary":[{"type":"summary_text","text":"Looking at main.go"}]},
	  {"type":"function_call","id":"fc_1","call_id":"call_1","name":"shell","arguments":"{\"cmd\":\"ls\"}"},
	  {"type":"function_call_output","call_id":"call_1","output":"main.go"},
	  {"type":"reasoning","id":"rs_sealed","summary":[],"encrypted_content":"gAAAA-azure-sealed"},
	  {"type":"message","role":"assistant","content":[{"type":"output_text","text":"I fixed main.go"}]}`
	for _, tc := range []struct{ name, body string }{
		{"compaction", `{"model":"azure/gpt-5.4","stream":true,"store":false,"include":["reasoning.encrypted_content"],"tools":[{"type":"function","name":"shell","parameters":{"type":"object"}}],
		  "input":[` + conversation + `,{"type":"compaction_trigger"}]}`},
		{"a turn relayed as it is", `{"model":"azure/gpt-5.4","stream":true,"store":false,"include":["reasoning.encrypted_content"],
		  "input":[` + conversation + `,{"type":"message","role":"user","content":[{"type":"input_text","text":"now add a test"}]}]}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			up.inputs = nil
			code, body := codexPost(t, tc.body)
			if code != 200 || !strings.Contains(body, "SUMMARY") && !strings.Contains(body, "U1VNTUFSWQ") {
				t.Fatalf("%d %s", code, body)
			}
			if len(up.inputs) != 1 {
				t.Fatalf("asked Azure %d times", len(up.inputs))
			}
			var kept []string
			for _, it := range up.inputs[0] {
				if it["type"] == "reasoning" {
					kept = append(kept, it["id"].(string))
				}
			}
			// Azure's own sealed reasoning still goes; the summary of the
			// rest goes as the conversation it is
			if strings.Join(kept, ",") != "rs_sealed" {
				t.Errorf("reasoning sent: %v", kept)
			}
			b, _ := json.Marshal(up.inputs[0])
			for _, want := range []string{"fix the login bug", "I fixed main.go", "call_1"} {
				if !strings.Contains(string(b), want) {
					t.Errorf("input lacks %q: %s", want, b)
				}
			}
		})
	}
}

// A relay in front of Azure or OpenAI, which uni-switch can't tell from any
// other, answers the summary request with Azure's 400: it is asked again
// with the conversation as text, as a 404 is (#866), and the compaction
// completes.
func TestCodexCompactItemNotFoundAsksAgainAsText(t *testing.T) {
	f := &fake{t: t, reply: sse(
		`data: {"type":"response.output_item.done","output_index":0,"item":{"type":"message","role":"assistant","content":[{"type":"output_text","text":"SUMMARY"}]}}`,
		`data: {"type":"response.completed","response":{"id":"resp_p","status":"completed","output":[]}}`)}
	f.refuse = func(body []byte) (int, string) {
		if strings.Contains(string(body), `"rs_1"`) {
			return 400, `{"error":{"code":null,"message":"Item with id 'rs_1' not found. Items are not persisted when ` + "`store`" + ` is set to false.","param":null,"type":"invalid_request_error"}}`
		}
		return 0, ""
	}
	setup(t, provider.Responses, f)
	code, body := codexPost(t, compact404Request)
	if code != 200 {
		t.Fatalf("%d %s", code, body)
	}
	if got := compactedText(t, body); got != "SUMMARY" {
		t.Errorf("summary %q", got)
	}
	if !strings.Contains(string(f.got), "fix the login bug") || strings.Contains(string(f.got), `"rs_1"`) {
		t.Errorf("asked again with: %s", f.got)
	}
}

// Any other 400 still goes back to Codex as it came.
func TestCodexCompactOther400Stays(t *testing.T) {
	f := &fake{t: t, refuse: func([]byte) (int, string) {
		return 400, `{"error":{"message":"Invalid value for 'reasoning.effort'","type":"invalid_request_error"}}`
	}}
	setup(t, provider.Responses, f)
	code, body := codexPost(t, compact404Request)
	if code != 400 || f.calls != 1 {
		t.Errorf("%d %s (calls %d)", code, body, f.calls)
	}
}

package gateway

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// A request that carries a hosted web search — Messages'
// web_search_20250305, Chat's web_search_options, Responses' web_search —
// for a model that can't search by itself is answered through searchReply's
// rounds. A call the model wrote into its text (#823) reached the client
// as that text there, while every route without a search made it a call:
// a search route's replies are the only ones the recognizer never saw
// (#4562).
func TestTextWrittenCallSearchRouted(t *testing.T) {
	search := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/models":
			io.WriteString(w, `{"data":[{"id":"claude-haiku-4-5"}]}`)
		case "/v1/messages":
			w.Header().Set("Content-Type", "application/json")
			io.WriteString(w, `{"id":"s","type":"message","role":"assistant","model":"claude-haiku-4-5","content":[
				{"type":"server_tool_use","id":"srv","name":"web_search","input":{"query":"go"}},
				{"type":"web_search_tool_result","tool_use_id":"srv","content":[{"type":"web_search_result","title":"Go downloads","url":"https://go.dev/dl/"}]},
				{"type":"text","text":"The latest Go is 1.27.1."}],"stop_reason":"end_turn","usage":{"input_tokens":5,"output_tokens":5}}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer search.Close()
	model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, sse(chunkOf("Let me look.\n\n"+issueDSML[:40]), chunkOf(issueDSML[40:]), stopChunk, `data: [DONE]`))
	}))
	defer model.Close()

	fresh(t)
	hosts := searchHosts[provider.Anthropic]
	searchHosts[provider.Anthropic] = append(hosts, provider.HostOf(search.URL))
	defer func() { searchHosts[provider.Anthropic] = hosts }()
	for _, p := range []provider.Provider{
		{ID: "srch", Name: "Search", Key: "k", Anthropic: search.URL},
		{ID: "deep", Name: "Deep", Key: "k", Chat: model.URL, Models: []string{"deep-chat"}},
	} {
		if err := provider.Save(p); err != nil {
			t.Fatal(err)
		}
	}
	p, _ := provider.Find("srch")
	if _, err := p.Fetch(t.Context()); err != nil {
		t.Fatal(err)
	}

	for _, c := range []struct {
		name, path, body string
	}{
		{"messages", "/v1/messages", `{"model":"deep/deep-chat","max_tokens":1000,"stream":false,"messages":[{"role":"user","content":"What is the latest Go?"}],
			"tools":[{"name":"Bash","description":"run","input_schema":{"type":"object","properties":{"command":{"type":"string"}}}},{"type":"web_search_20250305","name":"web_search","max_uses":8}]}`},
		{"chat", "/v1/chat/completions", `{"model":"deep/deep-chat","stream":false,"messages":[{"role":"user","content":"What is the latest Go?"}],` + issueTools + `,"web_search_options":{}}`},
		{"responses", "/v1/responses", `{"model":"deep/deep-chat","stream":false,"input":"What is the latest Go?","tools":[{"type":"function","name":"Bash","parameters":{"type":"object","properties":{"command":{"type":"string"}}}},{"type":"web_search"}]}`},
	} {
		t.Run(c.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			New().Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodPost, c.path, strings.NewReader(c.body)))
			raw := rec.Body.String()
			if rec.Code != 200 {
				t.Fatalf("%d %s", rec.Code, raw)
			}
			if !strings.Contains(raw, "Let me look.") || strings.Contains(raw, "DSML") || strings.Contains(raw, "tool_call>") || strings.Contains(raw, "parameter") {
				t.Fatalf("the text around the call was lost, or the markup handed on: %s", raw)
			}
			var call map[string]any
			switch c.name {
			case "messages":
				var got struct {
					Content []struct {
						Type  string `json:"type"`
						Name  string `json:"name"`
						Input map[string]any
					} `json:"content"`
					StopReason string `json:"stop_reason"`
				}
				if json.Unmarshal(rec.Body.Bytes(), &got) != nil {
					t.Fatalf("%s", raw)
				}
				if got.StopReason != "tool_use" {
					t.Fatalf("stop_reason %q: %s", got.StopReason, raw)
				}
				for _, b := range got.Content {
					if b.Type == "tool_use" {
						call = map[string]any{"name": b.Name, "input": b.Input}
					}
				}
			case "chat":
				var got struct {
					Choices []struct {
						Message struct {
							ToolCalls []wholeCall `json:"tool_calls"`
						} `json:"message"`
						FinishReason string `json:"finish_reason"`
					} `json:"choices"`
				}
				if json.Unmarshal(rec.Body.Bytes(), &got) != nil || len(got.Choices) != 1 {
					t.Fatalf("%s", raw)
				}
				if got.Choices[0].FinishReason != "tool_calls" {
					t.Fatalf("finish_reason %q: %s", got.Choices[0].FinishReason, raw)
				}
				if len(got.Choices[0].Message.ToolCalls) == 1 {
					tc := got.Choices[0].Message.ToolCalls[0]
					var args any
					json.Unmarshal([]byte(tc.Function.Arguments), &args)
					call = map[string]any{"name": tc.Function.Name, "input": args}
				}
			case "responses":
				var got struct {
					Output []struct {
						Type      string `json:"type"`
						Name      string `json:"name"`
						Arguments string `json:"arguments"`
					} `json:"output"`
					Status string `json:"status"`
				}
				if json.Unmarshal(rec.Body.Bytes(), &got) != nil {
					t.Fatalf("%s", raw)
				}
				if got.Status != "completed" {
					t.Fatalf("status %q: %s", got.Status, raw)
				}
				for _, it := range got.Output {
					if it.Type == "function_call" {
						var args any
						json.Unmarshal([]byte(it.Arguments), &args)
						call = map[string]any{"name": it.Name, "input": args}
					}
				}
			}
			var want any
			json.Unmarshal([]byte(`{"command":"ls /tmp"}`), &want)
			if call == nil || call["name"] != "Bash" {
				t.Fatalf("the call written into the text reached the client as text: %s", raw)
			}
			got, err := json.Marshal(call["input"])
			var input any
			json.Unmarshal(got, &input)
			if err != nil || !jsonEqual(want, input) {
				t.Fatalf("call arguments %s: %s", call["input"], raw)
			}
		})
	}
}

// A round whose events end without its stop (searchRounds takes such a
// round as ended) still hands on the text textCallRound held from a mark
// begun: it is the reply's, not lost with the channel (#4562 review).
func TestTextCallRoundReleasesHeldAtEnd(t *testing.T) {
	text := "Let me look.\n\n" + issueDSML[:40]
	ask := func(ctx context.Context, req *Request) (<-chan Event, int, string) {
		in := make(chan Event, 1)
		in <- Event{Kind: KText, Text: text}
		close(in)
		return in, 200, ""
	}
	out, _, _ := textCallRound([]Tool{{Name: "Bash"}}, ask)(t.Context(), &Request{})
	got := ""
	for ev := range out {
		if ev.Kind == KText {
			got += ev.Text
		}
	}
	if got != text {
		t.Fatalf("a round ended without its stop handed on %q, not %q", got, text)
	}
}

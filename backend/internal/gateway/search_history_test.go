package gateway

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// searchedHistory is a conversation that searched the web, its items the
// ChatGPT backend's own (gpt-6.1-sol, a ChatGPT Pro account, 2026-10-09):
// a commentary message, the web_search_call, the answer citing what it
// found.
const searchedHistory = `{"type":"message","role":"user","content":[{"type":"input_text","text":"Use web search: what is the newest Go release announced on go.dev? Answer in one line."}]},` +
	`{"id":"msg_0c7e789f31e92725016ac8f910d47087d0a126fed4f3afdc86","type":"message","status":"completed","content":[{"type":"output_text","annotations":[],"logprobs":[],"text":"I’ll check go.dev for the latest release announcement.\n"}],"phase":"commentary","role":"assistant"},` +
	`{"id":"ws_0c7e789f31e92725016ac8f91213cc87d0ac350b2fd39a94a2","type":"web_search_call","status":"completed","action":{"type":"search","queries":["site:go.dev newest Go release October 2026 release"],"query":"site:go.dev newest Go release October 2026 release"}},` +
	`{"id":"msg_0c7e789f31e92725016ac8f915990c87d0987c34e2e7b8b409","type":"message","status":"completed","content":[{"type":"output_text","annotations":[{"type":"url_citation","end_index":152,"start_index":90,"title":"Release History - The Go Programming Language","url":"https://go.dev/doc/devel/release?utm_source=openai"}],"logprobs":[],"text":"The newest Go release listed on go.dev is **Go 1.27.2**, released on **October 8, 2026**. ([go.dev](https://go.dev/doc/devel/release?utm_source=openai))"}],"phase":"final_answer","role":"assistant"}`

// protectedSearch stands in for the ChatGPT backend as it answered that
// history on 2026-10-09: replayed without the web_search tool declared,
// HTTP 200 and only this error event; declared (in the tools, or in an
// additional_tools item), a reply. A Lite request with web_search among
// its top-level tools is refused 400, as the backend refused it. A request
// declaring both Codex's web.run and the hosted web_search is refused as
// Savin was (Discord, Codex 0.163.0-alpha); web.run alone is taken as the
// conversation's search, as Codex sends it so to the backend itself. It
// keeps the bodies it was sent.
func protectedSearch(t *testing.T) *[][]byte {
	t.Helper()
	var mu sync.Mutex
	got := &[][]byte{}
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		*got = append(*got, b)
		mu.Unlock()
		var q struct {
			Input []struct {
				Type  string           `json:"type"`
				Tools []map[string]any `json:"tools"`
			} `json:"input"`
			Tools []map[string]any `json:"tools"`
		}
		json.Unmarshal(b, &q)
		searched, declared, compact, webRun := false, false, false, false
		isWebRun := func(tl map[string]any) bool {
			return tl["type"] == "namespace" && tl["name"] == "web"
		}
		for _, it := range q.Input {
			searched = searched || it.Type == "web_search_call"
			compact = compact || it.Type == "compaction_trigger"
			for _, tl := range it.Tools {
				declared = declared || tl["type"] == "web_search"
				webRun = webRun || isWebRun(tl)
			}
		}
		for _, tl := range q.Tools {
			webRun = webRun || isWebRun(tl)
		}
		for _, tl := range q.Tools {
			if tl["type"] == "web_search" && strings.EqualFold(r.Header.Get("X-OpenAI-Internal-Codex-Responses-Lite"), "true") {
				w.WriteHeader(400)
				io.WriteString(w, `{"error":{"message":"X-OpenAI-Internal-Codex-Responses-Lite only supports function tools, custom tools, and client-executed tool search.","type":"invalid_request_error","param":"tools","code":null}}`)
				return
			}
			declared = declared || tl["type"] == "web_search"
		}
		if webRun && declared {
			w.WriteHeader(400)
			io.WriteString(w, `{"error":{"message":"The native ChatGPT backend does not allow a single request to contain both the client-declared function web.run and the hosted tool web_search.","type":"invalid_request_error","param":"tools","code":null}}`)
			return
		}
		if searched && !declared && !webRun {
			io.WriteString(w, "event: error\ndata: {\"type\":\"error\",\"error\":{\"message\":\"response protection is unavailable\",\"type\":\"internal_error\",\"param\":null,\"code\":null}}\n\n")
			return
		}
		item := `{"type":"message","role":"assistant","content":[{"type":"output_text","text":"Go 1.27.2 is the newest release."}]}`
		if compact {
			item = `{"type":"compaction","encrypted_content":"gAAAAABsealed-by-the-backend"}`
		}
		io.WriteString(w, sse(
			`data: {"type":"response.created","response":{"id":"resp_1","model":"gpt-6.1-sol"}}`,
			`data: {"type":"response.output_item.done","output_index":0,"item":`+item+`}`,
			`data: {"type":"response.completed","response":{"id":"resp_1","usage":{"input_tokens":9,"output_tokens":2}}}`))
	}))
	t.Cleanup(up.Close)
	was := provider.CodexBase
	provider.CodexBase = up.URL + "/backend-api/codex"
	t.Cleanup(func() { provider.CodexBase = was })
	return got
}

// sentTools is what a request sent upstream declared: its tools' types,
// its additional_tools items' tools' types, and its tool_choice.
func sentTools(t *testing.T, b []byte) (tools, additional []string, choice any, last string) {
	t.Helper()
	var q struct {
		Input []struct {
			Type  string           `json:"type"`
			Tools []map[string]any `json:"tools"`
		} `json:"input"`
		Tools      []map[string]any `json:"tools"`
		ToolChoice any              `json:"tool_choice"`
	}
	if err := json.Unmarshal(b, &q); err != nil {
		t.Fatalf("%v: %s", err, b)
	}
	for _, tl := range q.Tools {
		tools = append(tools, tl["type"].(string))
	}
	for _, it := range q.Input {
		for _, tl := range it.Tools {
			additional = append(additional, tl["type"].(string))
		}
	}
	if n := len(q.Input); n > 0 {
		last = q.Input[n-1].Type
	}
	return tools, additional, q.ToolChoice, last
}

// #1270 (xjdata, Codex CLI 0.162.0 on a custom provider, codex/gpt-6-astra):
// Codex compacts a custom provider's conversation itself, a plain request
// with tools: [] and no compaction_trigger, so uni-switch's compaction never
// sees it. A conversation that searched the web failed so every time; one
// that didn't was answered.
func TestSearchedConversationCompactsOnAnAccount(t *testing.T) {
	codexSignedIn(t)
	got := protectedSearch(t)
	code, body := post(t, "/v1/responses", `{"model":"codex/gpt-6-astra","stream":true,"store":false,"tools":[],"tool_choice":"auto","parallel_tool_calls":false,"input":[`+
		searchedHistory+`,{"type":"message","role":"user","content":[{"type":"input_text","text":"You are performing a CONTEXT CHECKPOINT COMPACTION. Create a handoff summary for another LLM that will resume the task."}]}]}`)
	if code != 200 || strings.Contains(body, "response protection") || !strings.Contains(body, "response.completed") {
		t.Fatalf("status %d: %s", code, body)
	}
	tools, _, choice, _ := sentTools(t, (*got)[len(*got)-1])
	if len(tools) != 1 || tools[0] != "web_search" || choice != "none" {
		t.Errorf("sent tools %v, tool_choice %v: want the web_search tool and none", tools, choice)
	}
	if !strings.Contains(string((*got)[len(*got)-1]), `"external_web_access":false`) {
		t.Errorf("the declared search may fetch live: %s", (*got)[len(*got)-1])
	}
}

// Codex's remote compaction of a uni-switch model on the ChatGPT accounts:
// the backend writes the summary from the history as it came, once,
// rather than refusing it and leaving uni-switch to ask again as text.
func TestSearchedConversationCompactionTrigger(t *testing.T) {
	codexSignedIn(t)
	got := protectedSearch(t)
	code, body := codexPost(t, `{"model":"codex/gpt-6.1-sol","stream":true,"store":false,"tools":[],"input":[`+searchedHistory+`,{"type":"compaction_trigger"}]}`)
	if code != 200 || !strings.Contains(body, `"type":"compaction"`) {
		t.Fatalf("status %d: %s", code, body)
	}
	if len(*got) != 1 {
		t.Fatalf("asked %d times, not once", len(*got))
	}
	tools, _, choice, _ := sentTools(t, (*got)[0])
	if len(tools) != 1 || tools[0] != "web_search" || choice != "none" {
		t.Errorf("sent tools %v, tool_choice %v", tools, choice)
	}
}

// A turn with Codex's function tools but not web search (search turned
// off since the conversation searched) keeps its tools callable as asked.
func TestSearchedConversationFunctionTools(t *testing.T) {
	codexSignedIn(t)
	got := protectedSearch(t)
	code, body := post(t, "/v1/responses", `{"model":"codex/gpt-6.1-sol","stream":true,"tools":[{"type":"function","name":"shell","parameters":{"type":"object","properties":{}}}],"tool_choice":"auto","input":[`+searchedHistory+`]}`)
	if code != 200 || strings.Contains(body, "response protection") {
		t.Fatalf("status %d: %s", code, body)
	}
	tools, _, choice, _ := sentTools(t, (*got)[len(*got)-1])
	if strings.Join(tools, ",") != "function,web_search" || choice != "auto" {
		t.Errorf("sent tools %v, tool_choice %v", tools, choice)
	}
}

// Codex's own model on Codex's own sign-in, relayed as it came: a
// thread's description asked with the searched conversation and no tools
// (congee949's thread_description failures), and a Responses Lite
// compaction, whose tools go as an additional_tools item.
func TestSearchedConversationOnCodexSignIn(t *testing.T) {
	setup(t, provider.Chat, &fake{t: t})
	got := protectedSearch(t)
	code, body := codexPost(t, `{"model":"gpt-6-luna","stream":true,"store":false,"tools":[],"tool_choice":"auto","input":[`+searchedHistory+`,{"type":"message","role":"user","content":[{"type":"input_text","text":"Describe this thread."}]}]}`)
	if code != 200 || strings.Contains(body, "response protection") {
		t.Fatalf("status %d: %s", code, body)
	}
	if tools, _, choice, _ := sentTools(t, (*got)[0]); len(tools) != 1 || tools[0] != "web_search" || choice != "none" {
		t.Errorf("sent tools %v, tool_choice %v", tools, choice)
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", CodexPath+"/responses", strings.NewReader(`{"model":"gpt-6.1-sol","stream":true,"store":false,"parallel_tool_calls":false,"reasoning":{"effort":"medium","context":"all_turns"},"input":[`+searchedHistory+`,{"type":"compaction_trigger"}]}`))
	req.Header.Set("Authorization", "Bearer chatgpt-token")
	req.Header.Set("chatgpt-account-id", "acct-1")
	req.Header.Set("X-OpenAI-Internal-Codex-Responses-Lite", "true")
	New().Handler().ServeHTTP(rec, req)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"type":"compaction"`) {
		t.Fatalf("Lite: status %d: %s", rec.Code, rec.Body.String())
	}
	tools, additional, choice, last := sentTools(t, (*got)[1])
	if len(tools) != 0 || strings.Join(additional, ",") != "web_search" || choice != "none" || last != "compaction_trigger" {
		t.Errorf("Lite sent tools %v, additional %v, tool_choice %v, last item %s", tools, additional, choice, last)
	}
}

// webRunTool is Codex's web.run as it declares it (codex-rs/ext/web-search
// tool.rs): a function in the "web" namespace, which Codex offers in place
// of the hosted web_search on a Responses Lite model or with its standalone
// web search on, and runs through /alpha/search.
const webRunTool = `{"type":"namespace","name":"web","description":"Tools in the web namespace.","tools":[{"type":"function","name":"run","description":"Search the web.","strict":false,"parameters":{"type":"object","properties":{"search_query":{"type":"array"}}}}]}`

// Savin on Discord (Codex 0.163.0-alpha.10.2, signed in with ChatGPT):
// a conversation that had searched with the hosted tool went on under
// Codex's web.run, and uni-switch declared the hosted web_search for the
// replayed search (#1270) beside it, which the backend refuses. web.run is
// the conversation's search: the turn goes as Codex sent it, on Codex's
// own sign-in and on a ChatGPT account in uni-switch, Lite or not.
func TestSearchedConversationUnderWebRun(t *testing.T) {
	codexSignedIn(t)
	got := protectedSearch(t)
	lite := `{"model":"gpt-6.1-sol","stream":true,"store":false,"parallel_tool_calls":false,"reasoning":{"effort":"medium","context":"all_turns"},"input":[{"type":"additional_tools","role":"developer","tools":[` + webRunTool + `]},` + searchedHistory + `]}`
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", CodexPath+"/responses", strings.NewReader(lite))
	req.Header.Set("Authorization", "Bearer chatgpt-token")
	req.Header.Set("chatgpt-account-id", "acct-1")
	req.Header.Set("X-OpenAI-Internal-Codex-Responses-Lite", "true")
	New().Handler().ServeHTTP(rec, req)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "response.completed") {
		t.Fatalf("Lite on Codex's sign-in: status %d: %s", rec.Code, rec.Body.String())
	}
	if sent := (*got)[len(*got)-1]; string(sent) != lite {
		t.Errorf("Lite on Codex's sign-in relayed changed:\n%s\n%s", lite, sent)
	}

	for _, in := range []string{
		`{"model":"codex/gpt-6.1-sol","stream":true,"reasoning":{"effort":"medium","context":"all_turns"},"input":[{"type":"additional_tools","role":"developer","tools":[` + webRunTool + `]},` + searchedHistory + `]}`,
		`{"model":"codex/gpt-5.5","stream":true,"tools":[{"type":"function","name":"shell","parameters":{"type":"object","properties":{}}},` + webRunTool + `],"tool_choice":"auto","input":[` + searchedHistory + `]}`,
	} {
		code, body := post(t, "/v1/responses", in)
		if code != 200 || !strings.Contains(body, "response.completed") {
			t.Fatalf("on an account: status %d: %s", code, body)
		}
		tools, additional, _, _ := sentTools(t, (*got)[len(*got)-1])
		for _, typ := range append(tools, additional...) {
			if typ == "web_search" {
				t.Errorf("web_search declared beside web.run: tools %v, additional %v", tools, additional)
			}
		}
	}
}

// Nothing changes for a conversation that didn't search, or one whose
// request declares search already.
func TestUnsearchedConversationUnchanged(t *testing.T) {
	setup(t, provider.Chat, &fake{t: t})
	got := protectedSearch(t)
	for _, in := range []string{
		`{"model":"gpt-6-luna","stream":true,"tools":[],"input":[{"type":"message","role":"user","content":[{"type":"input_text","text":"hi"}]}]}`,
		`{"model":"gpt-6-luna","stream":true,"tools":[{"type":"web_search","external_web_access":true}],"tool_choice":"auto","input":[` + searchedHistory + `]}`,
	} {
		if code, body := codexPost(t, in); code != 200 {
			t.Fatalf("status %d: %s", code, body)
		}
		if sent := (*got)[len(*got)-1]; string(sent) != in {
			t.Errorf("relayed changed:\n%s\n%s", in, sent)
		}
	}
}

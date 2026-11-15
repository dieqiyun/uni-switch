package gateway

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// deepSeekCustomRefusal is DeepSeek's Responses API turning Codex's exec
// away, the message as #2656's reporter got it from api.deepseek.com.
const deepSeekCustomRefusal = `{"error":{"message":"Unsupported custom tool: 'exec'. Only 'apply_patch' is supported.","type":"invalid_request_error","param":null,"code":null}}`

// customExecRequest is #2656's request: Codex's exec offered as a custom
// tool, nothing called yet.
const customExecRequest = `{"model":%q,"stream":%t,"input":[{"role":"user","content":"Return ok."}],` +
	`"tools":[{"type":"custom","name":"exec","description":"Execute source text.","format":{"type":"text"}}]}`

// customUpstream is a Responses API that, like DeepSeek's, takes no custom
// tool but apply_patch, and calls exec when it is offered as a function.
type customUpstream struct {
	mu    sync.Mutex
	asked []map[string]any
	paths []string
}

func (u *customUpstream) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	b, _ := io.ReadAll(r.Body)
	var q map[string]any
	json.Unmarshal(b, &q)
	u.mu.Lock()
	u.asked = append(u.asked, q)
	u.paths = append(u.paths, r.URL.Path)
	u.mu.Unlock()
	tools, _ := q["tools"].([]any)
	for _, t := range tools {
		if tm, _ := t.(map[string]any); tm["type"] == "custom" && tm["name"] != "apply_patch" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(400)
			io.WriteString(w, deepSeekCustomRefusal)
			return
		}
	}
	call := `{"type":"function_call","id":"fc_1","call_id":"call_1","name":"exec","arguments":"{\"input\":\"print(1)\"}","status":"completed"}`
	if q["stream"] == true {
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, sse(
			`data: {"type":"response.created","response":{"id":"r1","status":"in_progress","output":[]}}`,
			`data: {"type":"response.output_item.added","output_index":0,"item":{"type":"function_call","id":"fc_1","call_id":"call_1","name":"exec","arguments":""}}`,
			`data: {"type":"response.function_call_arguments.delta","output_index":0,"item_id":"fc_1","delta":"{\"input\":\"print(1)\"}"}`,
			`data: {"type":"response.output_item.done","output_index":0,"item":`+call+`}`,
			`data: {"type":"response.completed","response":{"id":"r1","status":"completed","output":[`+call+`],"usage":{"input_tokens":9,"output_tokens":3}}}`))
		return
	}
	w.Header().Set("Content-Type", "application/json")
	io.WriteString(w, `{"id":"r1","object":"response","status":"completed","output":[`+call+`],"usage":{"input_tokens":9,"output_tokens":3}}`)
}

// A Responses API that takes no custom tool (DeepSeek's, #2656) turned
// every Codex turn offering exec away, and uni-switch gave the 400 back. The
// turn is now translated on the same API: exec offered as a function
// taking its input, the call back to Codex as the custom_tool_call it
// runs, exec's name kept. The next turn goes translated at once, and an
// upstream that takes custom tools still gets them as sent.
func TestCustomToolTranslatedWhereRefused(t *testing.T) {
	for _, stream := range []bool{false, true} {
		t.Run(fmt.Sprintf("stream %t", stream), func(t *testing.T) {
			fresh(t)
			noRests()
			t.Cleanup(noRests)
			refusing := &customUpstream{}
			ds := httptest.NewServer(refusing)
			t.Cleanup(ds.Close)
			var taking []map[string]any
			var mu sync.Mutex
			lax := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				b, _ := io.ReadAll(r.Body)
				var q map[string]any
				json.Unmarshal(b, &q)
				mu.Lock()
				taking = append(taking, q)
				mu.Unlock()
				answer(w, r)
			}))
			t.Cleanup(lax.Close)
			for _, p := range []provider.Provider{
				// Chat and Responses both, as DeepSeek's preset has them
				{ID: "ds", Name: "DeepSeek", Key: "k", Models: []string{"deepseek-flash"}, Chat: ds.URL + "/v1", Responses: ds.URL + "/v1"},
				{ID: "lax", Name: "Lax", Key: "k", Models: []string{"m1"}, Responses: lax.URL + "/v1"},
			} {
				if err := provider.Save(p); err != nil {
					t.Fatal(err)
				}
			}
			s := New()
			ask := func(model string) *httptest.ResponseRecorder {
				rec := httptest.NewRecorder()
				s.Handler().ServeHTTP(rec, httptest.NewRequest("POST", "/v1/responses", strings.NewReader(fmt.Sprintf(customExecRequest, model, stream))))
				return rec
			}
			rec := ask("ds/deepseek-flash")
			if rec.Code != 200 {
				t.Fatalf("ds: %d %s", rec.Code, rec.Body)
			}
			if len(refusing.asked) != 2 || refusing.paths[1] != "/v1/responses" {
				t.Fatalf("ds asked %d times, at %v", len(refusing.asked), refusing.paths)
			}
			tool, _ := refusing.asked[1]["tools"].([]any)[0].(map[string]any)
			props, _ := tool["parameters"].(map[string]any)["properties"].(map[string]any)
			if tool["type"] != "function" || tool["name"] != "exec" || props["input"] == nil {
				t.Fatalf("asked again with %v", tool)
			}
			var call map[string]any
			if stream {
				for _, e := range events(rec.Body.String()) {
					if e["type"] == "response.output_item.done" {
						call, _ = e["item"].(map[string]any)
					}
				}
			} else {
				var out struct{ Output []map[string]any }
				json.Unmarshal(rec.Body.Bytes(), &out)
				for _, it := range out.Output {
					if it["type"] != "reasoning" {
						call = it
					}
				}
			}
			if call["type"] != "custom_tool_call" || call["name"] != "exec" || call["input"] != "print(1)" || call["call_id"] != "call_1" {
				t.Fatalf("Codex got %v\n%s", call, rec.Body)
			}

			// the next turn goes translated at once
			if rec = ask("ds/deepseek-flash"); rec.Code != 200 || len(refusing.asked) != 3 {
				t.Fatalf("next turn: %d, asked %d times: %s", rec.Code, len(refusing.asked), rec.Body)
			}
			if tool, _ := refusing.asked[2]["tools"].([]any)[0].(map[string]any); tool["type"] != "function" {
				t.Fatalf("next turn offered %v", tool)
			}

			// an upstream that takes them gets them as sent
			if rec = ask("lax/m1"); rec.Code != 200 || len(taking) != 1 {
				t.Fatalf("lax: %d, asked %d times", rec.Code, len(taking))
			}
			if tool, _ := taking[0]["tools"].([]any)[0].(map[string]any); tool["type"] != "custom" {
				t.Fatalf("lax offered %v", tool)
			}
		})
	}
}

// The refusal is known as DeepSeek words it, decoded or not, and as a
// vendor may name the type; a 400 over something else about a tool is
// not taken for one. A group's next member is asked over it where it
// reaches the group untranslated.
func TestCustomToolRefusalWordings(t *testing.T) {
	known := []string{
		deepSeekCustomRefusal,
		`{"error":{"message":"Unsupported custom tool: 'exec'. Only 'apply_patch' is supported."}}`,
		`{"error":{"message":"DeepSeek: Unsupported custom tool: 'exec'. Only 'apply_patch' is supported.","type":"invalid_request_error"}}`,
		`{"error":{"message":"Invalid tool type: 'custom'"}}`,
		`{"error":{"message":"custom tools are not supported"}}`,
		// xAI 422s the type with Rust's own wording (#3392, kongkongklng),
		// in an error body and bare, as uni-switch reads it off a reply's words
		`{"error":{"message":"xAI: Failed to deserialize the JSON body into the target type: tools[6].type: unknown variant ` + "`custom`" + `, expected one of ` + "`function`" + `, ` + "`web_search`" + `","type":"invalid_request_error"}}`,
		`Failed to deserialize the JSON body into the target type: tools[6].type: unknown variant ` + "`custom`" + `, expected one of ` + "`function`" + `, ` + "`shell`" + `, ` + "`tool_search`" + ``,
	}
	for _, b := range known {
		if !customToolRefusal.Match(wordsOf([]byte(b))) {
			t.Errorf("not known: %s", b)
		}
		if !shapeRefused(400, []byte(b)) {
			t.Errorf("no next member over: %s", b)
		}
	}
	for _, b := range []string{
		`{"error":{"message":"Invalid schema for function 'exec': 'input' is required"}}`,
		`{"error":{"message":"tools[0].function.strict is not supported"}}`,
		`{"error":{"message":"Insufficient Balance"}}`,
		// a variant other than the type itself says nothing of custom tools
		`{"error":{"message":"tools[2].type: unknown variant ` + "`web_search_2025`" + `"}}`,
	} {
		if customToolRefusal.Match([]byte(b)) {
			t.Errorf("taken for one: %s", b)
		}
	}
}

package gateway

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
	"github.com/dieqiyun/uni-switch/backend/internal/usage"
)

// what the Anthropic API streams of a search its model ran by itself, as
// Anthropic documents it and DeepSeek's Anthropic API answers (#669)
var deepSeekNativeStream = sse(
	`event: message_start`+"\n"+`data: {"type":"message_start","message":{"id":"m","type":"message","role":"assistant","model":"deepseek-flash","content":[],"usage":{"input_tokens":4672,"output_tokens":1}}}`,
	`event: content_block_start`+"\n"+`data: {"type":"content_block_start","index":0,"content_block":{"type":"server_tool_use","id":"call_00_x","name":"web_search","input":{}}}`,
	`event: content_block_delta`+"\n"+`data: {"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"{\"query\":\"kangaroo orbit 7781\"}"}}`,
	`event: content_block_stop`+"\n"+`data: {"type":"content_block_stop","index":0}`,
	`event: content_block_start`+"\n"+`data: {"type":"content_block_start","index":1,"content_block":{"type":"web_search_tool_result","tool_use_id":"call_00_x","content":[{"type":"web_search_result","title":"t","url":"https://example.com/","encrypted_content":"SEALED"}]}}`,
	`event: content_block_stop`+"\n"+`data: {"type":"content_block_stop","index":1}`,
	`event: content_block_start`+"\n"+`data: {"type":"content_block_start","index":2,"content_block":{"type":"text","text":""}}`,
	`event: content_block_delta`+"\n"+`data: {"type":"content_block_delta","index":2,"delta":{"type":"text_delta","text":"Found it."}}`,
	`event: content_block_stop`+"\n"+`data: {"type":"content_block_stop","index":2}`,
	`event: message_delta`+"\n"+`data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":825,"server_tool_use":{"web_search_requests":1}}}`,
	`event: message_stop`+"\n"+`data: {"type":"message_stop"}`)

// two searches on a Responses API: each item comes added and done, and
// the end lists them again
var responsesNativeStream = sse(
	`data: {"type":"response.created","response":{"id":"r1","model":"deepseek-flash"}}`,
	`data: {"type":"response.output_item.added","output_index":0,"item":{"id":"ws_1","type":"web_search_call","status":"in_progress"}}`,
	`data: {"type":"response.output_item.done","output_index":0,"item":{"id":"ws_1","type":"web_search_call","status":"completed","action":{"type":"search","query":"kangaroo orbit 7781"}}}`,
	`data: {"type":"response.output_item.added","output_index":1,"item":{"id":"ws_2","type":"web_search_call","status":"in_progress"}}`,
	`data: {"type":"response.output_item.done","output_index":1,"item":{"id":"ws_2","type":"web_search_call","status":"completed","action":{"type":"search","query":"kangaroo orbit 7782"}}}`,
	`data: {"type":"response.output_text.delta","delta":"Found it."}`,
	`data: {"type":"response.completed","response":{"id":"r1","status":"completed","output":[{"id":"ws_1","type":"web_search_call","status":"completed"},{"id":"ws_2","type":"web_search_call","status":"completed"},{"type":"message","role":"assistant","content":[{"type":"output_text","text":"Found it."}]}],"usage":{"input_tokens":4000,"output_tokens":20}}}`)

const responsesNative = `{"id":"r1","object":"response","status":"completed","model":"deepseek-flash","output":[
	{"id":"ws_1","type":"web_search_call","status":"completed","action":{"type":"search","query":"kangaroo orbit 7781"}},
	{"id":"ws_2","type":"web_search_call","status":"completed","action":{"type":"search","query":"kangaroo orbit 7782"}},
	{"type":"message","role":"assistant","content":[{"type":"output_text","text":"Found it."}]}],
	"usage":{"input_tokens":4000,"output_tokens":20}}`

// cybercaius on Discord: with uni-switch's search off, DeepSeek searched by
// itself and Usage showed nothing of it. A search a provider's model runs
// with its provider's own tool, which its reply shows (Anthropic's
// server_tool_use, the Responses API's web_search_call), is a Web search
// row of the request log under that provider and model, streamed or
// whole, once per search however often the reply names it. The query is
// in no such row, and a reply that searched nothing writes none.
func TestOwnProviderSearchesAreLogged(t *testing.T) {
	fresh(t)
	searchless := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/models") {
			io.WriteString(w, `{"data":[{"id":"deepseek-flash"}]}`)
			return
		}
		var q struct {
			Stream bool `json:"stream"`
		}
		b, _ := io.ReadAll(r.Body)
		json.Unmarshal(b, &q)
		switch {
		case searchless && strings.HasSuffix(r.URL.Path, "/messages"):
			w.Header().Set("Content-Type", "application/json")
			io.WriteString(w, `{"id":"m","type":"message","role":"assistant","model":"deepseek-flash","content":[{"type":"text","text":"server_tool_use is a block type."}],"stop_reason":"end_turn","usage":{"input_tokens":10,"output_tokens":5}}`)
		case r.URL.Path == "/anthropic/v1/messages" && q.Stream:
			w.Header().Set("Content-Type", "text/event-stream")
			io.WriteString(w, deepSeekNativeStream)
		case r.URL.Path == "/anthropic/v1/messages":
			w.Header().Set("Content-Type", "application/json")
			io.WriteString(w, deepSeekNative)
		case r.URL.Path == "/v1/responses" && q.Stream:
			w.Header().Set("Content-Type", "text/event-stream")
			io.WriteString(w, responsesNativeStream)
		case r.URL.Path == "/v1/responses":
			w.Header().Set("Content-Type", "application/json")
			io.WriteString(w, responsesNative)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	searchesOn(t, provider.Anthropic, srv.URL)
	searchesOn(t, provider.Responses, srv.URL)
	save(t, provider.Provider{ID: "ds", Name: "DeepSeek", Key: "k", Anthropic: srv.URL + "/anthropic", Responses: srv.URL + "/v1", Models: []string{"deepseek-flash"}})

	h := New().Handler()
	send := func(path, body string) {
		t.Helper()
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		h.ServeHTTP(rec, req)
		if rec.Code != 200 {
			t.Fatalf("%s: %d %s", path, rec.Code, rec.Body.String())
		}
	}
	rowsNow := func() []usage.Record {
		t.Helper()
		rows, _ := searchRows(t)
		return rows
	}
	const msgs = `"messages":[{"role":"user","content":"search kangaroo orbit 7781"}],"tools":[{"type":"web_search_20250305","name":"web_search"}]`
	const input = `"input":"search kangaroo orbit 7781","tools":[{"type":"web_search"}]`
	for _, c := range []struct {
		name, path, body string
		searches         int
	}{
		{"anthropic whole", "/v1/messages", `{"model":"ds/deepseek-flash","max_tokens":100,` + msgs + `}`, 1},
		{"anthropic stream", "/v1/messages", `{"model":"ds/deepseek-flash","max_tokens":100,"stream":true,` + msgs + `}`, 1},
		{"responses stream", "/v1/responses", `{"model":"ds/deepseek-flash","stream":true,` + input + `}`, 2},
		{"responses whole", "/v1/responses", `{"model":"ds/deepseek-flash",` + input + `}`, 2},
	} {
		before := len(rowsNow())
		send(c.path, c.body)
		rows := rowsNow()
		if got := len(rows) - before; got != c.searches {
			t.Fatalf("%s: %d search rows, want %d: %+v", c.name, got, c.searches, rows[before:])
		}
		for _, r := range rows[before:] {
			if r.Kind != "web_search" || r.Source != usage.SourceSearch || r.Provider != "ds" || r.Model != "deepseek-flash" ||
				r.Host != provider.HostOf(srv.URL) || r.Status != 200 || r.Failed() {
				t.Fatalf("%s: row %+v", c.name, r)
			}
			if b, _ := json.Marshal(r); strings.Contains(string(b), "kangaroo") {
				t.Fatalf("%s: the query is in the row: %s", c.name, b)
			}
		}
	}
	searchless = true
	before := len(rowsNow())
	send("/v1/messages", `{"model":"ds/deepseek-flash","max_tokens":100,`+msgs+`}`)
	if rows := rowsNow(); len(rows) != before {
		t.Fatalf("a reply that searched nothing wrote %+v", rows[before:])
	}
}

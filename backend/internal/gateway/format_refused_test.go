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

// mimoSchemaRefusal is Xiaomi MiMo's Token Plan turning away a schema
// (klarkxy, #2126: Codex's memory_consolidation and thread_title). The
// message is the reporter's, word for word; the envelope around it is
// OpenAI's, which MiMo's API follows.
const mimoSchemaRefusal = `{"error":{"message":"text.format type 'json_schema' is not supported, only 'text' and 'json_object' are allowed.","type":"invalid_request_error","param":null,"code":"400"}}`

// mimoProbe is the request #2126 gives to reproduce it, with an
// instructions field as Codex sends one.
const mimoProbe = `{"model":"xiaomi/mimo-v2.6-flash","stream":true,"reasoning":{"effort":"low"},"instructions":"You are Codex.","input":"Return a JSON object whose ok field is true.","text":{"format":{"type":"json_schema","name":"compat_probe","strict":true,"schema":{"type":"object","properties":{"ok":{"type":"boolean"}},"required":["ok"],"additionalProperties":false}}}}`

// formatRefuser answers as an upstream that takes no json_schema, and with
// object no json_object either: Responses, Chat and Anthropic Messages.
type formatRefuser struct {
	mu     sync.Mutex
	bodies []map[string]any
	object bool
}

func (v *formatRefuser) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	b, _ := io.ReadAll(r.Body)
	var m map[string]any
	json.Unmarshal(b, &m)
	v.mu.Lock()
	v.bodies = append(v.bodies, m)
	v.mu.Unlock()
	var f map[string]any
	switch {
	case strings.HasSuffix(r.URL.Path, "/responses"):
		t, _ := m["text"].(map[string]any)
		f, _ = t["format"].(map[string]any)
	case strings.HasSuffix(r.URL.Path, "/chat/completions"):
		f, _ = m["response_format"].(map[string]any)
	default:
		oc, _ := m["output_config"].(map[string]any)
		f, _ = oc["format"].(map[string]any)
	}
	if f != nil && (f["type"] == "json_schema" || v.object && f["type"] == "json_object") {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(400)
		if strings.HasSuffix(r.URL.Path, "/messages") {
			io.WriteString(w, `{"type":"error","error":{"type":"invalid_request_error","message":"output_config.format: Extra inputs are not permitted"}}`)
			return
		}
		if v.object {
			io.WriteString(w, `{"error":{"message":"This response_format type is unavailable now","type":"invalid_request_error","param":null,"code":"invalid_request_error"}}`)
			return
		}
		io.WriteString(w, mimoSchemaRefusal)
		return
	}
	text, _ := json.Marshal(`{"ok":true}`)
	w.Header().Set("Content-Type", "text/event-stream")
	switch {
	case strings.HasSuffix(r.URL.Path, "/responses"):
		io.WriteString(w, sse(`data: {"type":"response.output_text.delta","delta":`+string(text)+`}`,
			`data: {"type":"response.completed","response":{"id":"r1","status":"completed","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":`+string(text)+`}]}]}}`))
	case strings.HasSuffix(r.URL.Path, "/chat/completions"):
		io.WriteString(w, sse(`data: {"id":"c1","object":"chat.completion.chunk","model":"m","choices":[{"index":0,"delta":{"role":"assistant","content":`+string(text)+`}}]}`,
			`data: {"id":"c1","object":"chat.completion.chunk","model":"m","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`,
			`data: [DONE]`))
	default:
		io.WriteString(w, sse(`event: message_start`+"\n"+`data: {"type":"message_start","message":{"id":"msg_1","type":"message","role":"assistant","model":"m","content":[],"usage":{"input_tokens":1,"output_tokens":0}}}`,
			`event: content_block_start`+"\n"+`data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`,
			`event: content_block_delta`+"\n"+`data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":`+string(text)+`}}`,
			`event: content_block_stop`+"\n"+`data: {"type":"content_block_stop","index":0}`,
			`event: message_delta`+"\n"+`data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":5}}`,
			`event: message_stop`+"\n"+`data: {"type":"message_stop"}`))
	}
}

func (v *formatRefuser) all() []map[string]any {
	v.mu.Lock()
	defer v.mu.Unlock()
	return append([]map[string]any(nil), v.bodies...)
}

// A request passed through as the client sent it (Codex's Responses to
// Xiaomi MiMo's Responses API, #2126) whose schema the upstream turns away
// is asked again for any JSON object, the schema in its instructions, and
// so from then on; one whose object is turned away too, with the format in
// the instructions alone. Before, the 400 went to the client.
func TestPassthroughSchemaRefusedAskedAsObject(t *testing.T) {
	for _, c := range []struct {
		name   string
		object bool // json_object turned away too
		first  int  // requests on the first turn
		want   any  // text.format sent once it is taken
	}{
		{"object taken", false, 2, map[string]any{"type": "json_object"}},
		{"object refused too", true, 3, nil},
	} {
		t.Run(c.name, func(t *testing.T) {
			fresh(t)
			up := &formatRefuser{object: c.object}
			srv := httptest.NewServer(up)
			t.Cleanup(srv.Close)
			if err := provider.Save(provider.Provider{ID: "xiaomi", Name: "Xiaomi MiMo", Preset: "xiaomi", Key: "tp-k", Models: []string{"mimo-v2.6-flash"}, Responses: srv.URL + "/v1"}); err != nil {
				t.Fatal(err)
			}
			h := New().Handler()
			// the first turn as the reporter's probe, the second where Codex
			// sends its memory_consolidation
			for turn, path := range []string{"/v1/responses", CodexPath + "/responses"} {
				rec := httptest.NewRecorder()
				h.ServeHTTP(rec, httptest.NewRequest("POST", path, strings.NewReader(mimoProbe)))
				if rec.Code != 200 || !strings.Contains(rec.Body.String(), `ok`) {
					t.Fatalf("turn %d: status %d: %s", turn, rec.Code, rec.Body.String())
				}
				bodies := up.all()
				if turn == 0 && len(bodies) != c.first {
					t.Fatalf("%d requests on the first turn, want %d", len(bodies), c.first)
				}
				if turn == 1 && len(bodies) != c.first+1 {
					t.Fatalf("second turn took %d requests, want 1: the refusal wasn't remembered", len(bodies)-c.first)
				}
				last := bodies[len(bodies)-1]
				text, _ := last["text"].(map[string]any)
				if jsonOf(text["format"]) != jsonOf(c.want) {
					t.Fatalf("turn %d: text.format = %s, want %s", turn, jsonOf(text["format"]), jsonOf(c.want))
				}
				ins, _ := last["instructions"].(string)
				if !strings.HasPrefix(ins, "You are Codex.\n\n") || !strings.Contains(ins, `matching this JSON schema`) || !strings.Contains(ins, `"required":["ok"]`) {
					t.Fatalf("turn %d: schema not told in the instructions: %q", turn, ins)
				}
				if last["input"] != "Return a JSON object whose ok field is true." {
					t.Fatalf("turn %d: input changed: %s", turn, jsonOf(last["input"]))
				}
			}
		})
	}
}

// The same on Chat's response_format, passed through to a Chat upstream,
// and on Anthropic's output_config.format, which takes a schema only and
// so goes to the system prompt at once.
func TestPassthroughFormatRefusedChatAndAnthropic(t *testing.T) {
	t.Run("chat", func(t *testing.T) {
		fresh(t)
		up := &formatRefuser{}
		srv := httptest.NewServer(up)
		t.Cleanup(srv.Close)
		if err := provider.Save(provider.Provider{ID: "xiaomi", Name: "Xiaomi MiMo", Key: "tp-k", Models: []string{"mimo-v2.6-flash"}, Chat: srv.URL + "/v1"}); err != nil {
			t.Fatal(err)
		}
		rec := httptest.NewRecorder()
		New().Handler().ServeHTTP(rec, httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(strings.Replace(sdkChatParse, "%s", "xiaomi/mimo-v2.6-flash", 1))))
		if rec.Code != 200 {
			t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
		}
		bodies := up.all()
		last := bodies[len(bodies)-1]
		if len(bodies) != 2 || jsonOf(last["response_format"]) != `{"type":"json_object"}` {
			t.Fatalf("%d requests, last response_format %s", len(bodies), jsonOf(last["response_format"]))
		}
		msgs, _ := last["messages"].([]any)
		first, _ := msgs[0].(map[string]any)
		if len(msgs) != 2 || first["role"] != "system" || !strings.HasPrefix(first["content"].(string), "Extract the event information.\n\n") || !strings.Contains(first["content"].(string), `"CalendarEvent"`) {
			t.Fatalf("messages = %s", jsonOf(msgs))
		}
	})
	t.Run("anthropic", func(t *testing.T) {
		fresh(t)
		up := &formatRefuser{}
		srv := httptest.NewServer(up)
		t.Cleanup(srv.Close)
		if err := provider.Save(provider.Provider{ID: "xiaomi", Name: "Xiaomi MiMo", Key: "tp-k", Models: []string{"mimo-v2.6-flash"}, Anthropic: srv.URL}); err != nil {
			t.Fatal(err)
		}
		body := `{"model":"xiaomi/mimo-v2.6-flash","max_tokens":100,"stream":true,"system":[{"type":"text","text":"Extract the event information."}],"messages":[{"role":"user","content":"Alice and Bob are going to a science fair on Friday."}],"output_config":{"effort":"low","format":{"type":"json_schema","schema":` + calendarSchema + `}}}`
		h := New().Handler()
		for turn := range 2 {
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, httptest.NewRequest("POST", "/v1/messages", strings.NewReader(body)))
			if rec.Code != 200 {
				t.Fatalf("turn %d: status %d: %s", turn, rec.Code, rec.Body.String())
			}
		}
		bodies := up.all()
		if len(bodies) != 3 {
			t.Fatalf("%d requests, want 3: one refused, then two told in words", len(bodies))
		}
		last := bodies[2]
		if jsonOf(last["output_config"]) != `{"effort":"low"}` {
			t.Fatalf("output_config = %s", jsonOf(last["output_config"]))
		}
		sys := jsonOf(last["system"])
		if !strings.Contains(sys, "Extract the event information.") || !strings.Contains(sys, "matching this JSON schema") {
			t.Fatalf("system = %s", sys)
		}
	})
}

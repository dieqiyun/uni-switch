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

// Codex's spawn_agent is in a namespace, and Codex offers web_search beside
// it. On a Responses provider that doesn't search by itself the request is
// translated rather than relayed (uni-switch's search goes in its place), and
// the call the upstream made came to Codex with arguments {} (#613): the
// upstream's argument deltas left out what its finished call held.
func TestNamespacedCallKeepsArgsWhenSearchTakesOver(t *testing.T) {
	const args = `{"task_name":"readonly-test","message":"gAAAAsealed"}`
	quoted, _ := json.Marshal(args)
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/responses" {
			http.NotFound(w, r)
			return
		}
		b, _ := io.ReadAll(r.Body)
		if !strings.Contains(string(b), `"collaboration__spawn_agent"`) {
			http.Error(w, `{"error":{"message":"no spawn_agent: `+strings.ReplaceAll(string(b), `"`, `'`)+`"}}`, 400)
			return
		}
		// its calls go back to Codex as unsealed, so the upstream isn't
		// asked to seal them
		if strings.Contains(string(b), `"encrypted"`) {
			http.Error(w, `{"error":{"message":"sealed message asked for"}}`, 400)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, sse(
			`data: {"type":"response.created","response":{"id":"r1","model":"m"}}`,
			`data: {"type":"response.output_item.added","output_index":0,"item":{"type":"function_call","id":"fc_1","call_id":"call_1","name":"collaboration__spawn_agent","arguments":""}}`,
			`data: {"type":"response.function_call_arguments.delta","output_index":0,"item_id":"fc_1","delta":""}`,
			`data: {"type":"response.function_call_arguments.done","output_index":0,"item_id":"fc_1","arguments":`+string(quoted)+`}`,
			`data: {"type":"response.output_item.done","output_index":0,"item":{"type":"function_call","id":"fc_1","call_id":"call_1","name":"collaboration__spawn_agent","arguments":`+string(quoted)+`,"status":"completed"}}`,
			`data: {"type":"response.completed","response":{"id":"r1","status":"completed","output":[],"usage":{"input_tokens":5,"output_tokens":5}}}`,
		))
	}))
	defer up.Close()

	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	setHome(t, t.TempDir())
	if err := provider.Save(provider.Provider{ID: "rel", Name: "Rel", Key: "k", Responses: up.URL + "/v1", Models: []string{"m"}}); err != nil {
		t.Fatal(err)
	}
	for _, stream := range []bool{false, true} {
		body := `{"model":"rel/m","stream":` + map[bool]string{false: "false", true: "true"}[stream] + `,
			"input":[{"role":"user","content":"Spawn a read-only test sub-agent."}],
			"tools":[{"type":"namespace","name":"collaboration","description":"Sub-agents.","tools":[
				{"type":"function","name":"spawn_agent","strict":false,"description":"Spawns an agent.",
				 "parameters":{"type":"object","properties":{"task_name":{"type":"string"},"message":{"type":"string","encrypted":true}},
				 "required":["task_name","message"],"additionalProperties":false}}]},
			{"type":"web_search"}],"tool_choice":"auto"}`
		rec := httptest.NewRecorder()
		New().Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(body)))
		if rec.Code != 200 {
			t.Fatalf("stream %v: %d %s", stream, rec.Code, rec.Body.String())
		}
		var items []map[string]any
		if stream {
			for _, line := range strings.Split(rec.Body.String(), "\n") {
				data, ok := strings.CutPrefix(line, "data: ")
				if !ok {
					continue
				}
				var ev struct {
					Type string         `json:"type"`
					Item map[string]any `json:"item"`
				}
				if json.Unmarshal([]byte(data), &ev) == nil && ev.Type == "response.output_item.done" {
					items = append(items, ev.Item)
				}
			}
		} else {
			var out struct {
				Output []map[string]any `json:"output"`
			}
			json.Unmarshal(rec.Body.Bytes(), &out)
			items = out.Output
		}
		if len(items) != 1 || items[0]["name"] != "spawn_agent" || items[0]["namespace"] != "collaboration" || items[0]["arguments"] != args {
			t.Fatalf("stream %v: items = %v\n%s", stream, items, rec.Body.String())
		}
	}
}

// What a Responses upstream streams of a call's arguments and what its
// finished call holds can differ; the call is given the fuller.
func TestResponsesCallArgsFromDone(t *testing.T) {
	for _, c := range []struct {
		name   string
		deltas []string
		done   string
		want   string
	}{
		{"deltas only", []string{`{"a":`, `1}`}, "", `{"a":1}`},
		{"done only", nil, `{"a":1}`, `{"a":1}`},
		{"empty deltas", []string{""}, `{"a":1}`, `{"a":1}`},
		{"redacted deltas", []string{"{}"}, `{"a":1}`, `{"a":1}`},
		{"same", []string{`{"a":`, `1}`}, `{"a":1}`, `{"a":1}`},
	} {
		t.Run(c.name, func(t *testing.T) {
			var d responsesDecoder
			lines := []string{`{"type":"response.output_item.added","item":{"type":"function_call","call_id":"c1","name":"f"}}`}
			for _, x := range c.deltas {
				q, _ := json.Marshal(x)
				lines = append(lines, `{"type":"response.function_call_arguments.delta","delta":`+string(q)+`}`)
			}
			q, _ := json.Marshal(c.done)
			lines = append(lines, `{"type":"response.output_item.done","item":{"type":"function_call","call_id":"c1","name":"f","arguments":`+string(q)+`}}`,
				`{"type":"response.completed","response":{"status":"completed"}}`)
			var col collector
			for _, l := range lines {
				d.decode(l, col.add)
			}
			res := col.finish()
			if len(res.Parts) != 1 || string(res.Parts[0].Args) != c.want || res.Stop != "tool" {
				t.Fatalf("parts %+v stop %q", res.Parts, res.Stop)
			}
		})
	}
}

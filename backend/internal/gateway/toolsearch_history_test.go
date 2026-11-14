package gateway

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// A conversation that searched keeps the search's call and result after
// the search tool is gone: Codex offers tool_search only while a deferred
// tool is left to find, and its compaction of a uni-switch model goes with no
// tools at all. A Responses vendor that doesn't know the items refused the
// whole request. Each refusal is the reporter's, word for word: Anyrouter
// on the compaction (#1171, tcxp001), Xiaomi MiMo on a later turn (#1755,
// FivePink). The items go as a function's call and result, the tools the
// search found aren't offered again, and the reply isn't read as a search.
func TestSearchHistoryWithoutTheSearch(t *testing.T) {
	refusals := map[string]string{
		"compaction": `{"error":{"code":null,"message":"invalid codex request","param":null,"type":"invalid_request_error"}}`,
		"later turn": `{"error":{"message":"input item type 'tool_search_call' is not supported by this gateway phase","type":"invalid_request_error","param":null,"code":null}}`,
	}
	withoutSearch := `"tools":[{"type":"function","name":"exec_command","parameters":{"type":"object"}}]`
	for _, tc := range []struct{ name, body string }{
		{"compaction", `{"model":"fake/m1","stream":true,` + withoutSearch + `,` + strings.TrimSuffix(searchInput, "]") + `,{"type":"compaction_trigger"}]}`},
		{"later turn", `{"model":"fake/m1","stream":true,` + withoutSearch + `,` + searchInput + `}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := &fake{t: t, reply: sse(
				`data: {"type":"response.created","response":{"id":"r1"}}`,
				`data: {"type":"response.output_item.done","output_index":0,"item":{"type":"message","id":"m1","role":"assistant","content":[{"type":"output_text","text":"SUMMARY"}]}}`,
				`data: {"type":"response.output_item.done","output_index":1,"item":{"type":"function_call","id":"fc_1","call_id":"c9","name":"tool_search","arguments":"{}","status":"completed"}}`,
				`data: {"type":"response.completed","response":{"id":"r1","output":[],"usage":{"input_tokens":5,"output_tokens":2}}}`)}
			f.refuse = func(b []byte) (int, string) {
				if bytes.Contains(b, []byte(`"tool_search_call"`)) || bytes.Contains(b, []byte(`"tool_search_output"`)) {
					return 400, refusals[tc.name]
				}
				return 0, ""
			}
			setup(t, provider.Responses, f)
			var code int
			var body string
			if tc.name == "compaction" {
				rec := codexPostTo(New(), tc.body)
				code, body = rec.Code, rec.Body.String()
			} else {
				code, body = post(t, "/v1/responses", tc.body)
			}
			if code != 200 || f.calls == 0 {
				t.Fatalf("%d %s", code, body)
			}
			var q struct {
				Tools []rTool `json:"tools"`
				Input []struct {
					Type      string `json:"type"`
					CallID    string `json:"call_id"`
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
					Output    string `json:"output"`
				} `json:"input"`
			}
			if err := json.Unmarshal(f.got, &q); err != nil {
				t.Fatal(err)
			}
			for _, rt := range q.Tools {
				if rt.Name == "tool_search" || rt.Name == "mcp__cal" {
					t.Errorf("offered %s, which Codex didn't: %s", rt.Name, f.got)
				}
			}
			if in := q.Input[1]; in.Type != "function_call" || in.CallID != "s1" || in.Name != "tool_search" || !strings.Contains(in.Arguments, "calendar create") {
				t.Fatalf("search call = %+v", in)
			}
			if in := q.Input[2]; in.Type != "function_call_output" || in.CallID != "s1" || !strings.Contains(in.Output, "mcp__cal__create_event") {
				t.Fatalf("search output = %+v", in)
			}
			if in := q.Input[3]; in.Type != "function_call" || in.CallID != "c1" || in.Name != "create_event" {
				t.Fatalf("the found tool's call changed: %+v", in)
			}
			if strings.Contains(body, `"tool_search_call"`) {
				t.Errorf("a function the model called came back as a search: %s", body)
			}
		})
	}
}

// Unit: history alone is changed, the tools are left as they came, and it
// doesn't say the search was offered.
func TestSearchAsFunctionHistoryOnly(t *testing.T) {
	for _, tools := range []string{``, `"tools":[],`, `"tools":null,`, `"tools":[{"type":"function","name":"exec_command"}],`} {
		in := `{"model":"m",` + tools + searchInput + `}`
		out, ok := searchAsFunction([]byte(in))
		if ok {
			t.Errorf("%s: says the search was offered", tools)
		}
		if bytes.Contains(out, []byte(`"tool_search_`)) {
			t.Errorf("%s: history left: %s", tools, out)
		}
		var a, b map[string]any
		json.Unmarshal([]byte(in), &a)
		json.Unmarshal(out, &b)
		ta, _ := json.Marshal(a["tools"])
		tb, _ := json.Marshal(b["tools"])
		if string(ta) != string(tb) {
			t.Errorf("tools %s became %s", ta, tb)
		}
	}
}

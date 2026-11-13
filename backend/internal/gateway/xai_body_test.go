package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// #3392's second half, as kongkongklng reported it after merging the
// custom-tool fix: through xAI's own key the first Codex turn went
// through and every one after it 400ed "Could not decode the compaction
// blob. Ensure it is unmodified from the compact response." (Codex hands
// the sealed reasoning item back with "content": null), and every Claude
// call 400ed "Invalid request content: Invalid message role." (Claude
// Code's system turn mid-conversation).
const (
	xaiCompactionRefusal = `{"code":"invalid-argument","error":"Could not decode the compaction blob. Ensure it is unmodified from the compact response."}`
	xaiUnknownCustom     = `Failed to deserialize the JSON body into the target type: tools[6].type: unknown variant ` + "`custom`" + `, expected one of ` + "`function`" + `, ` + "`web_search`" + `, ` + "`shell`" + `, ` + "`tool_search`"
	xaiInvalidRole       = `{"type":"error","error":{"type":"invalid_request_error","message":"Invalid request content: Invalid message role."}}`
	xaiNullRequired      = `{"type":"error","error":{"type":"invalid_request_error","message":"Invalid request content: Schema validation failed: [standard_violation] /required: null is not of type \"array\""}}`
)

// The reporter's second turn: Codex's replay of the sealed reasoning item
// its first turn got, null content and all, beside the answer and the next
// ask (the texts cut short).
const xaiSecondTurn = `{"model":"fake/grok-4.7","stream":true,"store":false,"include":["reasoning.encrypted_content"],
	"tools":[{"type":"function","name":"exec_command","parameters":{"type":"object"}},
		{"type":"custom","name":"apply_patch","description":"Apply a patch.","format":{"type":"text"}}],
	"input":[{"type":"message","role":"user","content":[{"type":"input_text","text":"hi"}]},
		{"type":"reasoning","id":"rs_1","summary":[{"type":"summary_text","text":"The user said hi."}],"content":null,"encrypted_content":"gAABblob"},
		{"type":"message","role":"assistant","id":"msg_1","content":[{"type":"output_text","text":"Hello!"}]},
		{"type":"message","role":"user","content":[{"type":"input_text","text":"and now?"}]}]}`

// atXAI points uni-switch's fake provider at api.x.ai and its client at up,
// as xAI's own API is gated on the host.
func atXAI(t *testing.T, up *httptest.Server) *Server {
	t.Helper()
	if err := provider.Save(provider.Provider{ID: "fake", Name: "Fake", Key: "k", Models: []string{"grok-4.7"}, Responses: "http://api.x.ai/v1"}); err != nil {
		t.Fatal(err)
	}
	s := New()
	addr := strings.TrimPrefix(up.URL, "http://")
	s.client = &http.Client{Transport: &http.Transport{DialContext: func(ctx context.Context, network, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, network, addr)
	}}}
	return s
}

// xaiResponsesUpstream refuses what xAI's API does (#3392): a custom tool
// with its 422 serde wording, a reasoning item's null content with the
// compaction-blob 400.
func xaiResponsesUpstream(f *fake) {
	f.refuse = func(body []byte) (int, string) {
		var q struct {
			Tools []struct {
				Type string `json:"type"`
			} `json:"tools"`
			Input []struct {
				Type    string          `json:"type"`
				Content json.RawMessage `json:"content"`
			} `json:"input"`
		}
		json.Unmarshal(body, &q)
		for _, t := range q.Tools {
			if t.Type == "custom" {
				return 422, xaiUnknownCustom
			}
		}
		for _, it := range q.Input {
			if it.Type == "reasoning" && bytes.Equal(bytes.TrimSpace(it.Content), []byte("null")) {
				return 400, xaiCompactionRefusal
			}
		}
		return 0, ""
	}
}

// Codex's turns to xAI's own key, the reporter's session: the first is
// 422ed over its custom tool, translated on the same API; the second, the
// sealed reasoning item handed back with "content": null beside its
// encrypted_content, was 400ed "Could not decode the compaction blob" —
// the null goes, on the translated path the custom tool had put it on and
// on the relay alike.
func TestXAITakesCodexsSealedReasoningBack(t *testing.T) {
	for _, turn := range []string{xaiSecondTurn,
		// the relay alone: no custom tool to turn away, the replayed item
		// shaped where the request is passed through as sent
		strings.Replace(xaiSecondTurn, `,{"type":"custom","name":"apply_patch","description":"Apply a patch.","format":{"type":"text"}}`, "", 1)} {
		f := &fake{t: t, reply: sse(
			`data: {"type":"response.created","response":{"id":"resp_1","status":"in_progress","output":[]}}`,
			`data: {"type":"response.output_item.done","output_index":0,"item":{"type":"message","role":"assistant","content":[{"type":"output_text","text":"ok"}]}}`,
			`data: {"type":"response.completed","response":{"id":"resp_1","status":"completed","output":[],"usage":{"input_tokens":9,"output_tokens":1}}}`)}
		xaiResponsesUpstream(f)
		up := setup(t, provider.Responses, f)
		s := atXAI(t, up)
		rec := httptest.NewRecorder()
		s.Handler().ServeHTTP(rec, httptest.NewRequest("POST", "/v1/responses", strings.NewReader(turn)))
		if rec.Code != 200 {
			t.Fatalf("%d %s\nxAI got %s", rec.Code, rec.Body, f.got)
		}
		if bytes.Contains(f.got, []byte(`"content":null`)) {
			t.Errorf("xAI got the null content: %s", f.got)
		}
		if !bytes.Contains(f.got, []byte(`"encrypted_content":"gAABblob"`)) || !bytes.Contains(f.got, []byte(`"rs_1"`)) {
			t.Errorf("sealed reasoning not replayed as it came: %s", f.got)
		}
	}
}

// The custom tool's 422 is learned from (custom_tool.go) and the turn
// answered translated, so the second turn above reaches xAI through the
// translation: both halves of the reporter's session in one.
func TestXAILearnsCustomToolsThenTakesTheReplay(t *testing.T) {
	f := &fake{t: t, reply: sse(
		`data: {"type":"response.created","response":{"id":"resp_1","status":"in_progress","output":[]}}`,
		`data: {"type":"response.output_item.done","output_index":0,"item":{"type":"message","role":"assistant","content":[{"type":"output_text","text":"ok"}]}}`,
		`data: {"type":"response.completed","response":{"id":"resp_1","status":"completed","output":[],"usage":{"input_tokens":9,"output_tokens":1}}}`)}
	xaiResponsesUpstream(f)
	up := setup(t, provider.Responses, f)
	s := atXAI(t, up)
	first := `{"model":"fake/grok-4.7","stream":true,"store":false,"include":["reasoning.encrypted_content"],
	"tools":[{"type":"function","name":"exec_command","parameters":{"type":"object"}},
		{"type":"custom","name":"apply_patch","description":"Apply a patch.","format":{"type":"text"}}],
	"input":[{"type":"message","role":"user","content":[{"type":"input_text","text":"hi"}]}]}`
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest("POST", "/v1/responses", strings.NewReader(first)))
	if rec.Code != 200 {
		t.Fatalf("first: %d %s\nxAI got %s", rec.Code, rec.Body, f.got)
	}
	if bytes.Contains(f.got, []byte(`"type":"custom"`)) || !bytes.Contains(f.got, []byte(`"name":"apply_patch"`)) {
		t.Errorf("first answered translated, but xAI got %s", f.got)
	}
	rec = httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest("POST", "/v1/responses", strings.NewReader(xaiSecondTurn)))
	if rec.Code != 200 {
		t.Fatalf("second: %d %s\nxAI got %s", rec.Code, rec.Body, f.got)
	}
	if bytes.Contains(f.got, []byte(`"content":null`)) || !bytes.Contains(f.got, []byte(`"encrypted_content":"gAABblob"`)) {
		t.Errorf("second turn: xAI got %s", f.got)
	}
}

// Claude Code's messages carry a system turn mid-conversation (Anthropic
// made them GA); xAI's Messages answers the role "Invalid message role",
// so each folds into the top-level system, the only place it takes the
// role (#3392: every claude call 400ed).
func TestXAIFoldsClaudesSystemTurn(t *testing.T) {
	f := &fake{t: t, reply: sse(
		`event: message_start`+"\n"+`data: {"type":"message_start","message":{"id":"msg_1","type":"message","role":"assistant","model":"grok-4.7","content":[],"usage":{"input_tokens":1,"output_tokens":0}}}`,
		`event: content_block_delta`+"\n"+`data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"ok"}}`,
		`event: content_block_stop`+"\n"+`data: {"type":"content_block_stop","index":0}`,
		`event: message_delta`+"\n"+`data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":1}}`,
		`event: message_stop`+"\n"+`data: {"type":"message_stop"}`)}
	f.refuse = func(body []byte) (int, string) {
		var q struct {
			Messages []struct {
				Role string `json:"role"`
			} `json:"messages"`
		}
		json.Unmarshal(body, &q)
		for _, m := range q.Messages {
			if m.Role == "system" {
				return 400, xaiInvalidRole
			}
		}
		return 0, ""
	}
	up := setup(t, provider.Anthropic, f)
	if err := provider.Save(provider.Provider{ID: "fake", Name: "Fake", Key: "k", Models: []string{"grok-4.7"}, Anthropic: "http://api.x.ai"}); err != nil {
		t.Fatal(err)
	}
	s := New()
	addr := strings.TrimPrefix(up.URL, "http://")
	s.client = &http.Client{Transport: &http.Transport{DialContext: func(ctx context.Context, network, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, network, addr)
	}}}
	// the reporter's shape: the billing system prompt top-level, the
	// environment one a system turn after the user's hello (texts cut)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest("POST", "/v1/messages", strings.NewReader(`{"model":"fake/grok-4.7","stream":true,"max_tokens":128000,
	"system":[{"type":"text","text":"x-anthropic-billing-header: cc_version=2.1.292.c59;"},{"type":"text","text":"You are Claude Code, Anthropic's official CLI for Claude."}],
	"messages":[{"role":"user","content":[{"type":"text","text":"hello"}]},
		{"role":"system","content":[{"type":"text","text":"# Environment\nYou are powered by the model xai/grok-4.7.","cache_control":{"type":"ephemeral"}}]}],
	"thinking":{"type":"adaptive","display":"updates"}}`)))
	if rec.Code != 200 {
		t.Fatalf("%d %s\nxAI got %s", rec.Code, rec.Body, f.got)
	}
	if strings.Contains(string(f.got), `"role":"system"`) {
		t.Errorf("system turn left in messages: %s", f.got)
	}
	for _, said := range []string{"# Environment", "You are Claude Code", `"role":"user"`, `"cache_control":{"type":"ephemeral"}`} {
		if !strings.Contains(string(f.got), said) {
			t.Errorf("xAI's system lost %q: %s", said, f.got)
		}
	}
}

// #3392's third round, as kongkongklng reported it after the system-turn
// fix: codex went through, every Claude call still 400ed xAI's schema
// validation "Invalid request content: Schema validation failed:
// [standard_violation] /required: null is not of type \"array\"" — a
// tool's schema said "required": null where none were (Claude Code's own
// tools or an MCP server's; Anthropic's API takes the null, xAI validates
// the schema). The null required goes, root or nested; a real array of
// names stays.
func TestXAITakesANullRequiredToolSchema(t *testing.T) {
	f := &fake{t: t, reply: sse(
		`event: message_start`+"\n"+`data: {"type":"message_start","message":{"id":"msg_1","type":"message","role":"assistant","model":"grok-4.7","content":[],"usage":{"input_tokens":1,"output_tokens":0}}}`,
		`event: content_block_delta`+"\n"+`data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"ok"}}`,
		`event: content_block_stop`+"\n"+`data: {"type":"content_block_stop","index":0}`,
		`event: message_delta`+"\n"+`data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":1}}`,
		`event: message_stop`+"\n"+`data: {"type":"message_stop"}`)}
	f.refuse = func(body []byte) (int, string) {
		if bytes.Contains(body, []byte(`"required":null`)) {
			return 400, xaiNullRequired
		}
		return 0, ""
	}
	up := setup(t, provider.Anthropic, f)
	if err := provider.Save(provider.Provider{ID: "fake", Name: "Fake", Key: "k", Models: []string{"grok-4.7"}, Anthropic: "http://api.x.ai"}); err != nil {
		t.Fatal(err)
	}
	s := New()
	addr := strings.TrimPrefix(up.URL, "http://")
	s.client = &http.Client{Transport: &http.Transport{DialContext: func(ctx context.Context, network, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, network, addr)
	}}}
	// the reporter's shape: Bash's schema with its real required array
	// beside an MCP-flavoured tool whose schema says required: null at the
	// root, one whose nested property does, and two whose null sits under
	// an array — a zod union's anyOf branch and a tuple items (texts cut)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest("POST", "/v1/messages", strings.NewReader(`{"model":"fake/grok-4.7","stream":true,"max_tokens":128000,
	"tools":[{"name":"Bash","description":"Run a command","input_schema":{"type":"object","properties":{"command":{"type":"string"}},"required":["command"]}},
		{"name":"mcp__server__search","description":"Search","input_schema":{"type":"object","properties":{"query":{"type":"string"}},"required":null}},
		{"name":"mcp__server__nested","description":"Nested","input_schema":{"type":"object","properties":{"opts":{"type":"object","properties":{"a":{"type":"string"}},"required":null},"b":{"type":"number"}}}},
		{"name":"mcp__server__union","description":"Union","input_schema":{"type":"object","properties":{"filter":{"anyOf":[{"type":"object","properties":{"id":{"type":"string"}},"required":null},{"type":"string"}]}}}},
		{"name":"mcp__server__pair","description":"Pair","input_schema":{"type":"object","items":[{"type":"object","properties":{"first":{"type":"string"}},"required":null}],"type":"object"}}],
	"messages":[{"role":"user","content":[{"type":"text","text":"hello"}]}]}`)))
	if rec.Code != 200 {
		t.Fatalf("%d %s\nxAI got %s", rec.Code, rec.Body, f.got)
	}
	if bytes.Contains(f.got, []byte(`"required":null`)) {
		t.Errorf("null required left in a tool schema: %s", f.got)
	}
	for _, said := range []string{`"required":["command"]`, `"query"`, `"opts"`, `"mcp__server__nested"`, `"anyOf"`, `"first"`} {
		if !bytes.Contains(f.got, []byte(said)) {
			t.Errorf("xAI's schemas lost %q: %s", said, f.got)
		}
	}
}

// The other two shapes a tool schema leaves uni-switch in get the same null
// out: Responses' parameters and chat's function.parameters.
func TestXaiBodyDropsNullRequiredInEveryShape(t *testing.T) {
	p := provider.Provider{ID: "fake", Chat: "https://api.x.ai/v1", Responses: "http://api.x.ai/v1"}
	chat := `{"model":"fake/grok-4.7","tools":[{"type":"function","function":{"name":"Bash","parameters":{"type":"object","properties":{"command":{"type":"string"}},"required":null}}},{"type":"function","function":{"name":"Read","parameters":{"type":"object","properties":{"path":{"type":"string"}},"required":["path"]}}}],"messages":[]}`
	got := xaiBody(p, provider.Chat, []byte(chat))
	if bytes.Contains(got, []byte(`"required":null`)) {
		t.Errorf("chat shape kept the null: %s", got)
	}
	if !bytes.Contains(got, []byte(`"required":["path"]`)) || !bytes.Contains(got, []byte(`"command"`)) {
		t.Errorf("chat shape lost a real schema: %s", got)
	}
	responses := `{"model":"fake/grok-4.7","tools":[{"type":"function","name":"exec_command","parameters":{"type":"object","properties":{"cmd":{"type":"string"}},"required":null}}],"input":[]}`
	got = xaiBody(p, provider.Responses, []byte(responses))
	if bytes.Contains(got, []byte(`"required":null`)) {
		t.Errorf("responses shape kept the null: %s", got)
	}
	if !bytes.Contains(got, []byte(`"cmd"`)) {
		t.Errorf("responses shape lost a real schema: %s", got)
	}
}

// A body xAI takes is passed through untouched: the shapers change
// nothing else about it, and another host's own quirks stay its own.
func TestXaiBodyLeavesWhatItTakes(t *testing.T) {
	plain := `{"model":"m","input":[{"type":"message","role":"user","content":[{"type":"input_text","text":"hi"}]},{"type":"reasoning","id":"rs_1","encrypted_content":"gAABblob"}],"messages":[{"role":"user","content":"hi"}],"system":"prompt"}`
	if got := withoutNullReasoningContent([]byte(plain)); !bytes.Equal(got, []byte(plain)) {
		t.Errorf("reasoning without content changed: %s", got)
	}
	if got := systemTurnsFolded([]byte(plain)); !bytes.Equal(got, []byte(plain)) {
		t.Errorf("no system turn changed: %s", got)
	}
	// a reasoning item's null content alone goes
	got := withoutNullReasoningContent([]byte(xaiSecondTurn))
	if bytes.Contains(got, []byte(`"content":null`)) || !bytes.Contains(got, []byte(`"input_text"`)) {
		t.Errorf("null content not gone: %s", got)
	}
	// a system turn with a string content folds beside a string system
	got = systemTurnsFolded([]byte(`{"model":"m","max_tokens":10,"system":"prompt","messages":[{"role":"user","content":"hi"},{"role":"system","content":"mid"}]}`))
	var q struct {
		System []struct {
			Text string `json:"text"`
		} `json:"system"`
		Messages []struct {
			Role string `json:"role"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(got, &q); err != nil {
		t.Fatal(err)
	}
	if len(q.System) != 2 || q.System[0].Text != "prompt" || q.System[1].Text != "mid" {
		t.Errorf("system folded to %s", got)
	}
	if len(q.Messages) != 1 || q.Messages[0].Role != "user" {
		t.Errorf("messages left %s", got)
	}
	// another host's body is untouched
	p := provider.Provider{ID: "other", Chat: "https://api.other/v1"}
	if got := xaiBody(p, provider.Responses, []byte(xaiSecondTurn)); !bytes.Equal(got, []byte(xaiSecondTurn)) {
		t.Errorf("other host shaped: %s", got)
	}
	// a schema whose required are all real arrays keeps its bytes
	real := `{"model":"fake/grok-4.7","tools":[{"type":"function","function":{"name":"Bash","parameters":{"type":"object","properties":{"command":{"type":"string"}},"required":["command"]}}}],"messages":[]}`
	xp := provider.Provider{ID: "fake", Chat: "https://api.x.ai/v1"}
	if got := xaiBody(xp, provider.Chat, []byte(real)); !bytes.Equal(got, []byte(real)) {
		t.Errorf("real required arrays changed: %s", got)
	}
}

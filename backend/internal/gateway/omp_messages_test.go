package gateway

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/tidwall/gjson"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// omp asks a Claude served on Anthropic's Messages API at /v1/messages
// (#329), with fields of its own: context_management keeping every thinking
// block, thinking.display, and output_config.effort beside adaptive
// thinking. They reach the provider as omp sent them, and so do the signed
// thinking of a tool turn going back and the signature of the one coming.
func TestOmpMessagesRelayedAsSent(t *testing.T) {
	sig := "EqQBCkYIBxgCKkDsig=="
	f := &fake{t: t, reply: "event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"msg\",\"type\":\"message\",\"role\":\"assistant\",\"content\":[],\"usage\":{\"input_tokens\":1,\"output_tokens\":0}}}\n\n" +
		"event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"thinking\",\"thinking\":\"\"}}\n\n" +
		"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"thinking_delta\",\"thinking\":\"hm\"}}\n\n" +
		"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"signature_delta\",\"signature\":\"" + sig + "\"}}\n\n" +
		"event: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":0}\n\n" +
		"event: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":{\"output_tokens\":2}}\n\n" +
		"event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"}
	setup(t, provider.Anthropic, f)
	p, _ := provider.Find("fake")
	p.Models = []string{"claude-sonnet-5", "claude-sonnet-4-5"}
	provider.Save(*p)

	history := `[{"role":"user","content":[{"type":"text","text":"list files"}]},` +
		`{"role":"assistant","content":[{"type":"thinking","thinking":"use ls","signature":"` + sig + `"},{"type":"tool_use","id":"toolu_1","name":"bash","input":{"command":"ls"}}]},` +
		`{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_1","content":"a.go"}]}]`
	for _, c := range []struct{ model, thinking, extra string }{
		{"claude-sonnet-5", `{"type":"adaptive","display":"summarized"}`, `,"output_config":{"effort":"high"}`},
		{"claude-sonnet-4-5", `{"type":"enabled","budget_tokens":16384,"display":"summarized"}`, ``},
	} {
		body := `{"model":"fake/` + c.model + `","max_tokens":32000,"stream":true,"system":[{"type":"text","text":"You are omp."}],"messages":` + history +
			`,"thinking":` + c.thinking + c.extra + `,"context_management":{"edits":[{"type":"clear_thinking_20251015","keep":"all"}]}}`
		rec := httptest.NewRecorder()
		req := httptest.NewRequest("POST", "/v1/messages", strings.NewReader(body))
		req.Header.Set("User-Agent", "Anthropic/JS 0.71.2")
		req.Header.Set("Anthropic-Beta", "context-management-2025-06-27,interleaved-thinking-2025-05-14")
		New().Handler().ServeHTTP(rec, req)
		if rec.Code != 200 {
			t.Fatalf("%s: %d %s", c.model, rec.Code, rec.Body.String())
		}
		got := string(f.got)
		if f.path != "/v1/messages" || gjson.Get(got, "model").Str != c.model {
			t.Errorf("%s: sent to %s as %s", c.model, f.path, gjson.Get(got, "model").Str)
		}
		for _, k := range []string{"thinking", "context_management", "output_config", "messages"} {
			if want := gjson.Get(body, k).Raw; gjson.Get(got, k).Raw != want {
				t.Errorf("%s: %s %s, want %s", c.model, k, gjson.Get(got, k).Raw, want)
			}
		}
		if !strings.Contains(f.head.Get("Anthropic-Beta"), "context-management-2025-06-27") {
			t.Errorf("%s: beta %q", c.model, f.head.Get("Anthropic-Beta"))
		}
		if !strings.Contains(rec.Body.String(), `"signature":"`+sig+`"`) {
			t.Errorf("%s: signature lost on the way back:\n%s", c.model, rec.Body.String())
		}
	}
}

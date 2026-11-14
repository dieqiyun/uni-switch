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

// bareBedrock answers Anthropic's messages as Bedrock does for an inference
// profile: its reply names the model by Anthropic's own id, without the
// profile's geography and maker (us.anthropic.claude-sonnet-4-20250514-v1:0
// answered as claude-sonnet-4-20250514, TestSwappedModel).
type bareBedrock struct{ served string }

func (b *bareBedrock) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	io.ReadAll(r.Body)
	w.Header().Set("Content-Type", "text/event-stream")
	io.WriteString(w, sse(
		`event: message_start`+"\n"+`data: {"type":"message_start","message":{"id":"msg_1","model":"`+b.served+`","usage":{"input_tokens":5}}}`,
		`data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`,
		`data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"ok"}}`,
		`data: {"type":"content_block_stop","index":0}`,
		`data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":2}}`,
		`data: {"type":"message_stop"}`))
}

// Bedrock's preset gives Claude as global. inference profiles, and an
// account in GovCloud has us-gov. ones: a reply naming the model by its
// own id is that model, not another swapped in, as it is for us. and apac.
func TestBedrockProfileAnsweredByItsModelIsNoSwap(t *testing.T) {
	for _, c := range []struct {
		sent, served string
		swapped      bool
	}{
		{"global.anthropic.claude-opus-5-5", "claude-opus-5-5", false},
		{"us-gov.anthropic.claude-sonnet-4-5-20250929-v1:0", "claude-sonnet-4-5-20250929", false},
		{"global.anthropic.claude-haiku-4-5-20251001-v1:0", "claude-haiku-4-5-20251001", false},
		{"global.openai.gpt-6-luna", "gpt-6-luna", false},
		{"apac.anthropic.claude-opus-5-5", "claude-opus-5-5", false},
		// models.dev's bedrock list has since grown makers whose models
		// are answered by their own name as Anthropic's are: xAI's, Writer's,
		// Moonshot's and NVIDIA's, asked for as geography. maker ids
		{"global.xai.grok-4.7", "grok-4.7", false},
		{"us.xai.grok-4.6", "xai.grok-4.6", false},
		{"us.writer.palmyra-x5-v1:0", "palmyra-x5", false},
		{"us.writer.palmyra-x5-v1:0", "writer.palmyra-x5-v1:0", false},
		{"moonshot.kimi-k2-thinking", "kimi-k2-thinking", false},
		{"nvidia.nemotron-nano-12b-v2", "nemotron-nano-12b-v2", false},
		// another model is still a swap
		{"global.anthropic.claude-opus-5-5", "claude-sonnet-5", true},
		{"global.openai.gpt-6-sol", "gpt-6-luna", true},
		{"global.xai.grok-4.7", "grok-4.6", true},
		{"us.writer.palmyra-x5-v1:0", "palmyra-x4", true},
	} {
		if got := usage.Swapped(c.sent, c.served); got != c.swapped {
			t.Errorf("Swapped(%q, %q) = %v, want %v", c.sent, c.served, got, c.swapped)
		}
	}

	fresh(t)
	srv := httptest.NewServer(&bareBedrock{served: "claude-opus-5-5"})
	t.Cleanup(srv.Close)
	p, err := provider.FromPreset("bedrock")
	if err != nil {
		t.Fatal(err)
	}
	p.Key = "ABSK-test"
	p.Anthropic, p.Chat = srv.URL+"/anthropic", srv.URL+"/openai/v1"
	p.Models = []string{"global.anthropic.claude-opus-5-5"}
	if err := provider.Save(p); err != nil {
		t.Fatal(err)
	}
	s := New()
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest("POST", "/v1/messages", strings.NewReader(
		`{"model":"bedrock/global.anthropic.claude-opus-5-5","max_tokens":20,"stream":true,"messages":[{"role":"user","content":"hi"}]}`)))
	if rec.Code != 200 {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	s.trace.mu.Lock()
	r := *s.trace.routes[len(s.trace.routes)-1]
	s.trace.mu.Unlock()
	if r.Swapped || len(r.Tries) == 0 || r.Tries[len(r.Tries)-1].Swapped {
		b, _ := json.Marshal(r)
		t.Errorf("route marked swapped for Bedrock's own name of the model: %s", b)
	}
	rows, _, _ := usage.Ledger(usage.All, usage.Filter{})
	for _, row := range rows {
		if row.Swapped {
			t.Errorf("usage row marked swapped: model %q served %q", row.Model, row.Served)
		}
	}
}

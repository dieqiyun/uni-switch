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

// adaptiveVendor answers as Anthropic does for a model from 4.6 on: a 400
// for thinking.type=enabled, which claude-opus-5-5 no longer takes.
type adaptiveVendor struct {
	mu     sync.Mutex
	bodies []map[string]any
}

func (v *adaptiveVendor) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	b, _ := io.ReadAll(r.Body)
	var m map[string]any
	json.Unmarshal(b, &m)
	v.mu.Lock()
	v.bodies = append(v.bodies, m)
	v.mu.Unlock()
	th, _ := m["thinking"].(map[string]any)
	model, _ := m["model"].(string)
	if th["type"] == "enabled" && strings.Contains(model, "5-5") || strings.Contains(model, "5.5") && th["type"] == "enabled" {
		w.WriteHeader(400)
		io.WriteString(w, `{"type":"error","error":{"type":"invalid_request_error","message":"thinking.type.enabled is not supported for this model. Use thinking.type.adaptive and output_config.effort to control thinking behavior."}}`)
		return
	}
	if m["stream"] == true {
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, "event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_1\",\"type\":\"message\",\"role\":\"assistant\",\"model\":\""+model+"\",\"content\":[],\"usage\":{\"input_tokens\":1,\"output_tokens\":0}}}\n\n"+
			"event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\",\"text\":\"\"}}\n\n"+
			"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"ok\"}}\n\n"+
			"event: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":0}\n\n"+
			"event: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":{\"output_tokens\":1}}\n\n"+
			"event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	io.WriteString(w, `{"id":"msg_1","type":"message","role":"assistant","model":"`+model+`","content":[{"type":"text","text":"ok"}],"stop_reason":"end_turn","usage":{"input_tokens":1,"output_tokens":1}}`)
}

func (v *adaptiveVendor) last() map[string]any {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.bodies[len(v.bodies)-1]
}

// Keenc on Discord: claude-opus-5-5 answers 400 to thinking.type=enabled,
// and uni-switch sent it so — relaying an agent's budget as it came, and
// building one for a model uni-switch knows by another name than the vendor's.
// Every request reaching an Anthropic endpoint for it asks adaptive
// thinking, with the effort in output_config; an older Claude keeps its
// budget, and "disabled" stays.
func TestAdaptiveOnlyModelsNeverGetABudget(t *testing.T) {
	fresh(t)
	up := &adaptiveVendor{}
	srv := httptest.NewServer(up)
	t.Cleanup(srv.Close)
	if err := provider.Save(provider.Provider{ID: "relay", Name: "Relay", Key: "k", Anthropic: srv.URL,
		Models: []string{"claude-opus-5.5", "anthropic.claude-opus-5-5-20260901-v1:0", "opus", "claude-sonnet-4-5"}}); err != nil {
		t.Fatal(err)
	}
	// uni-switch's "opus" is the vendor's claude-opus-5-5[1m]
	if err := provider.SetUpstreamName("relay/opus", "claude-opus-5-5[1m]"); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		name, path, body string
		thinking, effort string
	}{
		{"budget relayed", "/v1/messages",
			`{"model":"relay/claude-opus-5.5","max_tokens":32000,"thinking":{"type":"enabled","budget_tokens":10000},"messages":[{"role":"user","content":"hi"}]}`,
			`{"display":"summarized","type":"adaptive"}`, "medium"},
		{"budget relayed, Bedrock's spelling", "/v1/messages",
			`{"model":"relay/anthropic.claude-opus-5-5-20260901-v1:0","max_tokens":64000,"thinking":{"type":"enabled","budget_tokens":31999},"messages":[{"role":"user","content":"hi"}]}`,
			`{"display":"summarized","type":"adaptive"}`, "max"},
		{"effort kept beside a budget", "/v1/messages",
			`{"model":"relay/claude-opus-5.5","max_tokens":32000,"thinking":{"type":"enabled","budget_tokens":4000,"display":"summarized"},"output_config":{"effort":"high"},"messages":[{"role":"user","content":"hi"}]}`,
			`{"display":"summarized","type":"adaptive"}`, "high"},
		{"the client's own display kept", "/v1/messages",
			`{"model":"relay/claude-opus-5.5","max_tokens":32000,"thinking":{"type":"enabled","budget_tokens":10000,"display":"omitted"},"messages":[{"role":"user","content":"hi"}]}`,
			`{"display":"omitted","type":"adaptive"}`, "medium"},
		{"name mapped after", "/v1/messages",
			`{"model":"relay/opus","max_tokens":32000,"thinking":{"type":"enabled","budget_tokens":24000},"messages":[{"role":"user","content":"hi"}]}`,
			`{"display":"summarized","type":"adaptive"}`, "high"},
		{"chat's effort, name mapped after", "/v1/chat/completions",
			`{"model":"relay/opus","reasoning_effort":"low","messages":[{"role":"user","content":"hi"}]}`,
			`{"display":"summarized","type":"adaptive"}`, "low"},
		{"disabled stays on an older Claude", "/v1/messages",
			`{"model":"relay/claude-sonnet-4-5","max_tokens":100,"thinking":{"type":"disabled"},"messages":[{"role":"user","content":"hi"}]}`,
			`{"type":"disabled"}`, ""},
		// Opus 5.5 refuses disabled at every effort (#1454)
		{"disabled left out for Opus 5.5", "/v1/messages",
			`{"model":"relay/claude-opus-5.5","max_tokens":100,"thinking":{"type":"disabled"},"messages":[{"role":"user","content":"hi"}]}`,
			`null`, "low"},
		{"an older Claude keeps its budget", "/v1/messages",
			`{"model":"relay/claude-sonnet-4-5","max_tokens":32000,"thinking":{"type":"enabled","budget_tokens":10000},"messages":[{"role":"user","content":"hi"}]}`,
			`{"budget_tokens":10000,"type":"enabled"}`, ""},
	} {
		t.Run(c.name, func(t *testing.T) {
			code, body := post(t, c.path, c.body)
			if code != 200 {
				t.Fatalf("status %d: %s", code, body)
			}
			got := up.last()
			if th, _ := json.Marshal(got["thinking"]); string(th) != c.thinking {
				t.Errorf("thinking = %s, want %s", th, c.thinking)
			}
			oc, _ := got["output_config"].(map[string]any)
			if e, _ := oc["effort"].(string); e != c.effort {
				t.Errorf("output_config = %v, want effort %q", got["output_config"], c.effort)
			}
		})
	}
}

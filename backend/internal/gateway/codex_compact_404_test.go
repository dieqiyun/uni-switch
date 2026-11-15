package gateway

import (
	"encoding/base64"
	"strings"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// compactedText is the summary in a compaction Codex was answered with.
func compactedText(t *testing.T, body string) string {
	t.Helper()
	for _, e := range events(body) {
		if e["type"] != "response.output_item.done" {
			continue
		}
		item := e["item"].(map[string]any)
		enc, _ := item["encrypted_content"].(string)
		if item["type"] != "compaction" || !strings.HasPrefix(enc, uniSwitchCompaction) {
			t.Fatalf("not uni-switch's compaction: %s", body)
		}
		b, _ := base64.StdEncoding.DecodeString(strings.TrimPrefix(enc, uniSwitchCompaction))
		return string(b)
	}
	t.Fatalf("no compaction: %s", body)
	return ""
}

const compact404Request = `{"model":"fake/m1","stream":true,"store":false,"include":["reasoning.encrypted_content"],
  "input":[{"type":"message","role":"user","content":[{"type":"input_text","text":"fix the login bug"}]},
  {"type":"reasoning","id":"rs_1","summary":[]},
  {"type":"function_call","id":"fc_1","call_id":"call_1","name":"shell","arguments":"{\"cmd\":\"ls\"}"},
  {"type":"function_call_output","call_id":"call_1","output":"main.go"},
  {"type":"message","role":"assistant","content":[{"type":"output_text","text":"I fixed main.go"}]},
  {"type":"message","role":"user","content":[{"type":"input_text","text":"now add a test"}]},
  {"type":"compaction_trigger"}]}`

const relay404 = `{"error":{"message":"Upstream request failed","type":"upstream_error"}}`

// #866: a relay that answers a uni-switch model's summary request 404
// ("flyapi: Upstream request failed") left Codex's compaction aborted and
// reconnecting. uni-switch asks again with the conversation as text, and when
// that fails too compacts without a model, so Codex goes on.
func TestCodexCompact404AsksAgainAsText(t *testing.T) {
	f := &fake{t: t, reply: sse(
		`data: {"type":"response.output_item.done","output_index":0,"item":{"type":"message","role":"assistant","content":[{"type":"output_text","text":"SUMMARY"}]}}`,
		`data: {"type":"response.completed","response":{"id":"resp_p","status":"completed","output":[]}}`)}
	// the relay can't serve the items' ids and sealed reasoning
	f.refuse = func(body []byte) (int, string) {
		if strings.Contains(string(body), `"rs_1"`) || strings.Contains(string(body), `"fc_1"`) {
			return 404, relay404
		}
		return 0, ""
	}
	setup(t, provider.Responses, f)
	code, body := codexPost(t, compact404Request)
	if code != 200 {
		t.Fatalf("%d %s", code, body)
	}
	if got := compactedText(t, body); got != "SUMMARY" {
		t.Errorf("summary %q", got)
	}
	for _, want := range []string{"fix the login bug", "now add a test", "I fixed main.go", "shell", "main.go", "CONTEXT CHECKPOINT COMPACTION"} {
		if !strings.Contains(string(f.got), want) {
			t.Errorf("plain request lacks %q: %s", want, f.got)
		}
	}
}

func TestCodexCompact404CompactsLocally(t *testing.T) {
	f := &fake{t: t, refuse: func([]byte) (int, string) { return 404, relay404 }}
	setup(t, provider.Responses, f)
	code, body := codexPost(t, compact404Request)
	if code != 200 {
		t.Fatalf("%d %s", code, body)
	}
	got := compactedText(t, body)
	for _, want := range []string{"fix the login bug", "now add a test", "I fixed main.go", "Upstream request failed", "404"} {
		if !strings.Contains(got, want) {
			t.Errorf("local summary lacks %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "CONTEXT CHECKPOINT COMPACTION") {
		t.Errorf("local summary has the prompt:\n%s", got)
	}
}

// Other failures go back to Codex as they came.
func TestCodexCompactOtherFailuresStay(t *testing.T) {
	for _, status := range []int{401, 429} {
		f := &fake{t: t, refuse: func([]byte) (int, string) { return status, `{"error":{"message":"nope"}}` }}
		setup(t, provider.Responses, f)
		code, body := codexPost(t, compact404Request)
		if code == 200 || f.calls > 1 && strings.Contains(string(f.got), "This is the conversation so far") {
			t.Errorf("%d: %d %s (calls %d)", status, code, body, f.calls)
		}
	}
}

// #1511 (TooPub): a relay answered a uni-switch model's summary request
// 400 invalid_request_error "apex: Upstream request failed", and Codex's
// compaction stopped there, while Codex asking the same relay itself
// compacted. The reporter's body, as it came.
const relayApex400 = `{"error":{"code":null,"message":"apex: Upstream request failed","param":null,"type":"invalid_request_error"}}`

func TestCodexCompactUpstreamFailed400AsksAgainAsText(t *testing.T) {
	f := &fake{t: t, reply: sse(
		`data: {"type":"response.output_item.done","output_index":0,"item":{"type":"message","role":"assistant","content":[{"type":"output_text","text":"SUMMARY"}]}}`,
		`data: {"type":"response.completed","response":{"id":"resp_p","status":"completed","output":[]}}`)}
	f.refuse = func(body []byte) (int, string) {
		if strings.Contains(string(body), `"rs_1"`) || strings.Contains(string(body), `"fc_1"`) {
			return 400, relayApex400
		}
		return 0, ""
	}
	setup(t, provider.Responses, f)
	code, body := codexPost(t, compact404Request)
	if code != 200 {
		t.Fatalf("%d %s", code, body)
	}
	if got := compactedText(t, body); got != "SUMMARY" {
		t.Errorf("summary %q", got)
	}
	if f.calls != 2 || !strings.Contains(string(f.got), "fix the login bug") {
		t.Errorf("asked again (calls %d) with: %s", f.calls, f.got)
	}
}

func TestCodexCompactUpstreamFailed400CompactsLocally(t *testing.T) {
	for _, proto := range []provider.Protocol{provider.Responses, provider.Chat} {
		f := &fake{t: t, refuse: func([]byte) (int, string) { return 400, relayApex400 }}
		setup(t, proto, f)
		code, body := codexPost(t, compact404Request)
		if code != 200 {
			t.Fatalf("%v: %d %s", proto, code, body)
		}
		got := compactedText(t, body)
		for _, want := range []string{"fix the login bug", "now add a test", "apex: Upstream request failed", "400"} {
			if !strings.Contains(got, want) {
				t.Errorf("%v: local summary lacks %q:\n%s", proto, want, got)
			}
		}
	}
}

// The relay's words on an account's refusal still go back to Codex.
func TestCodexCompactUpstreamFailedAccountStays(t *testing.T) {
	for _, status := range []int{401, 403, 429} {
		f := &fake{t: t, refuse: func([]byte) (int, string) { return status, relayApex400 }}
		setup(t, provider.Responses, f)
		code, body := codexPost(t, compact404Request)
		if code == 200 || strings.Contains(string(f.got), "This is the conversation so far") {
			t.Errorf("%d: %d %s (calls %d)", status, code, body, f.calls)
		}
	}
}

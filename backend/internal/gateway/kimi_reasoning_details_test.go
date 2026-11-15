package gateway

import (
	"bytes"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// kimiRefuseDetails answers as api.kimi.com/coding/v1 did on 2026-10-12 a
// message with reasoning_details: an entry with a stream's index is
// turned away for it, any other entry (OpenRouter's reasoning.text,
// reasoning.summary, reasoning.encrypted, text, thinking) for its type.
// reasoning_content and an empty list are taken.
func kimiRefuseDetails(body []byte) (int, string) {
	var q struct {
		Messages []map[string]json.RawMessage `json:"messages"`
	}
	json.Unmarshal(body, &q)
	for i, m := range q.Messages {
		var details []map[string]any
		json.Unmarshal(m["reasoning_details"], &details)
		for j, d := range details {
			why := "has an invalid type"
			if _, ok := d["index"]; ok {
				why = "must not contain streaming index"
			}
			return 400, fmt.Sprintf(`{"error":{"message":"the reasoning_details at position %d entry %d %s","type":"invalid_request_error"}}`, i, j, why)
		}
	}
	return 0, ""
}

// #1765 (Leo-origin): a conversation MiMo answered first, through Command
// Code, has its thinking in reasoning_details with a stream's index, as pi
// keeps it; routed on to Kimi Code it was refused 400 on every turn. To
// Kimi it goes as reasoning_content in the first request; a turn with its
// own reasoning_content keeps that, and other messages keep their bytes.
func TestKimiGetsReasoningDetailsAsContent(t *testing.T) {
	f := &fake{t: t, reply: chatWholeOK, ctype: "application/json", refuse: kimiRefuseDetails}
	up := setup(t, provider.Chat, f)
	if err := provider.Save(provider.Provider{ID: "kimi-code-cn", Name: "Kimi Code (China)", Key: "k", Models: []string{"k3"},
		Preset: "kimi-code-cn", Chat: up.URL + "/v1"}); err != nil {
		t.Fatal(err)
	}
	srv := New()
	code, body := postTo(t, srv, "/v1/chat/completions", `{"model":"kimi-code-cn/k3","stream":false,"messages":[
        {"role":"user","content":"Is 91 prime?"},
        {"role":"assistant","content":"91 = 7 x 13.","reasoning_details":[{"type":"reasoning.text","text":"Check divisibility by 7: 7*13=91.","format":"unknown","index":0}]},
        {"role":"user","content":"And 93?"},
        {"role":"assistant","content":"93 = 3 x 31.","reasoning_content":"Digits sum to 12.","reasoning_details":[{"type":"reasoning.text","text":"Digits sum to 12.","format":"unknown","index":0}]},
        {"role":"user","content":"And 97? One word."}]}`)
	if code != 200 || f.calls != 1 {
		t.Fatalf("%d after %d calls, sent %s: %s", code, f.calls, f.got, body)
	}
	ms := sentMessages(t, f.got)
	if string(ms[0]) != `{"role":"user","content":"Is 91 prime?"}` || string(ms[4]) != `{"role":"user","content":"And 97? One word."}` {
		t.Errorf("other messages changed: %s", f.got)
	}
	sameJSON(t, ms[1], `{"role":"assistant","content":"91 = 7 x 13.","reasoning_content":"Check divisibility by 7: 7*13=91."}`)
	sameJSON(t, ms[3], `{"role":"assistant","content":"93 = 3 x 31.","reasoning_content":"Digits sum to 12."}`)
}

// An upstream that isn't known to be Kimi (a relay in front of it) and
// refuses reasoning_details the way Kimi does is asked once more with its
// thinking as reasoning_content, and from then on sent it so at once.
func TestReasoningDetailsRefusedAtPosition(t *testing.T) {
	f := &fake{t: t, reply: chatWholeOK, ctype: "application/json", refuse: kimiRefuseDetails}
	setup(t, provider.Chat, f)
	srv := New()
	req := `{"model":"m1","stream":false,"messages":[{"role":"user","content":"Read note.txt."},` +
		`{"role":"assistant","content":null,"tool_calls":[{"id":"call_1","type":"function","function":{"name":"read","arguments":"{}"}}],"reasoning_details":[{"type":"reasoning.text","text":"Let me read it.","format":"unknown","index":0}]},` +
		`{"role":"tool","tool_call_id":"call_1","content":"secret: uni-switch"}]}`
	for _, want := range []int{2, 1} {
		f.calls = 0
		code, body := postTo(t, srv, "/v1/chat/completions", req)
		if code != 200 || f.calls != want {
			t.Fatalf("%d after %d calls (want %d), sent %s: %s", code, f.calls, want, f.got, body)
		}
		ms := sentMessages(t, f.got)
		sameJSON(t, ms[1], `{"role":"assistant","content":null,"tool_calls":[{"id":"call_1","type":"function","function":{"name":"read","arguments":"{}"}}],"reasoning_content":"Let me read it."}`)
	}
	// a 400 that names reasoning_content otherwise (DeepSeek asking for it
	// back) leaves it where it is
	if got := refusedInMessages(400, []byte(`{"error":{"message":"The reasoning_content in the thinking mode must be passed back to the API.","type":"invalid_request_error"}}`), []byte(`{"messages":[{"reasoning_content":"x"}]}`)); len(got) != 0 {
		t.Errorf("DeepSeek's ask read as a refusal of %v", got)
	}
	if got := refusedInMessages(400, []byte(`{"error":{"message":"the reasoning_details at position 2 entry 0 must not contain streaming index","type":"invalid_request_error"}}`), []byte(`{"messages":[{"role":"user"}]}`)); len(got) != 0 {
		t.Errorf("a field the request doesn't carry read as refused: %v", got)
	}
	if !bytes.Contains(f.got, []byte(`"tool_call_id":"call_1"`)) {
		t.Errorf("the tool result was lost: %s", f.got)
	}
}

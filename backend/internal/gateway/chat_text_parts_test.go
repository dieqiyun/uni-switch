package gateway

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// stringContentOnly answers as an OpenAI-compatible server whose Chat
// schema takes a message's content as a string alone (pydantic, as the
// Intern discovery API's Atria answered DeepSeek Harness on Discord): a
// 422 with one "Input should be a valid string" per message whose content
// is a list, the list itself as the error's "input".
func stringContentOnly(body []byte) (int, string) {
	var q struct {
		Messages []map[string]json.RawMessage `json:"messages"`
	}
	json.Unmarshal(body, &q)
	var errs []string
	for i, m := range q.Messages {
		c := bytes.TrimSpace(m["content"])
		if len(c) > 0 && c[0] == '[' {
			errs = append(errs, fmt.Sprintf(`{"type":"string_type","loc":["body","messages",%d,"content"],"msg":"Input should be a valid string","input":%s}`, i, c))
		}
	}
	if len(errs) == 0 {
		return 0, ""
	}
	return 422, `{"detail":[` + strings.Join(errs, ",") + `]}`
}

// DeepSeek Harness (pi-ai's openai-completions) sends every user message
// as a list of text parts, its system prompt, assistant text and tool
// results as strings. Relayed as it came to a custom provider on OpenAI
// compatible whose server takes a string alone, every request was turned
// away. The lists go as the string they say, the rest as it was sent.
func TestChatRelayTextPartsAsString(t *testing.T) {
	f := &fake{t: t, reply: chatWholeOK, ctype: "application/json", refuse: stringContentOnly}
	up := setup(t, provider.Chat, f)
	chatProvider(t, up.URL, provider.Provider{ID: "atria", Models: []string{"Atria-Dawn"}})
	code, body := postTo(t, New(), "/v1/chat/completions", `{"model":"atria/Atria-Dawn","stream":false,
		"tools":[{"type":"function","function":{"name":"read","parameters":{"type":"object","properties":{"path":{"type":"string"}}}}}],
		"messages":[
		{"role":"system","content":"You are dsh. <rules> & more"},
		{"role":"user","content":[{"type":"text","text":"read a.go"}]},
		{"role":"assistant","content":"Reading.","tool_calls":[{"id":"call_1","type":"function","function":{"name":"read","arguments":"{\"path\":\"a.go\"}"}}]},
		{"role":"tool","content":"package a","tool_call_id":"call_1"},
		{"role":"user","content":[{"text":"now <b>","type":"text"},{"type":"text","text":"and c.go"}]}]}`)
	if code != 200 || f.calls != 1 {
		t.Fatalf("%d after %d calls, sent %s: %s", code, f.calls, f.got, body)
	}
	want := []string{
		`{"role":"system","content":"You are dsh. <rules> & more"}`,
		`{"role":"user","content":"read a.go"}`,
		`{"role":"assistant","content":"Reading.","tool_calls":[{"id":"call_1","type":"function","function":{"name":"read","arguments":"{\"path\":\"a.go\"}"}}]}`,
		`{"role":"tool","content":"package a","tool_call_id":"call_1"}`,
		`{"role":"user","content":"now <b>\nand c.go"}`,
	}
	got := sentMessages(t, f.got)
	if len(got) != len(want) {
		t.Fatalf("sent %s", f.got)
	}
	for i, w := range want {
		sameJSON(t, got[i], w)
	}
}

// A list with anything but plain text in it stays a list: an image, a
// cache point (OpenRouter's Claude), a part of another type. A body with
// no list of parts comes back byte for byte, and in one with, every byte
// around the lists joined stays as sent.
func TestTextPartsAsStringOnlyPlainText(t *testing.T) {
	clean := []byte(`{"model":"m","messages":[{"role":"user","content":"a <b> & c"},{"role":"assistant","content":"x","n":1.50}]}`)
	if got := textPartsAsString(clean); !bytes.Equal(got, clean) {
		t.Fatalf("a clean body was rewritten: %s", got)
	}
	for _, kept := range []string{
		`[{"type":"text","text":"see"},{"type":"image_url","image_url":{"url":"data:image/png;base64,AAAA"}}]`,
		`[{"type":"text","text":"long prompt","cache_control":{"type":"ephemeral"}}]`,
		`[{"type":"thinking","thinking":"hm"},{"type":"text","text":"ok"}]`,
		`[{"type":"input_text","text":"hi"}]`,
		`[{"type":"text"}]`,
		`["hi"]`,
		`[]`,
	} {
		body := []byte(`{"model":"m","messages":[{"role":"user","content":` + kept + `}]}`)
		if got := textPartsAsString(body); !bytes.Equal(got, body) {
			t.Errorf("%s was rewritten: %s", kept, got)
		}
	}
	body := []byte(`{"model":"m","temperature":0.70,"messages":[{"role":"user","content": [{"type":"text","text":"a <b>"}]},{"role":"user","content":[{"type":"image_url","image_url":{"url":"x"}}]},{"role":"user","content":[{"type":"text","text":"b"},{"type":"text","text":"c"}],"name":"u"}],"stream":true}`)
	want := `{"model":"m","temperature":0.70,"messages":[{"role":"user","content": "a <b>"},{"role":"user","content":[{"type":"image_url","image_url":{"url":"x"}}]},{"role":"user","content":"b\nc","name":"u"}],"stream":true}`
	if got := textPartsAsString(body); string(got) != want {
		t.Fatalf("got  %s\nwant %s", got, want)
	}
}

package gateway

import (
	"bufio"
	"encoding/json"
	"strings"
	"testing"

	"github.com/tidwall/gjson"
)

// GLM on a Chat API reasoning, its filter then ending the reply with
// nothing said: Zhipu's finish_reason "sensitive" (yhwangl on X: the
// thinking was shown, the answer never came, and the next model wasn't
// asked).
var glmThoughtThenSensitive = sse(
	`data: {"id":"c1","choices":[{"index":0,"delta":{"role":"assistant","reasoning_content":"first thought"}}]}`,
	`data: {"id":"c1","choices":[{"index":0,"delta":{"reasoning_content":" second thought"}}]}`,
	`data: {"id":"c1","choices":[{"index":0,"delta":{},"finish_reason":"sensitive"}],"usage":{"prompt_tokens":3,"completion_tokens":5,"total_tokens":8}}`,
	`data: [DONE]`)

// The same, ended by OpenAI's content_filter.
var chatThoughtThenFiltered = strings.Replace(glmThoughtThenSensitive, `"sensitive"`, `"content_filter"`, 1)

// The same, ended by Zhipu's 1301 error mid-stream.
var glmThoughtThen1301 = sse(
	`data: {"id":"c1","choices":[{"index":0,"delta":{"role":"assistant","reasoning_content":"first thought"}}]}`,
	`data: {"id":"c1","choices":[{"index":0,"delta":{"reasoning_content":" second thought"}}]}`,
	`data: {"error":{"code":"1301","message":"系统检测到输入或生成内容可能包含不安全或敏感内容，请您避免输入易产生敏感内容的提示语，感谢您的配合。"}}`)

// An Anthropic API's reasoning, its thinking block still open when the
// filter's stop_reason "refusal" ends it.
var glmAnthropicThoughtThenRefused = sse(
	`event: message_start`+"\n"+`data: {"type":"message_start","message":{"id":"m1","role":"assistant","model":"glm-5.1","content":[],"usage":{"input_tokens":3}}}`,
	`event: content_block_start`+"\n"+`data: {"type":"content_block_start","index":0,"content_block":{"type":"thinking","thinking":""}}`,
	`event: content_block_delta`+"\n"+`data: {"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"first thought"}}`,
	`event: content_block_delta`+"\n"+`data: {"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":" second thought"}}`,
	`event: message_delta`+"\n"+`data: {"type":"message_delta","delta":{"stop_reason":"refusal","stop_sequence":null},"usage":{"output_tokens":5}}`,
	`event: message_stop`+"\n"+`data: {"type":"message_stop"}`)

const chatStreamAsk = `{"model":"group/g","stream":true,"messages":[{"role":"user","content":"hi"}]}`

// dataOf reads an SSE body's data, one per event, [DONE] as it is.
func dataOf(t *testing.T, body string) []string {
	t.Helper()
	var out []string
	sc := bufio.NewScanner(strings.NewReader(body))
	sc.Buffer(nil, 1<<20)
	for sc.Scan() {
		if d, ok := strings.CutPrefix(sc.Text(), "data:"); ok {
			out = append(out, strings.TrimSpace(d))
		}
	}
	return out
}

// One message, the reasoning's block stopped before the next member's
// text block, numbered after it.
func checkAnthropicSpliced(t *testing.T, body string) {
	t.Helper()
	starts, blocks, open := 0, 0, map[int64]bool{}
	for _, d := range dataOf(t, body) {
		switch gjson.Get(d, "type").String() {
		case "message_start":
			starts++
		case "content_block_start":
			i := gjson.Get(d, "index").Int()
			if i != int64(blocks) || len(open) > 0 {
				t.Fatalf("block %d begun as block %d, %v still open: %s", blocks, i, open, body)
			}
			blocks++
			open[i] = true
		case "content_block_delta":
			if !open[gjson.Get(d, "index").Int()] {
				t.Fatalf("a delta for a block not open: %s", d)
			}
		case "content_block_stop":
			delete(open, gjson.Get(d, "index").Int())
		}
	}
	if starts != 1 || blocks != 2 || len(open) != 0 || strings.Count(body, `"type":"message_stop"`) != 1 {
		t.Fatalf("message_start %d, blocks %d, open %v: %s", starts, blocks, open, body)
	}
}

// One stream, one [DONE].
func checkChatSpliced(t *testing.T, body string) {
	t.Helper()
	ev := dataOf(t, body)
	if strings.Count(body, "[DONE]") != 1 || ev[len(ev)-1] != "[DONE]" {
		t.Fatalf("not one stream: %s", body)
	}
	for _, d := range ev[:len(ev)-1] {
		if !json.Valid([]byte(d)) {
			t.Fatalf("a chunk that isn't JSON: %q", d)
		}
	}
}

// One response begun, the next member's message after the reasoning's
// item, the sequence numbers going on from the reasoning's.
func checkResponsesSpliced(t *testing.T, body string) {
	t.Helper()
	created, last, items := 0, int64(-1), map[string]int64{}
	for _, d := range dataOf(t, body) {
		typ := gjson.Get(d, "type").String()
		if typ == "response.created" {
			created++
		}
		if sq := gjson.Get(d, "sequence_number"); sq.Exists() {
			if sq.Int() != last+1 {
				t.Fatalf("sequence_number %d after %d: %s", sq.Int(), last, body)
			}
			last = sq.Int()
		}
		if typ == "response.output_item.added" {
			items[gjson.Get(d, "item.type").String()] = gjson.Get(d, "output_index").Int()
		}
	}
	if created != 1 || strings.Count(body, `"type":"response.completed"`) != 1 {
		t.Fatalf("response.created %d: %s", created, body)
	}
	if r, ok := items["reasoning"]; !ok || items["message"] != r+1 {
		t.Fatalf("output items %v: %s", items, body)
	}
}

// The vendors' words for their filter refusing, beside OpenAI's and
// Anthropic's: Zhipu's, DashScope's and Volcengine Ark's.
func TestSensitiveContentRefusals(t *testing.T) {
	for _, raw := range []string{
		`{"error":{"code":"1301","message":"系统检测到输入或生成内容可能包含不安全或敏感内容"}}`,
		`{"error":{"code":"data_inspection_failed","type":"data_inspection_failed","message":"Input data may contain inappropriate content."}}`,
		`{"code":"DataInspectionFailed","message":"Output data may contain inappropriate content."}`,
		`{"error":{"code":"OutputTextSensitiveContentDetected","message":"The request failed because the output text may contain sensitive information."}}`,
		`{"error":{"code":"InputTextSensitiveContentDetected","message":"x"}}`,
	} {
		if _, ok := policyRefusal([]byte(raw)); !ok {
			t.Errorf("not a refusal: %s", raw)
		}
	}
	for _, raw := range []string{
		`{"error":{"code":"1301","message":"rate limited"}}`,
		`{"error":{"code":"1113","message":"余额不足"}}`,
	} {
		if _, ok := policyRefusal([]byte(raw)); ok {
			t.Errorf("a refusal: %s", raw)
		}
	}
	if stopFromChat("sensitive") != "filter" {
		t.Error("Zhipu's sensitive is not the filter's stop")
	}
}

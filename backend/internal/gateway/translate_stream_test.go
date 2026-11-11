package gateway

import (
	"bufio"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
)

// chatToResponses runs Chat Completions stream chunks through the decoder and
// the Responses encoder and returns every event written.
func chatToResponses(t *testing.T, chunks ...string) []map[string]any {
	t.Helper()
	rec := httptest.NewRecorder()
	enc := &responsesEncoder{w: newSSEWriter(rec), model: "m"}
	var d chatDecoder
	for _, c := range chunks {
		if err := d.decode(c, enc.event); err != nil {
			t.Fatal(err)
		}
	}
	enc.finish()
	var out []map[string]any
	sc := bufio.NewScanner(strings.NewReader(rec.Body.String()))
	for sc.Scan() {
		if data, ok := strings.CutPrefix(sc.Text(), "data: "); ok {
			var m map[string]any
			if json.Unmarshal([]byte(data), &m) == nil {
				out = append(out, m)
			}
		}
	}
	return out
}

// The translated reply has just one choice. Interleaved choices must not
// mix text, thinking, tool calls, or a finish reason into the first choice.
func TestChatTranslationKeepsFirstChoice(t *testing.T) {
	var d chatDecoder
	var got []Event
	for _, chunk := range []string{
		`{"id":"x","choices":[{"index":0,"delta":{"reasoning_content":"First thought","content":"Alpha","tool_calls":[{"index":0,"id":"first","function":{"name":"right","arguments":"{\"ok\":true}"}}]}},{"index":1,"delta":{"reasoning_content":"Other thought","content":"Beta","tool_calls":[{"index":0,"id":"other","function":{"name":"wrong","arguments":"{}"}}]},"finish_reason":"tool_calls"}]}`,
		`{"id":"x","choices":[{"index":1,"delta":{"content":" again","tool_calls":[{"index":0,"function":{"arguments":"not mine"}}]},"finish_reason":"stop"},{"index":0,"delta":{"content":" done","tool_calls":[{"index":0,"function":{"arguments":" more"}}]},"finish_reason":"tool_calls"}]}`,
		// An omitted index continues the choice already selected.
		`{"id":"x","choices":[{"delta":{"content":"!"}}]}`,
	} {
		if err := d.decode(chunk, func(ev Event) { got = append(got, ev) }); err != nil {
			t.Fatal(err)
		}
	}
	var text, think, toolID, toolName, args, stop string
	for _, ev := range got {
		switch ev.Kind {
		case KText:
			text += ev.Text
		case KThink:
			think += ev.Text
		case KToolStart:
			toolID, toolName = ev.ID, ev.Name
		case KToolArgs:
			args += ev.Text
		case KStop:
			stop = ev.Stop
		}
	}
	if text != "Alpha done!" || think != "First thought" || toolID != "first" || toolName != "right" || args != `{"ok":true} more` || stop != "tool" {
		t.Fatalf("mixed choices: text=%q think=%q tool=%q/%q args=%q stop=%q", text, think, toolID, toolName, args, stop)
	}
}

// A relay can number its only choice 1. Translation must keep that reply,
// including on later chunks, instead of returning a successful empty answer.
func TestChatTranslationKeepsOnlyChoiceIndexedOne(t *testing.T) {
	out := chatToResponses(t,
		`{"id":"x","choices":[{"index":1,"delta":{"content":"Hel"}}]}`,
		`{"id":"x","choices":[{"index":0,"delta":{"content":"not mine"}},{"index":1,"delta":{"content":"lo"},"finish_reason":"stop"}]}`,
	)
	var text string
	for _, ev := range out {
		if ev["type"] == "response.output_text.delta" {
			text += ev["delta"].(string)
		}
	}
	if text != "Hello" {
		t.Fatalf("translated text = %q; want Hello", text)
	}
}

// Some relays spell the choice index as a string. Decoding the rest of the
// chunk must not fail or silently drop content on these responses.
func TestChatTranslationAcceptsStringChoiceIndex(t *testing.T) {
	out := chatToResponses(t,
		`{"id":"x","choices":[{"index":"0","delta":{"content":"Hel"}}]}`,
		`{"id":"x","choices":[{"index":"1","delta":{"content":"not mine"}},{"index":0,"delta":{"content":"lo"},"finish_reason":"stop"}]}`,
	)
	var text string
	for _, ev := range out {
		if ev["type"] == "response.output_text.delta" {
			text += ev["delta"].(string)
		}
	}
	if text != "Hello" {
		t.Fatalf("translated text = %q; want Hello", text)
	}
}

// A usage-only chunk cannot choose which answer is kept. An unindexed
// one-choice relay continues to work when index is omitted or null.
func TestChatTranslationUsageBeforeUnindexedChoice(t *testing.T) {
	out := chatToResponses(t,
		`{"id":"x","choices":[],"usage":{"prompt_tokens":7,"completion_tokens":2}}`,
		`{"id":"x","choices":[{"index":null,"delta":{"content":"Hel"}}]}`,
		`{"id":"x","choices":[{"delta":{"content":"lo"},"finish_reason":"stop"}]}`,
	)
	var text string
	for _, ev := range out {
		if ev["type"] == "response.output_text.delta" {
			text += ev["delta"].(string)
		}
	}
	if text != "Hello" {
		t.Fatalf("translated text = %q; want Hello", text)
	}
}

// A role-only chunk with no index must not select index 0 before the relay
// sends the actual answer under its sole indexed choice.
func TestChatTranslationUnindexedRoleBeforeIndexedChoice(t *testing.T) {
	out := chatToResponses(t,
		`{"id":"x","choices":[{"delta":{"role":"assistant"}}]}`,
		`{"id":"x","choices":[{"index":1,"delta":{"content":"Hel"}}]}`,
		`{"id":"x","choices":[{"index":1,"delta":{"content":"lo"},"finish_reason":"stop"}]}`,
	)
	var text string
	for _, ev := range out {
		if ev["type"] == "response.output_text.delta" {
			text += ev["delta"].(string)
		}
	}
	if text != "Hello" {
		t.Fatalf("translated text = %q; want Hello", text)
	}
}

// Missing and null indexes after an indexed first chunk continue that
// choice; malformed indexes should not turn an ordinary reply into a 200/empty.
func TestChatTranslationUnindexedChunksContinueIndexedChoice(t *testing.T) {
	out := chatToResponses(t,
		`{"id":"x","choices":[{"index":1,"delta":{"content":"H"}}]}`,
		`{"id":"x","choices":[{"delta":{"content":"e"}}]}`,
		`{"id":"x","choices":[{"index":null,"delta":{"content":"l"}}]}`,
		`{"id":"x","choices":[{"index":{},"delta":{"content":"l"}}]}`,
		`{"id":"x","choices":[{"index":1,"delta":{"content":"o"},"finish_reason":"stop"}]}`,
	)
	var text string
	for _, ev := range out {
		if ev["type"] == "response.output_text.delta" {
			text += ev["delta"].(string)
		}
	}
	if text != "Hello" {
		t.Fatalf("translated text = %q; want Hello", text)
	}
}

// JSON numeric 1.0 and 1 name the same upstream choice. The normalized
// index must not vary across chunks of that one answer.
func TestChatTranslationEquivalentNumericIndexes(t *testing.T) {
	out := chatToResponses(t,
		`{"id":"x","choices":[{"index":1.0,"delta":{"content":"Hel"}}]}`,
		`{"id":"x","choices":[{"index":1,"delta":{"content":"lo"},"finish_reason":"stop"}]}`,
	)
	var text string
	for _, ev := range out {
		if ev["type"] == "response.output_text.delta" {
			text += ev["delta"].(string)
		}
	}
	if text != "Hello" {
		t.Fatalf("translated text = %q; want Hello", text)
	}
}

// #18: with parallel tool calls each function_call item kept the next call's
// id, and the last two shared one — upstreams then refused the history.
func TestParallelToolCallIDs(t *testing.T) {
	var chunks []string
	for i, id := range []string{"call_00", "call_01", "call_02"} {
		chunks = append(chunks, `{"id":"x","choices":[{"delta":{"tool_calls":[{"index":`+string(rune('0'+i))+`,"id":"`+id+`","type":"function","function":{"name":"run","arguments":""}}]}}]}`,
			`{"id":"x","choices":[{"delta":{"tool_calls":[{"index":`+string(rune('0'+i))+`,"function":{"arguments":"{\"cmd\":\"`+id+`\"}"}}]}}]}`)
	}
	chunks = append(chunks, `{"id":"x","choices":[{"delta":{},"finish_reason":"tool_calls"}]}`)
	var got []string
	for _, ev := range chatToResponses(t, chunks...) {
		if ev["type"] != "response.output_item.done" {
			continue
		}
		item := ev["item"].(map[string]any)
		if item["type"] != "function_call" {
			continue
		}
		var args struct{ Cmd string }
		json.Unmarshal([]byte(item["arguments"].(string)), &args)
		if item["call_id"] != args.Cmd {
			t.Errorf("call %s went out with id %v", args.Cmd, item["call_id"])
		}
		got = append(got, item["call_id"].(string))
	}
	if strings.Join(got, ",") != "call_00,call_01,call_02" {
		t.Fatalf("call ids = %v", got)
	}
}

// Some relays put the same thought under reasoning_content and reasoning;
// it must come out once, not "TheThe user user".
func TestReasoningSentUnderBothNames(t *testing.T) {
	var think strings.Builder
	for _, ev := range chatToResponses(t,
		`{"id":"x","choices":[{"delta":{"reasoning_content":"The ","reasoning":"The "}}]}`,
		`{"id":"x","choices":[{"delta":{"reasoning_content":"user","reasoning":"user"}}]}`,
		`{"id":"x","choices":[{"delta":{"reasoning":" wants"}}]}`,
		`{"id":"x","choices":[{"delta":{"content":"ok"},"finish_reason":"stop"}]}`) {
		if ev["type"] == "response.reasoning_summary_text.delta" {
			think.WriteString(ev["delta"].(string))
		}
	}
	if think.String() != "The user wants" {
		t.Fatalf("thinking = %q", think.String())
	}
}

// The ChatGPT backend's completed response lists no output, so a streamed
// function call alone says the turn stopped for a tool.
func TestStreamedCallStopsForTool(t *testing.T) {
	var d responsesDecoder
	var stop string
	for _, c := range []string{
		`{"type":"response.created","response":{"id":"r1"}}`,
		`{"type":"response.output_item.added","item":{"type":"function_call","call_id":"c1","name":"get_weather"}}`,
		`{"type":"response.function_call_arguments.delta","delta":"{\"city\":\"Paris\"}"}`,
		`{"type":"response.completed","response":{"id":"r1","status":"completed","output":[]}}`,
	} {
		d.decode(c, func(e Event) {
			if e.Kind == KStop {
				stop = e.Stop
			}
		})
	}
	if stop != "tool" {
		t.Fatalf("stop %q", stop)
	}
}

// A relay can repeat a tool call's id on every fragment of it, where the
// spec sends the id only on the first. Each such fragment must continue
// the call already open: only a new id, a new name or a new index starts
// the next one. Read as a fresh call every time, one tool call came out as
// several, each holding a piece of the arguments.
func TestChatTranslationToolCallIDRepeated(t *testing.T) {
	var d chatDecoder
	var got []Event
	for _, chunk := range []string{
		`{"id":"x","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"get_weather","arguments":""}}]}}]}`,
		`{"id":"x","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_1","function":{"arguments":"{\"ci"}}]}}]}`,
		`{"id":"x","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_1","function":{"arguments":"ty\":\"SF\"}"}}]}}]}`,
		`{"id":"x","choices":[{"delta":{},"finish_reason":"tool_calls"}]}`,
	} {
		if err := d.decode(chunk, func(ev Event) { got = append(got, ev) }); err != nil {
			t.Fatal(err)
		}
	}
	var starts []Event
	var args, stop string
	for _, ev := range got {
		switch ev.Kind {
		case KToolStart:
			starts = append(starts, ev)
		case KToolArgs:
			args += ev.Text
		case KStop:
			stop = ev.Stop
		}
	}
	if len(starts) != 1 {
		t.Fatalf("a repeated id started %d calls, want 1: %+v", len(starts), starts)
	}
	if starts[0].ID != "call_1" || starts[0].Name != "get_weather" {
		t.Fatalf("call = %q %q", starts[0].ID, starts[0].Name)
	}
	if args != `{"city":"SF"}` {
		t.Fatalf("arguments = %q", args)
	}
	if stop != "tool" {
		t.Fatalf("stop = %q, want tool", stop)
	}
}

// chatToAnthropic runs Chat Completions stream chunks through the decoder
// and the Anthropic encoder, giving back what an Anthropic client — Claude
// Code among them — is answered for a relay that only speaks Chat.
func chatToAnthropic(t *testing.T, chunks ...string) []map[string]any {
	t.Helper()
	rec := httptest.NewRecorder()
	enc := &anthropicEncoder{w: newSSEWriter(rec), model: "m"}
	var d chatDecoder
	for _, c := range chunks {
		if err := d.decode(c, enc.event); err != nil {
			t.Fatal(err)
		}
	}
	enc.finish()
	var out []map[string]any
	sc := bufio.NewScanner(strings.NewReader(rec.Body.String()))
	for sc.Scan() {
		if data, ok := strings.CutPrefix(sc.Text(), "data: "); ok {
			var m map[string]any
			if json.Unmarshal([]byte(data), &m) == nil {
				out = append(out, m)
			}
		}
	}
	return out
}

// The same repeated id, seen by an Anthropic client: one content block
// holding the whole call, not one block per fragment with the arguments
// split between them, none of them valid JSON.
func TestRepeatedToolCallIDOpensOneBlock(t *testing.T) {
	out := chatToAnthropic(t,
		`{"id":"x","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"get_weather","arguments":""}}]}}]}`,
		`{"id":"x","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_1","function":{"arguments":"{\"city\":"}}]}}]}`,
		`{"id":"x","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_1","function":{"arguments":"\"SF\"}"}}]}}]}`,
		`{"id":"x","choices":[{"delta":{},"finish_reason":"tool_calls"}]}`,
	)
	var blocks []map[string]any
	var input string
	for _, ev := range out {
		switch ev["type"] {
		case "content_block_start":
			if b, ok := ev["content_block"].(map[string]any); ok && b["type"] == "tool_use" {
				blocks = append(blocks, b)
			}
		case "content_block_delta":
			if d, ok := ev["delta"].(map[string]any); ok && d["type"] == "input_json_delta" {
				input += d["partial_json"].(string)
			}
		}
	}
	if len(blocks) != 1 {
		t.Fatalf("%d tool_use blocks, want 1: %+v", len(blocks), blocks)
	}
	if blocks[0]["id"] != "call_1" || blocks[0]["name"] != "get_weather" {
		t.Fatalf("block = %v", blocks[0])
	}
	var args map[string]any
	if json.Unmarshal([]byte(input), &args) != nil {
		t.Fatalf("arguments %q are not one whole JSON object", input)
	}
	if args["city"] != "SF" {
		t.Fatalf("arguments = %v", args)
	}
}

// Two calls in one reply are still two, whatever a relay repeats: the
// second index opens the second call, and its own fragments continue it.
func TestRepeatedToolCallIDKeepsParallelCalls(t *testing.T) {
	out := chatToResponses(t,
		`{"id":"x","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_a","type":"function","function":{"name":"one","arguments":""}}]}}]}`,
		`{"id":"x","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_a","function":{"arguments":"{\"n\":1}"}}]}}]}`,
		`{"id":"x","choices":[{"index":0,"delta":{"tool_calls":[{"index":1,"id":"call_b","type":"function","function":{"name":"two","arguments":"{\"n\":2}"}}]}}]}`,
		`{"id":"x","choices":[{"delta":{},"finish_reason":"tool_calls"}]}`,
	)
	var calls []string
	for _, ev := range out {
		if ev["type"] != "response.output_item.done" {
			continue
		}
		item, _ := ev["item"].(map[string]any)
		if item["type"] != "function_call" {
			continue
		}
		calls = append(calls, item["call_id"].(string)+"="+item["arguments"].(string))
	}
	if strings.Join(calls, " ") != `call_a={"n":1} call_b={"n":2}` {
		t.Fatalf("calls = %v", calls)
	}
}

// interleavedCalls is a Chat stream whose two tool calls come a fragment
// at a time, by turns: index 0, 1, 0, 1, 0, 1, the first fragment of each
// with its id and name, the rest only pieces of the arguments
// (mysterious_dove on Discord: Codex got six function_call items, the first
// two with arguments {}, the other four with no name and half of them).
var interleavedCalls = []string{
	`{"id":"x","model":"m","choices":[{"index":0,"delta":{"role":"assistant","tool_calls":[{"index":0,"id":"call_read","type":"function","function":{"name":"read_file","arguments":"{\"pa"}}]},"finish_reason":null}]}`,
	`{"id":"x","model":"m","choices":[{"index":0,"delta":{"tool_calls":[{"index":1,"id":"call_list","type":"function","function":{"name":"list_dir","arguments":"{\"di"}}]},"finish_reason":null}]}`,
	`{"id":"x","model":"m","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"th\":\"a/b"}}]},"finish_reason":null}]}`,
	`{"id":"x","model":"m","choices":[{"index":0,"delta":{"tool_calls":[{"index":1,"function":{"arguments":"r\":\"src"}}]},"finish_reason":null}]}`,
	`{"id":"x","model":"m","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":".go\"}"}}]},"finish_reason":null}]}`,
	`{"id":"x","model":"m","choices":[{"index":0,"delta":{"tool_calls":[{"index":1,"function":{"arguments":"/x\"}"}}]},"finish_reason":null}]}`,
	`{"id":"x","model":"m","choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}`,
	`[DONE]`,
}

// Interleaved calls reach a Responses client (Codex) as two function_call
// items, one per index, in index order, each whole, with every event of
// an item under the same output_index and item id.
func TestInterleavedToolCallsToResponses(t *testing.T) {
	out := chatToResponses(t, interleavedCalls...)
	type item struct {
		id, callID, name, args, streamed string
		index                            float64
	}
	var items []*item
	byID := map[string]*item{}
	idAt := map[float64]string{}
	for _, ev := range out {
		switch ev["type"] {
		case "response.output_item.added":
			it, _ := ev["item"].(map[string]any)
			if it["type"] != "function_call" {
				continue
			}
			x := &item{id: it["id"].(string), index: ev["output_index"].(float64)}
			items = append(items, x)
			byID[x.id] = x
			idAt[x.index] = x.id
		case "response.function_call_arguments.delta":
			x := byID[ev["item_id"].(string)]
			if x == nil || idAt[ev["output_index"].(float64)] != x.id {
				t.Fatalf("delta for an item not added at its output_index: %v", ev)
			}
			x.streamed += ev["delta"].(string)
		case "response.output_item.done":
			it, _ := ev["item"].(map[string]any)
			if it["type"] != "function_call" {
				continue
			}
			x := byID[it["id"].(string)]
			if x == nil || idAt[ev["output_index"].(float64)] != x.id {
				t.Fatalf("done for an item not added at its output_index: %v", ev)
			}
			x.callID, x.name, x.args = it["call_id"].(string), it["name"].(string), it["arguments"].(string)
		}
	}
	if len(items) != 2 {
		t.Fatalf("%d function_call items, want 2: %+v", len(items), out)
	}
	want := []struct{ callID, name, key, value string }{
		{"call_read", "read_file", "path", "a/b.go"},
		{"call_list", "list_dir", "dir", "src/x"},
	}
	for i, w := range want {
		x := items[i]
		var args map[string]string
		if err := json.Unmarshal([]byte(x.args), &args); err != nil {
			t.Fatalf("call %d arguments %q are not whole JSON: %v", i, x.args, err)
		}
		if x.callID != w.callID || x.name != w.name || args[w.key] != w.value || len(args) != 1 {
			t.Fatalf("call %d = %s %s %s, want %s %s {%q:%q}", i, x.callID, x.name, x.args, w.callID, w.name, w.key, w.value)
		}
		if x.streamed != x.args {
			t.Fatalf("call %d streamed %q but is done with %q", i, x.streamed, x.args)
		}
	}
	if items[0].index == items[1].index || items[0].id == items[1].id {
		t.Fatalf("two calls share an output_index or id: %+v %+v", items[0], items[1])
	}
}

// The same stream seen by an Anthropic client: two tool_use blocks, each
// with its own whole input.
func TestInterleavedToolCallsToAnthropic(t *testing.T) {
	out := chatToAnthropic(t, interleavedCalls...)
	type block struct{ id, name, input string }
	var blocks []*block
	at := map[float64]*block{}
	for _, ev := range out {
		switch ev["type"] {
		case "content_block_start":
			if b, ok := ev["content_block"].(map[string]any); ok && b["type"] == "tool_use" {
				x := &block{id: b["id"].(string), name: b["name"].(string)}
				blocks = append(blocks, x)
				at[ev["index"].(float64)] = x
			}
		case "content_block_delta":
			if d, ok := ev["delta"].(map[string]any); ok && d["type"] == "input_json_delta" {
				at[ev["index"].(float64)].input += d["partial_json"].(string)
			}
		}
	}
	if len(blocks) != 2 {
		t.Fatalf("%d tool_use blocks, want 2: %+v", len(blocks), out)
	}
	got := blocks[0].id + " " + blocks[0].name + " " + blocks[0].input + " | " + blocks[1].id + " " + blocks[1].name + " " + blocks[1].input
	if got != `call_read read_file {"path":"a/b.go"} | call_list list_dir {"dir":"src/x"}` {
		t.Fatalf("blocks = %s", got)
	}
}

// And a client answered whole, from the same stream: two calls.
func TestInterleavedToolCallsWhole(t *testing.T) {
	var d chatDecoder
	var col collector
	for _, c := range interleavedCalls {
		if err := d.decode(c, col.add); err != nil {
			t.Fatal(err)
		}
	}
	var got []string
	for _, p := range col.finish().Parts {
		if p.Kind == ToolCall {
			got = append(got, p.ID+" "+p.Name+" "+string(p.Args))
		}
	}
	if strings.Join(got, " | ") != `call_read read_file {"path":"a/b.go"} | call_list list_dir {"dir":"src/x"}` {
		t.Fatalf("calls = %q", got)
	}
}

// A stream that names every call first, arguments empty, then gives each
// call's arguments in turn, ending at [DONE] with no finish_reason: still
// one call per index, each with its own arguments.
func TestToolCallsNamedFirstThenArgued(t *testing.T) {
	var d chatDecoder
	var col collector
	for _, c := range []string{
		`{"id":"x","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"a","type":"function","function":{"name":"one","arguments":""}},{"index":1,"id":"b","type":"function","function":{"name":"two","arguments":""}}]}}]}`,
		`{"id":"x","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{\"n\":1}"}}]}}]}`,
		`{"id":"x","choices":[{"index":0,"delta":{"tool_calls":[{"index":1,"function":{"arguments":"{\"n\":2}"}}]}}]}`,
		`[DONE]`,
	} {
		if err := d.decode(c, col.add); err != nil {
			t.Fatal(err)
		}
	}
	var got []string
	for _, p := range col.finish().Parts {
		if p.Kind == ToolCall {
			got = append(got, p.ID+" "+p.Name+" "+string(p.Args))
		}
	}
	if strings.Join(got, " | ") != `a one {"n":1} | b two {"n":2}` {
		t.Fatalf("calls = %q", got)
	}
}

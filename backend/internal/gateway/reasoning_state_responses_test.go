package gateway

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// Reasoning items as OpenAI's reasoning guide gives them, with
// include: ["reasoning.encrypted_content"] and store false: one with a
// summary, one sealed with none (#1445, XYenon).
const (
	rsSummarized = `{"id":"rs_6820f383d7c08191846711c5df8233bc0ac5ba57aafcbac7","type":"reasoning","summary":[{"type":"summary_text","text":"**Checking the weather**\n\nThe user wants Paris."}],"encrypted_content":"gAAAAABoIPOHmVpvRSm3xMmc1nW0W1WqGa8AI6kjYvmdqBtbKdpE2qY0VJ0r6oeBbyuh1OWXIHfF41nXR4kaWh6iUxlyVcGmBVLGYzAa6ihHjyUuBaODTsy1ZJ3m1TXGDeT9zCYFx1k8ZDcyThGhw=="}`
	rsSealed     = `{"id":"rs_6820f3b2e9fc8191a5c7d0ab1c8e2f4a0ac5ba57aafcbac7","type":"reasoning","summary":[],"encrypted_content":"gAAAAABoIPOyXq2Ub7WqZbSd8dHXqfVt1pDkAm3VlAk7nM4lQ0pC9yr0eU8iHnJgTkRm5Zs2Wq8pX1cLbVfN6oA3eY7hD4uG2tK9sJ0wQ=="}`
)

// A Responses request's reasoning items go back to a Responses upstream
// as they came: id, summary and encrypted_content. The old build left
// them out (only DeepSeek's text was replayed).
func TestResponsesSealedReasoningGoesBackAsItCame(t *testing.T) {
	body := `{"model":"gpt-5","store":false,"include":["reasoning.encrypted_content"],"reasoning":{"effort":"medium"},"input":[
		{"type":"message","role":"user","content":[{"type":"input_text","text":"What's the weather in Paris?"}]},
		` + rsSummarized + `,
		` + rsSealed + `,
		{"type":"function_call","id":"fc_1","call_id":"call_1","name":"get_weather","arguments":"{\"location\":\"Paris\"}"},
		{"type":"function_call_output","call_id":"call_1","output":"15 degrees"}]}`
	req, err := parse(provider.Responses, []byte(body))
	if err != nil {
		t.Fatal(err)
	}
	for _, host := range []string{"chatgpt.com", "api.openai.com"} {
		out, err := build(provider.Responses, req, "gpt-5", host, false)
		if err != nil {
			t.Fatal(err)
		}
		var q struct {
			Input []json.RawMessage `json:"input"`
		}
		if err := json.Unmarshal(out, &q); err != nil {
			t.Fatal(err)
		}
		var got []json.RawMessage
		for _, it := range q.Input {
			if bytes.Contains(it, []byte(`"type":"reasoning"`)) {
				got = append(got, it)
			}
		}
		if len(got) != 2 || !jsonIs(t, got[0], rsSummarized) || !jsonIs(t, got[1], rsSealed) {
			t.Fatalf("%s: reasoning items %s\nin %s", host, got, out)
		}
	}
}

// Reasoning with its text in it (another vendor's, #1411) still isn't
// handed to OpenAI, sealed or not.
func TestResponsesReasoningTextStillLeftOut(t *testing.T) {
	body := `{"model":"gpt-5","input":[
		{"type":"message","role":"user","content":[{"type":"input_text","text":"hi"}]},
		{"id":"rs_1","type":"reasoning","summary":[],"content":[{"type":"reasoning_text","text":"DeepSeek's thought"}],"encrypted_content":"deepseek-sealed"},
		{"type":"message","role":"assistant","content":[{"type":"output_text","text":"hello"}]}]}`
	req, err := parse(provider.Responses, []byte(body))
	if err != nil {
		t.Fatal(err)
	}
	out, _ := build(provider.Responses, req, "gpt-5", "chatgpt.com", false)
	if bytes.Contains(out, []byte("deepseek-sealed")) || bytes.Contains(out, []byte(`"reasoning_text"`)) {
		t.Fatalf("reasoning text sent to OpenAI: %s", out)
	}
}

// responsesSealedStream is a Responses stream as OpenAI's streaming
// reference gives it: a reasoning item with a summary, one with none,
// then the answer.
var responsesSealedStream = []string{
	`{"type":"response.created","sequence_number":0,"response":{"id":"resp_67c9fdcecf488190bdd9a0409de3a1ec07b8b0ad4e5eb654","object":"response","status":"in_progress","model":"gpt-5","output":[]}}`,
	`{"type":"response.output_item.added","sequence_number":1,"output_index":0,"item":{"id":"rs_6820f383d7c08191846711c5df8233bc0ac5ba57aafcbac7","type":"reasoning","summary":[]}}`,
	`{"type":"response.reasoning_summary_part.added","sequence_number":2,"item_id":"rs_6820f383d7c08191846711c5df8233bc0ac5ba57aafcbac7","output_index":0,"summary_index":0,"part":{"type":"summary_text","text":""}}`,
	`{"type":"response.reasoning_summary_text.delta","sequence_number":3,"item_id":"rs_6820f383d7c08191846711c5df8233bc0ac5ba57aafcbac7","output_index":0,"summary_index":0,"delta":"**Checking the weather**\n\n"}`,
	`{"type":"response.reasoning_summary_text.delta","sequence_number":4,"item_id":"rs_6820f383d7c08191846711c5df8233bc0ac5ba57aafcbac7","output_index":0,"summary_index":0,"delta":"The user wants Paris."}`,
	`{"type":"response.reasoning_summary_text.done","sequence_number":5,"item_id":"rs_6820f383d7c08191846711c5df8233bc0ac5ba57aafcbac7","output_index":0,"summary_index":0,"text":"**Checking the weather**\n\nThe user wants Paris."}`,
	`{"type":"response.output_item.done","sequence_number":6,"output_index":0,"item":` + rsSummarized + `}`,
	`{"type":"response.output_item.added","sequence_number":7,"output_index":1,"item":{"id":"rs_6820f3b2e9fc8191a5c7d0ab1c8e2f4a0ac5ba57aafcbac7","type":"reasoning","summary":[]}}`,
	`{"type":"response.output_item.done","sequence_number":8,"output_index":1,"item":` + rsSealed + `}`,
	`{"type":"response.output_item.added","sequence_number":9,"output_index":2,"item":{"id":"msg_67c9fdcf37fc8190ba82116e33fb28c507b8b0ad4e5eb654","type":"message","status":"in_progress","role":"assistant","content":[]}}`,
	`{"type":"response.output_text.delta","sequence_number":10,"item_id":"msg_67c9fdcf37fc8190ba82116e33fb28c507b8b0ad4e5eb654","output_index":2,"content_index":0,"delta":"It's 15 degrees."}`,
	`{"type":"response.output_item.done","sequence_number":11,"output_index":2,"item":{"id":"msg_67c9fdcf37fc8190ba82116e33fb28c507b8b0ad4e5eb654","type":"message","status":"completed","role":"assistant","content":[{"type":"output_text","text":"It's 15 degrees.","annotations":[]}]}}`,
	`{"type":"response.completed","sequence_number":12,"response":{"id":"resp_67c9fdcecf488190bdd9a0409de3a1ec07b8b0ad4e5eb654","object":"response","status":"completed","model":"gpt-5","output":[],"usage":{"input_tokens":10,"output_tokens":30,"total_tokens":40}}}`,
}

// A Responses stream through translation gives the client each reasoning
// item as the upstream did it: its id while it streams, and the item
// whole when done and in the completed response. The old encoder made an
// rs_ id of its own and dropped encrypted_content.
func TestResponsesStreamKeepsSealedReasoning(t *testing.T) {
	evs := translateStream(t, provider.Responses, provider.Responses, responsesSealedStream...)
	var done []any
	var completed []any
	var addedIDs []string
	for _, ev := range evs {
		item, _ := ev["item"].(map[string]any)
		switch ev["type"] {
		case "response.output_item.added":
			if item["type"] == "reasoning" {
				addedIDs = append(addedIDs, item["id"].(string))
			}
		case "response.output_item.done":
			if item["type"] == "reasoning" {
				done = append(done, item)
			}
		case "response.reasoning_summary_text.delta":
			if ev["item_id"] != "rs_6820f383d7c08191846711c5df8233bc0ac5ba57aafcbac7" {
				t.Fatalf("summary delta for %v", ev["item_id"])
			}
		case "response.completed":
			for _, it := range ev["response"].(map[string]any)["output"].([]any) {
				if it.(map[string]any)["type"] == "reasoning" {
					completed = append(completed, it)
				}
			}
		}
	}
	if len(addedIDs) != 2 || addedIDs[0] != "rs_6820f383d7c08191846711c5df8233bc0ac5ba57aafcbac7" || addedIDs[1] != "rs_6820f3b2e9fc8191a5c7d0ab1c8e2f4a0ac5ba57aafcbac7" {
		t.Fatalf("added reasoning ids %v", addedIDs)
	}
	for name, items := range map[string][]any{"done": done, "completed": completed} {
		if len(items) != 2 || !jsonIs(t, items[0], rsSummarized) || !jsonIs(t, items[1], rsSealed) {
			t.Fatalf("%s reasoning items %v", name, items)
		}
	}
}

// The same for a client that didn't stream.
func TestResponsesWholeReplyKeepsSealedReasoning(t *testing.T) {
	out := translateWhole(t, provider.Responses, provider.Responses, responsesSealedStream...)
	var items []any
	for _, it := range out["output"].([]any) {
		if it.(map[string]any)["type"] == "reasoning" {
			items = append(items, it)
		}
	}
	if len(items) != 2 || !jsonIs(t, items[0], rsSummarized) || !jsonIs(t, items[1], rsSealed) {
		t.Fatalf("reasoning items %v", items)
	}
}

// OpenAI's sealed reasoning is nothing to another API's client: a
// Messages client gets the summary as thinking, never the seal.
func TestResponsesSealedReasoningStaysWithResponses(t *testing.T) {
	for _, from := range []provider.Protocol{provider.Anthropic, provider.Chat, provider.Gemini} {
		b, _ := json.Marshal(translateWhole(t, provider.Responses, from, responsesSealedStream...))
		s, _ := json.Marshal(translateStream(t, provider.Responses, from, responsesSealedStream...))
		if bytes.Contains(b, []byte("gAAAAABo")) || bytes.Contains(s, []byte("gAAAAABo")) {
			t.Fatalf("%s client handed OpenAI's sealed reasoning:\n%s\n%s", from, b, s)
		}
		if !bytes.Contains(b, []byte("Checking the weather")) {
			t.Fatalf("%s client lost the summary: %s", from, b)
		}
	}
}

// withID is a reasoning item raw under another id.
func withID(t *testing.T, raw, id string) string {
	t.Helper()
	var m map[string]any
	if err := decodeJSON([]byte(raw), &m); err != nil {
		t.Fatal(err)
	}
	m["id"] = id
	b, _ := json.Marshal(m)
	return string(b)
}

// cpStream is responsesSealedStream from an upstream whose reasoning ids
// aren't OpenAI's rs_ … (Copilot's begin Cp6820…): the item that streams
// its summary went out added and in every delta by one id, and comes back
// done as the sealed item by another (the same one when the upstream's ids
// are stable, as the summary's raw on done carries).
func cpStream(t *testing.T, summarizedAdded, summarizedDone, sealedAdded, sealedDone string) []string {
	t.Helper()
	rp := strings.NewReplacer(
		rsSummarized, withID(t, rsSummarized, summarizedDone),
		"rs_6820f383d7c08191846711c5df8233bc0ac5ba57aafcbac7", summarizedAdded,
		rsSealed, withID(t, rsSealed, sealedDone),
		"rs_6820f3b2e9fc8191a5c7d0ab1c8e2f4a0ac5ba57aafcbac7", sealedAdded,
	)
	out := make([]string, len(responsesSealedStream))
	for i, l := range responsesSealedStream {
		out[i] = rp.Replace(l)
	}
	return out
}

// A reasoning item the upstream seals at its done is the one that streamed
// its summary, whatever id it went by: the client reads the summary once.
// The old encoder knew its streamed item only by an OpenAI rs_ id, closed
// it unsealed at the seal, then added the sealed item again, so a client
// of the translated path read the summary as two items (#2974).
func TestResponsesStreamForeignReasoningIDNotDoubled(t *testing.T) {
	for name, ids := range map[string]struct{ sumA, sumD, sealA, sealD string }{
		"stable ids":                {sumA: "Cp6820f38x", sumD: "Cp6820f38x", sealA: "Cp6820f3bx", sealD: "Cp6820f3bx"},
		"ids that change per event": {sumA: "Cp6820f38a", sumD: "Cp6820f38b", sealA: "Cp6820f3ba", sealD: "Cp6820f3bb"},
	} {
		t.Run(name, func(t *testing.T) {
			evs := translateStream(t, provider.Responses, provider.Responses,
				cpStream(t, ids.sumA, ids.sumD, ids.sealA, ids.sealD)...)
			var added []string
			var done, completed []any
			deltas := 0
			for _, ev := range evs {
				item, _ := ev["item"].(map[string]any)
				switch ev["type"] {
				case "response.output_item.added":
					if item["type"] == "reasoning" {
						added = append(added, item["id"].(string))
					}
				case "response.output_item.done":
					if item["type"] == "reasoning" {
						done = append(done, item)
					}
				case "response.reasoning_summary_text.delta":
					deltas++
				case "response.completed":
					for _, it := range ev["response"].(map[string]any)["output"].([]any) {
						if it.(map[string]any)["type"] == "reasoning" {
							completed = append(completed, it)
						}
					}
				}
			}
			if deltas == 0 {
				t.Fatal("the summary never streamed")
			}
			if len(added) != 2 || !strings.HasPrefix(added[0], "rs_") || added[1] != ids.sealD {
				t.Fatalf("added reasoning ids %v, want the streamed item under the encoder's rs_ id and %s", added, ids.sealD)
			}
			for form, items := range map[string][]any{"done": done, "completed": completed} {
				if len(items) != 2 || !jsonIs(t, items[0], withID(t, rsSummarized, ids.sumD)) || !jsonIs(t, items[1], withID(t, rsSealed, ids.sealD)) {
					t.Fatalf("%s reasoning items %v, want 2: %s sealed by %s, then %s", form, items, rsSummarized, ids.sumD, rsSealed)
				}
			}
		})
	}
}

package gateway

import (
	"os"
	"strings"
	"testing"

	"github.com/tidwall/gjson"
)

// Copilot's /responses gives an item a new id in every event (the fixture
// is a real reply, gpt-5.5 offered web_search and a function, its
// encrypted parts cut short). Relayed to Codex, each item keeps the one
// id it was added with, in its events, its done and response.completed,
// so Codex shows the commentary once and ends the search it started
// (echo_ts on Discord).
func TestCopilotItemKeepsItsID(t *testing.T) {
	reply, err := os.ReadFile("testdata/copilot_rotating_ids.sse")
	if err != nil {
		t.Fatal(err)
	}
	c := copilotSol(t)
	c.mu.Lock()
	c.reply = string(reply)
	c.mu.Unlock()
	code, out := postTo(t, New(), "/v1/responses", `{"model":"copilot/gpt-6-luna","stream":true,"probe":"as sent",
		"input":[{"type":"message","role":"user","content":[{"type":"input_text","text":"Search for the latest Go release, then run go version."}]}],
		"tools":[{"type":"function","name":"exec_command","description":"Runs a command","strict":false,"parameters":{"type":"object","properties":{"cmd":{"type":"string"}},"required":["cmd"]}},{"type":"web_search"}]}`)
	if code != 200 {
		t.Fatalf("%d %s", code, out)
	}
	if c.last(t)["probe"] != "as sent" {
		t.Fatal("not relayed: the reply under test is Copilot's own")
	}
	ids := map[int64]map[string]bool{} // output_index → every id it was given
	note := func(at int64, id string) {
		if ids[at] == nil {
			ids[at] = map[string]bool{}
		}
		ids[at][id] = true
	}
	texts := map[string]int{}
	completed := false
	for _, line := range strings.Split(out, "\n") {
		data, ok := strings.CutPrefix(line, "data: ")
		if !ok {
			continue
		}
		e := gjson.Parse(data)
		switch e.Get("type").String() {
		case "response.completed":
			completed = true
			for i, it := range e.Get("response.output").Array() {
				note(int64(i), it.Get("id").String())
			}
			continue
		case "response.output_item.done":
			if it := e.Get("item"); it.Get("type").String() == "message" {
				texts[it.Get("content.0.text").String()]++
			}
		}
		at := e.Get("output_index")
		if !at.Exists() {
			continue
		}
		if id := e.Get("item.id"); id.Exists() {
			note(at.Int(), id.String())
		}
		if id := e.Get("item_id"); id.Exists() {
			note(at.Int(), id.String())
		}
	}
	if !completed || len(ids) != 4 {
		t.Fatalf("completed %v, items %d: %s", completed, len(ids), out)
	}
	for at, seen := range ids {
		if len(seen) != 1 {
			t.Errorf("item %d went by %d ids", at, len(seen))
		}
	}
	if len(texts) != 1 {
		t.Errorf("messages done: %v", texts)
	}
}

// An upstream whose ids agree, as OpenAI's do, goes through byte for byte.
func TestItemIDsThatAgreeGoAsTheyCame(t *testing.T) {
	stream := sse(
		`data: {"type":"response.created","response":{"id":"r1"}}`,
		`data: {"type":"response.output_item.added","output_index":0,"item":{"type":"message","id":"msg_1","role":"assistant","content":[]}}`,
		`data: {"type":"response.output_text.delta","output_index":0,"item_id":"msg_1","content_index":0,"delta":"a <b> & c"}`,
		`data: {"type":"response.output_item.done","output_index":0,"item":{"type":"message","id":"msg_1","role":"assistant","content":[{"type":"output_text","text":"a <b> & c"}]}}`,
		`data: {"type":"response.completed","response":{"id":"r1","output":[{"type":"message","id":"msg_1"}],"usage":{"input_tokens":1.0}}}`,
	)
	var got []byte
	tidy := &idTidy{}
	for i := 0; i < len(stream); i += 7 {
		got = append(got, tidy.write([]byte(stream[i:min(i+7, len(stream))]))...)
	}
	got = append(got, tidy.flush()...)
	if string(got) != stream {
		t.Errorf("changed:\n%s\nwant:\n%s", got, stream)
	}
}

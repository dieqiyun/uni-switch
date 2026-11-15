package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// liteConversation is largeConversation(Responses) in Codex's Lite shape:
// its tools in a first additional_tools item, in the "functions"
// namespace, and the model's calls to them under it.
func liteConversation(t testing.TB, words int) []byte {
	var m map[string]any
	if err := json.Unmarshal(largeConversation(provider.Responses, "gpt-5.6", words), &m); err != nil {
		t.Fatal(err)
	}
	tools := m["tools"]
	delete(m, "tools")
	in := m["input"].([]any)
	for _, it := range in {
		if im := it.(map[string]any); im["type"] == "function_call" {
			im["namespace"] = "functions"
		}
	}
	m["input"] = append([]any{map[string]any{"type": "additional_tools", "tools": []any{map[string]any{"type": "namespace", "name": "functions", "tools": tools}}}}, in...)
	b, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// A Lite request's tools are lifted as they were when the whole request
// was decoded to move them (liftWhole, the code before): the same request
// once decoded. What isn't moved goes byte for byte as it came: an item's
// keys in their order, its escapes as written.
func TestLiteLiftMovesOnlyTheTools(t *testing.T) {
	codex, err := os.ReadFile("testdata/context/codex.json")
	if err != nil {
		t.Fatal(err)
	}
	merged := `{"model":"m","tools":[{"type":"function","name":"shell","parameters":{"type":"object"}},
		{"type":"namespace","name":"mcp__probe","tools":[{"type":"function","name":"write"}]}],
		"input":[{"type":"additional_tools","tools":[
			{"type":"namespace","name":"functions","tools":[{"type":"function","name":"shell","description":"again"}]},
			{"type":"namespace","name":"mcp__probe","tools":[{"type":"function","name":"read"},{"type":"function","name":"write"}]}]},
		{"type":"message","role":"user","content":"hi"}]}`
	asSent := `{"role":"user","type":"message","content":[{"type":"input_text","text":"café <b> & 1.50"}]}`
	call := `{"type":"function_call","namespace":"functions","name":"shell","arguments":"{\"cmd\":[\"ls\"]}","call_id":"c1"}`
	kept := `{"stream":true,"model":"gpt-5.6","input":[{"type":"additional_tools","tools":[{"type":"namespace","name":"functions","tools":[{"type":"function","name":"shell"}]}]},` +
		asSent + `,` + call + `],"z":{"b":1,"a":2.50}}`
	for name, body := range map[string]string{
		"codex":        string(codex),
		"merged":       merged,
		"kept":         kept,
		"conversation": string(liteConversation(t, 2_000)),
		"none":         `{"model":"m","input":[{"type":"message","role":"user","content":"additional_tools"}]}`,
		"no input":     `{"model":"m","additional_tools":[]}`,
		"not a list":   `{"model":"m","input":"additional_tools"}`,
		"bad item":     `{"model":"m","input":[{"type":"additional_tools","tools":[]},1,"x",null]}`,
	} {
		got, want := liftAdditionalTools([]byte(body)), liftWhole([]byte(body))
		var g, w any
		if json.Unmarshal(got, &g) != nil || json.Unmarshal(want, &w) != nil || !reflect.DeepEqual(g, w) {
			t.Errorf("%s: lifted\n%s\nthe whole decode lifted\n%s", name, got, want)
		}
		if again := liftAdditionalTools(got); !bytes.Equal(again, got) {
			t.Errorf("%s: lifted twice: %s", name, again)
		}
	}
	out := string(liftAdditionalTools([]byte(kept)))
	for _, part := range []string{asSent, `"z":{"b":1,"a":2.50}`, `{"type":"function_call","name":"shell"`} {
		if !strings.Contains(out, part) {
			t.Errorf("%s isn't as it came in %s", part, out)
		}
	}
}

// Lifting a Lite request's tools allocates about the request, not the
// conversation decoded into maps and written again: ~7.5 times a 1.3 MB
// body on every read of it, twice a request (XiaoYin_OK on X: the app
// near 2 GB after a day of Codex).
func TestLiteLiftAllocatesAboutTheBody(t *testing.T) {
	body := liteConversation(t, 200_000)
	least := ^uint64(0)
	// the fewest of a few tries: another goroutine's allocations count in
	// TotalAlloc too (#3283)
	for range 3 {
		least = min(least, allocated(func() { liftAdditionalTools(body) }))
	}
	if least > uint64(len(body))*2 {
		t.Fatalf("lifting a %d-byte Lite request allocated %d bytes (%.1f×)", len(body), least, float64(least)/float64(len(body)))
	}
}

// A served request whose masked body differs from what its agent wrote
// has its conversation parsed once for its turn and its folder, not once
// for each: the turn was counted in the masked body, the folder read from
// the written one, two parses of the whole conversation for every
// request in flight.
func TestOriginalRequestParsedOnceForItsFacts(t *testing.T) {
	const key = "sk-proj-abcdEFGH1234ijklMNOP5678qrst"
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	f := &fake{ctype: "application/json", reply: `{"id":"r1","object":"response","status":"completed","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"ok"}]}],"usage":{"input_tokens":7,"output_tokens":1}}`}
	up := httptest.NewServer(f)
	defer up.Close()
	if err := provider.Save(provider.Provider{ID: "fake", Name: "Fake", Key: "k", Models: []string{"m1"}, Responses: up.URL + "/v1"}); err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	json.Unmarshal(liteConversation(t, 20_000), &m)
	m["model"] = "fake/m1"
	m["instructions"] = "Working directory: /work/" + t.Name() + "\nkey " + key
	body, _ := json.Marshal(m)
	var mu sync.Mutex
	parsed := 0
	seen := func(b []byte) {
		if len(b) > len(body)/2 {
			mu.Lock()
			parsed++
			mu.Unlock()
		}
	}
	factsParsed.Store(&seen)
	defer factsParsed.Store(nil)
	rec := httptest.NewRecorder()
	// a served request's context ends, as net/http's does: a body is held
	// only while one can (holdBody)
	ctx, done := context.WithCancel(context.Background())
	defer done()
	New().Handler().ServeHTTP(rec, httptest.NewRequest("POST", "/v1/responses", bytes.NewReader(body)).WithContext(ctx))
	if rec.Code != 200 || f.calls == 0 {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	if !strings.Contains(string(f.got), key) {
		t.Fatal("the original request text was changed")
	}
	mu.Lock()
	defer mu.Unlock()
	if parsed != 1 {
		t.Fatalf("the conversation was parsed %d times for its turn and folder", parsed)
	}
}

// liftWhole is liftAdditionalTools as it was: the whole request decoded
// into maps, the tools moved, and all of it written again.
func liftWhole(body []byte) []byte {
	if !bytes.Contains(body, []byte(`"additional_tools"`)) {
		return body
	}
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.UseNumber()
	var m map[string]any
	if dec.Decode(&m) != nil || m == nil {
		return body
	}
	items, ok := m["input"].([]any)
	if !ok {
		return body
	}
	tools, _ := m["tools"].([]any)
	have := toolsByName(tools)
	kept := items[:0:0]
	lifted := false
	for _, it := range items {
		im, _ := it.(map[string]any)
		if im["type"] != "additional_tools" {
			kept = append(kept, it)
			continue
		}
		lifted = true
		more, _ := im["tools"].([]any)
		for _, t := range more {
			tm, _ := t.(map[string]any)
			if tm == nil {
				continue
			}
			if tm["type"] == "namespace" && tm["name"] == liteNamespace {
				nested, _ := tm["tools"].([]any)
				for _, n := range nested {
					if nm, _ := n.(map[string]any); nm != nil {
						tools = addTool(tools, have, nm)
					}
				}
				continue
			}
			tools = addTool(tools, have, tm)
		}
	}
	if !lifted {
		return body
	}
	// a call the model made to one of Codex's own tools, under the
	// namespace they were grouped in, is the plain call it now is
	for _, it := range kept {
		if im, _ := it.(map[string]any); im != nil && im["namespace"] == liteNamespace {
			delete(im, "namespace")
		}
	}
	m["input"] = kept
	if len(tools) > 0 {
		m["tools"] = tools
	}
	out, err := marshalPlain(m)
	if err != nil {
		return body
	}
	return out
}

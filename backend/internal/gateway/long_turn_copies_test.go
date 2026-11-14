package gateway

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

// allocatedEach is what f allocates a run, over a few runs.
func allocatedEach(f func()) uint64 {
	f()
	const n = 4
	return allocated(func() {
		for range n {
			f()
		}
	}) / n
}

// A long Responses turn sent to a Chat upstream is copied as few times
// as it must be. A 3.2 MB Codex request allocated ~70 MB in the gateway,
// and that garbage, not what is kept, set the app's peak footprint: its
// text was unquoted into a buffer and copied again into a string, the
// user's words were joined only to see they were there, and the Chat
// message's text was built part by part, growing and copying.
func TestLongTurnCopiedFewTimes(t *testing.T) {
	var items []string
	for i := range 300 {
		text := strings.Repeat("读这个文件 read this file\n", 200) + strings.Repeat("x", i)
		q, _ := json.Marshal(text)
		items = append(items, `{"type":"message","role":"user","content":[{"type":"input_text","text":`+string(q)+`}]}`)
	}
	body := []byte(`{"model":"m","stream":true,"input":[` + strings.Join(items, ",") + `]}`)
	ctx, done := context.WithCancel(context.Background())
	defer done()
	holdBody(ctx, body) // as readRequestBody holds a request's
	r, err := readResponsesFor(body, false, false)
	if err != nil {
		t.Fatal(err)
	}
	size := uint64(len(body))

	if got := allocatedEach(func() { readResponsesFor(body, false, false) }); got > size*3 {
		t.Errorf("reading a %d-byte request allocated %d bytes (%.1f×)", size, got, float64(got)/float64(size))
	}
	if got := allocatedEach(func() { turnIn(r) }); got > size/16 {
		t.Errorf("finding a %d-byte request's turn allocated %d bytes", size, got)
	}
	// the text once for the merged turn, once in the marshalled body
	if got := allocatedEach(func() { buildChat(r, "m", "api.example.com", false) }); got > size*7/2 {
		t.Errorf("building a Chat body from a %d-byte request allocated %d bytes (%.1f×)", size, got, float64(got)/float64(size))
	}
	t.Logf("body %d: read %d, turn %d, chat %d", size,
		allocatedEach(func() { readResponsesFor(body, false, false) }), allocatedEach(func() { turnIn(r) }),
		allocatedEach(func() { buildChat(r, "m", "api.example.com", false) }))
}

// marshalSized writes what json.Marshal writes, byte for byte: every
// byte, invalid UTF-8, the HTML characters, U+2028 and U+2029, and the
// values it leaves to json.Marshal.
func TestMarshalSizedIsJSONMarshal(t *testing.T) {
	var all []byte
	for c := range 256 {
		all = append(all, byte(c))
	}
	temp := 0.7
	m := map[string]any{"a": 1}
	list := []map[string]any{{"x": "y"}}
	var values0 any = map[string]any{"role": "tool", "content": "<b>"}
	values := []any{
		nil, "", string(all), "a b c<>&\"\\\x7f\xed\xa0\x80é😀\xff", "读这个文件\n",
		true, false, 0, -42, int64(1) << 62, 1.5, &temp, []string{"a", "<"},
		map[string]any{"z": 1, "a": []any{nil, "x", 2, map[string]any{}}, "m": map[string]any(nil)},
		map[string]any{"<k>": "v", "é": 1, "q\"": 2},
		map[string]any{"raw": json.RawMessage(` {"a" : [1, 2]} `), "s": map[string]string{"b": "<"}},
		[]map[string]any{{"role": "user", "content": "hi\tthere"}, nil}, []map[string]any(nil),
		[]any(nil), []any{}, &m, &list, (*map[string]any)(nil), &values0, (*any)(nil),
	}
	for _, v := range values {
		want, werr := json.Marshal(v)
		got, gerr := marshalSized(v)
		if (werr == nil) != (gerr == nil) || string(got) != string(want) {
			t.Errorf("%#v: wrote %s (%v), json.Marshal %s (%v)", v, got, gerr, want, werr)
		}
		if n, ok := jsonSize(v); ok && n != len(want) {
			t.Errorf("%#v: sized %d, json.Marshal wrote %d", v, n, len(want))
		}
	}
	if _, err := marshalSized(map[string]any{"bad": func() {}}); err == nil {
		t.Error("a value json.Marshal refuses was written")
	}
}

package gateway

import (
	"bytes"
	"encoding/json"
	"math/rand/v2"
	"strings"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// mrCompare checks marshalRaw against encoding/json for v, with HTML
// escaped (json.Marshal) and without (an Encoder told not to): the same
// bytes, or encoding/json's error where marshalRaw declined.
func mrCompare(t *testing.T, v any) {
	t.Helper()
	for _, escape := range []bool{true, false} {
		var b bytes.Buffer
		enc := json.NewEncoder(&b)
		enc.SetEscapeHTML(escape)
		werr := enc.Encode(v)
		want := bytes.TrimRight(b.Bytes(), "\n")
		got, ok := marshalRaw(v, escape)
		if !ok {
			if werr == nil {
				t.Fatalf("escape %v: marshalRaw declined %#v, encoding/json wrote %q", escape, v, want)
			}
			continue
		}
		if werr != nil {
			t.Fatalf("escape %v: marshalRaw wrote %q, encoding/json refused: %v", escape, got, werr)
		}
		if !bytes.Equal(got, want) {
			t.Fatalf("escape %v:\n marshalRaw   %q\n encoding/json %q", escape, got, want)
		}
	}
}

// mrValues are raw values as a request's members and items hold them:
// spaced, escaped, HTML and the line separators in strings, nothing, and
// what isn't JSON at all.
var mrValues = []string{
	`1`, `-0.5e+3`, `true`, `null`, `""`, `"plain"`, `"a<b>&c"`, "\"line\u2028sep\u2029\"",
	"\"\xe2\x80\"", "\"\xff\xfe bad utf8\"", `"esc \" \\ \/ \u003c \n"`,
	` { "a" : [ 1 , 2 , { "b" : "x y" } ] , "c" : null } `, "\t[\n\"x\"\r\n]\n",
	`{"html":"<script>&amp;</script>","n":[1,2.5,-3e10]}`, `[]`, `{}`, `[ ]`, `{ }`,
	``, ` `, `{`, `[1,]`, `"open`, `tru`, `01`, `{"a":1}{"b":2}`, `nul`,
	strings.Repeat("[", 10001) + strings.Repeat("]", 10001),
}

// The keys a body's members have: plain, needing an escape, and those
// encoding/json escapes only with HTML.
var mrKeys = []string{"model", "input", "", "a b", "<tag>", "a&b", "quo\"te", "back\\slash", "tab\t", "é", "\u2028", "\xff", "\x7f", "z", "A"}

func TestMarshalRawIsEncodingJSON(t *testing.T) {
	mrCompare(t, map[string]json.RawMessage(nil))
	mrCompare(t, []json.RawMessage(nil))
	mrCompare(t, map[string]json.RawMessage{})
	mrCompare(t, []json.RawMessage{})
	mrCompare(t, map[string]json.RawMessage{"a": nil})
	mrCompare(t, []json.RawMessage{nil, json.RawMessage("1")})
	for _, v := range mrValues {
		mrCompare(t, []json.RawMessage{json.RawMessage(v)})
		for _, k := range mrKeys {
			mrCompare(t, map[string]json.RawMessage{k: json.RawMessage(v), "input": json.RawMessage(`[1]`)})
		}
	}
	all := map[string]json.RawMessage{}
	for i, k := range mrKeys {
		all[k] = json.RawMessage(mrValues[i%14])
	}
	mrCompare(t, all)
	// whole bodies split at their members and items, as the rewrites do
	for _, proto := range []provider.Protocol{provider.Anthropic, provider.Chat, provider.Responses} {
		body := largeConversation(proto, "m", 300)
		var q map[string]json.RawMessage
		if err := json.Unmarshal(body, &q); err != nil {
			t.Fatal(err)
		}
		mrCompare(t, q)
		for _, v := range q {
			var items []json.RawMessage
			if json.Unmarshal(v, &items) == nil {
				mrCompare(t, items)
			}
		}
	}
	for _, s := range fjSeeds {
		var q map[string]json.RawMessage
		if json.Unmarshal([]byte(s), &q) == nil {
			mrCompare(t, q)
		}
	}
	// every one-byte change of a spaced value
	r := rand.New(rand.NewPCG(3, 4))
	for _, s := range mrValues[11:14] {
		for i := range len(s) {
			b := []byte(s)
			b[i] = " \t\n{}[]\",:\\<>&nul0e.-x\xe2\xff"[r.IntN(24)]
			mrCompare(t, []json.RawMessage{b})
			mrCompare(t, map[string]json.RawMessage{"k": b})
		}
	}
}

// marshalPlain writes what it wrote through encoding/json, for these
// types as for any other.
func TestMarshalPlainRawTypes(t *testing.T) {
	q := map[string]json.RawMessage{"b": json.RawMessage(` "<x>" `), "a": json.RawMessage(`[ 1 ]`)}
	got, err := marshalPlain(q)
	if err != nil || string(got) != `{"a":[1],"b":"<x>"}` {
		t.Fatalf("marshalPlain = %q, %v", got, err)
	}
	if _, err := marshalPlain(map[string]json.RawMessage{"a": json.RawMessage(`{`)}); err == nil {
		t.Fatal("marshalPlain took a value that isn't JSON")
	}
}

func FuzzMarshalRaw(f *testing.F) {
	for i, v := range mrValues[:len(mrValues)-1] {
		f.Add([]byte(v), mrKeys[i%len(mrKeys)])
	}
	f.Fuzz(func(t *testing.T, data []byte, key string) {
		mrCompare(t, []json.RawMessage{data, nil})
		mrCompare(t, map[string]json.RawMessage{key: data, "model": json.RawMessage(`"m"`)})
	})
}

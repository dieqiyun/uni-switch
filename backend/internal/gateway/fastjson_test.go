package gateway

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math/rand/v2"
	"reflect"
	"strings"
	"testing"
	"time"
	"unsafe"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// The types a request body is decoded into, in the shapes they take.
type fjBlock struct {
	Type    string          `json:"type"`
	Text    string          `json:"text"`
	ID      string          `json:"id,omitempty"`
	Input   json.RawMessage `json:"input"`
	Content json.RawMessage `json:"content"`
	Source  *struct {
		Type string `json:"type"`
		Data string `json:"data"`
	} `json:"source"`
	Cache map[string]string `json:"cache_control"`
}

type fjBody struct {
	Model    string `json:"model"`
	Stream   bool   `json:"stream"`
	MaxTok   int    `json:"max_tokens"`
	Temp     *float64
	System   json.RawMessage `json:"system"`
	Messages []struct {
		Role    string          `json:"role"`
		Content json.RawMessage `json:"content"`
		Blocks  []fjBlock       `json:"blocks"`
	} `json:"messages"`
	Tools    []map[string]json.RawMessage `json:"tools"`
	Thinking *struct {
		Type   string `json:"type"`
		Budget int    `json:"budget_tokens"`
	} `json:"thinking"`
	Meta     map[string]any `json:"metadata"`
	Any      any            `json:"any"`
	Bytes    []byte         `json:"bytes"`
	Arr      [2]string      `json:"arr"`
	Args     rArgs          `json:"args"`
	At       *time.Time     `json:"at"`
	When     time.Time      `json:"when"`
	Ptrs     []*fjBlock     `json:"ptrs"`
	Strs     []string       `json:"strs"`
	ignored  string
	Skip     string `json:"-"`
	Untagged string
}

type fjEmbed struct {
	fjBlock
	Extra string `json:"extra"`
}

type fjNumStr struct {
	N int `json:"n,string"`
}

type fjClash struct {
	A string `json:"X"`
	X string
}

var fjSeeds = []string{
	`{}`, `[]`, `null`, `true`, `"x"`, `1`, ` {"model" : "m" } `,
	`{"model":"claude","stream":true,"max_tokens":32000,"messages":[{"role":"user","content":"hi"},{"role":"assistant","content":[{"type":"text","text":"a\nb"}],"blocks":[{"type":"tool_use","id":"t","input":{"a":[1,2,{"b":null}]}}]}]}`,
	`{"Model":"fold","MODEL":"again"}`,
	`{"model":"a","model":"b"}`,
	`{"messages":null,"tools":[],"thinking":null,"system":null}`,
	`{"messages":[null,{"role":null}],"ptrs":[null,{"type":"x"}]}`,
	`{"model":1}`, `{"stream":"yes"}`, `{"messages":{}}`, `{"tools":[1]}`,
	`{"model":"\u00e9\ud83d\ude00\ud800x\u0000\"\\\/\b\f\n\r\t"}`,
	"{\"model\":\"\xff\xfe raw invalid utf8 \xe6\xbc\"}",
	`{"mod\u0065l":"escaped key","str\u0073":["a"]}`,
	`{"metadata":{"user_id":"u","n":1.5e3,"deep":{"a":[true,false,null]}},"any":[1,"a",{}]}`,
	`{"bytes":"aGVsbG8=","arr":["a","b","c"],"args":{"k":"v"},"Temp":0.5}`,
	`{"args":"{\"k\":1}","Untagged":"u","Skip":"s","ignored":"i"}`,
	`{"tools":[{"name":"a","input_schema":{"type":"object"}},{"name":"b"}]}`,
	`{"messages":[{"blocks":[{"source":{"type":"base64","data":"AAA"},"cache_control":{"type":"ephemeral"}}]}]}`,
	`{"model":"x"} trailing`, `{"model":"x",}`, `{"model":}`, `[1,]`, `{"a":01}`, `{"a":-}`, `{"a":1.}`, `{"a":1e}`,
	`{"a":"\x"}`, `{"a":"\u12g4"}`, "{\"a\":\"\x01\"}", `{"a":tru}`, `{"a" "b"}`, `{`, `"`, ``, ` `,
	`{"extra":"e","type":"t"}`, `{"n":"12"}`, `{"x":"1"}`, `{"X":"2"}`,
	`{"at":"2024-01-01T00:00:00Z","when":"2024-02-01T00:00:00+08:00","model":"m"}`, `{"at":null,"when":null}`, `{"at":1}`, `{"args":null}`,
	` [1, {"a":"b"}] `, "                \t   {\"model\":\"m\"}                 \n          ", "                {\"model\" :                 \"m\"}       \x00", "\n{\"a\":[\"x\"]}", `[{"type":"text"},"s"]`,
	`"a long string with no escapes at all, past eight bytes"`, `"sevené then \"quoted\" \\ and \t past eight"`,
	"\"a control byte \x1f deep in a long string\"", "\"\x7f\x80 high bytes \xc3\xa9 \xe2\x82\xac in a long string\"",
	`{"strs":["a",null,"b"]}`, `{"strs":"notarray"}`, `{"metadata":null}`, `{"metadata":[]}`,
	`{"modèl":"a non-ASCII key","Ｍodel":"fullwidth","MODEL":"b","Model":"c"}`, "{\"mo\x7fdel\":\"d\",\"mo\x1fdel\":1}",
}

func fjCompare(t *testing.T, data []byte) {
	t.Helper()
	if got, want := jsonValid(data), json.Valid(data); got != want {
		t.Fatalf("jsonValid(%q) = %v, json.Valid %v", data, got, want)
	}
	for _, mk := range []func() any{
		func() any { return new(fjBody) },
		func() any { return new(fjBlock) },
		func() any { return new([]fjBlock) },
		func() any { return new(map[string]json.RawMessage) },
		func() any { return new([]json.RawMessage) },
		func() any { return new(json.RawMessage) },
		func() any { return new(any) },
		func() any { return new(string) },
		func() any { return new(*string) },
		func() any { return new(bool) },
		func() any { return new(int) },
		func() any { return new([]string) },
		func() any { return new(map[string]string) },
		func() any { return new(rArgs) },
		func() any { return new(fjEmbed) },
		func() any { return new(fjNumStr) },
		func() any { return new(fjClash) },
		func() any { return new(*fjBody) },
		func() any {
			return &fjBody{Model: "old", Strs: []string{"a", "b", "c"}, Ptrs: []*fjBlock{{Type: "old"}}}
		},
	} {
		got, want := mk(), mk()
		gerr := decodeJSON(data, got)
		werr := json.Unmarshal(data, want)
		if (gerr == nil) != (werr == nil) {
			t.Fatalf("%T %q: decodeJSON error %v, json.Unmarshal %v", got, data, gerr, werr)
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("%T %q:\n decodeJSON   %#v\n json.Unmarshal %#v", got, data, got, want)
		}
		view := mk()
		if verr := decodeJSONView(data, view); (verr == nil) != (werr == nil) || !reflect.DeepEqual(view, want) {
			t.Fatalf("%T %q:\n decodeJSONView %#v %v\n json.Unmarshal %#v %v", view, data, view, verr, want, werr)
		}
	}
}

func TestDecodeJSONIsUnmarshal(t *testing.T) {
	for _, s := range fjSeeds {
		fjCompare(t, []byte(s))
	}
	for _, proto := range []provider.Protocol{provider.Anthropic, provider.Chat, provider.Responses} {
		fjCompare(t, largeConversation(proto, "m", 2000))
	}
	// every cut and every one-byte change of a real body
	r := rand.New(rand.NewPCG(1, 2))
	for _, s := range fjSeeds {
		for i := range len(s) {
			fjCompare(t, []byte(s[:i]))
			b := []byte(s)
			b[i] = "{}[]\",:\\ nul0e.-x\xff"[r.IntN(18)]
			fjCompare(t, b)
		}
	}
}

// A struct marks the fields it has seen in a word's bits, or in a slice
// when it has more than 64: a key twice is refused the same in either.
func TestDecodeJSONKeyTwice(t *testing.T) {
	var fields []reflect.StructField
	for i := range 70 {
		fields = append(fields, reflect.StructField{Name: fmt.Sprintf("F%d", i), Type: reflect.TypeFor[string](), Tag: reflect.StructTag(fmt.Sprintf(`json:"f%d"`, i))})
	}
	wide := reflect.StructOf(fields)
	for _, s := range []string{`{"f0":"a","f69":"b","f65":"c"}`, `{"f66":"a","f66":"b"}`, `{"f3":"a","F3":"b"}`, `{"f69":1}`} {
		got, want := reflect.New(wide).Interface(), reflect.New(wide).Interface()
		gerr, werr := decodeJSON([]byte(s), got), json.Unmarshal([]byte(s), want)
		if (gerr == nil) != (werr == nil) || !reflect.DeepEqual(got, want) {
			t.Fatalf("%s: decodeJSON %v %v, json.Unmarshal %v %v", s, got, gerr, want, werr)
		}
	}
	// the fast path's own refusal, which encoding/json then decodes
	for _, s := range []string{`{"f66":"a","f66":"b"}`, `{"f3":"a","F3":"b"}`} {
		if err := fillJSON([]byte(s), reflect.New(wide).Elem(), false); err != errSlowJSON {
			t.Fatalf("%s: fillJSON %v, want errSlowJSON", s, err)
		}
	}
	for _, s := range []string{`{"model":"a","model":"b"}`, `{"Model":"a","MODEL":"b"}`} {
		if err := fillJSON([]byte(s), reflect.ValueOf(new(fjBody)).Elem(), false); err != errSlowJSON {
			t.Fatalf("%s: fillJSON %v, want errSlowJSON", s, err)
		}
	}
}

// decodeJSONView's raw values are the bytes it read, capped, however
// small a share of them: a rewritten body, which nobody holds, is read
// without a copy of its input.
func TestDecodeJSONViewShares(t *testing.T) {
	body := []byte(`{"model":"m","input":[{"type":"message","content":[{"type":"input_text","text":"hi"}]},{"type":"function_call_output","call_id":"c","output":"ok"}],"tools":[]}`)
	var q map[string]json.RawMessage
	if err := decodeJSONView(body, &q); err != nil {
		t.Fatal(err)
	}
	var item struct {
		Content json.RawMessage `json:"content"`
	}
	in := elementsOf(q["input"])
	if err := decodeJSONView(in[0], &item); err != nil {
		t.Fatal(err)
	}
	for name, raw := range map[string]json.RawMessage{"input": q["input"], "tools": q["tools"], "content": item.Content} {
		i := bytes.Index(body, raw)
		if i < 0 || unsafe.SliceData(raw) != unsafe.SliceData(body[i:]) || cap(raw) != len(raw) {
			t.Fatalf("%s %s: not body's own bytes, capped", name, raw)
		}
	}
	var copied map[string]json.RawMessage
	if err := decodeJSON(body, &copied); err != nil {
		t.Fatal(err)
	}
	if unsafe.SliceData(copied["tools"]) == unsafe.SliceData(body[bytes.Index(body, []byte(`[]`)):]) {
		t.Fatal("decodeJSON shared a body nobody holds")
	}
}

func TestDecodeJSONDepth(t *testing.T) {
	for _, n := range []int{9999, 10000, 10001} {
		fjCompare(t, []byte(strings.Repeat("[", n)+strings.Repeat("]", n)))
		fjCompare(t, []byte(`{"any":`+strings.Repeat("[", n)+strings.Repeat("]", n)+`}`))
	}
}

func FuzzDecodeJSON(f *testing.F) {
	for _, s := range fjSeeds {
		f.Add([]byte(s))
	}
	f.Fuzz(func(t *testing.T, data []byte) { fjCompare(t, data) })
}

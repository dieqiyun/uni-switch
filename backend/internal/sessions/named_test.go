package sessions

import (
	"encoding/json"
	"testing"
)

// A parse's models, days and tools are written as the maps they were, byte
// for byte, so a cache saved before reads the same, and read back as they
// were written: a name given twice is its last, and a number that isn't an
// int fails the read as it failed into a map.
func TestNamedIsAMapInJSON(t *testing.T) {
	m := map[string]Tokens{"z<&>": {Input: 1, CacheWrite1h: 2}, "a": {Output: -3}, "mé": {}}
	var n named[Tokens]
	for k, v := range m {
		*n.at(k) = v
	}
	want, _ := json.Marshal(m)
	got, err := json.Marshal(n)
	if err != nil || string(got) != string(want) {
		t.Fatalf("named wrote %s, %v; a map writes %s", got, err, want)
	}
	var back named[Tokens]
	if err := json.Unmarshal(got, &back); err != nil || len(back) != len(m) {
		t.Fatalf("read back %v, %v", back, err)
	}
	for k, v := range m {
		if back.of(k) != v {
			t.Errorf("%q read back %+v, want %+v", k, back.of(k), v)
		}
	}

	var d struct {
		T named[int]    `json:"t,omitempty"`
		M named[Tokens] `json:"m,omitempty"`
	}
	if err := json.Unmarshal([]byte(`{"t":{"b":1,"a":2,"b":3},"m":{"x":{"input":5,"extra":1}}}`), &d); err != nil {
		t.Fatal(err)
	}
	if len(d.T) != 2 || d.T.of("a") != 2 || d.T.of("b") != 3 || d.T[0].name != "a" {
		t.Errorf("tools read as %v, want a=2 b=3 in order", d.T)
	}
	if d.M.of("x") != (Tokens{Input: 5}) {
		t.Errorf("a field Tokens doesn't have read as %+v", d.M.of("x"))
	}
	if b, _ := json.Marshal(struct {
		T named[int] `json:"t,omitempty"`
	}{}); string(b) != `{}` {
		t.Errorf("none written as %s, want it left out", b)
	}

	for _, bad := range []string{`{"t":{"a":1.5}}`, `{"t":{"a":"1"}}`, `{"t":[1]}`, `{"m":{"x":{"input":1e3}}}`, `{"m":{"x":{"input":99999999999999999999}}}`} {
		var into struct {
			T named[int]    `json:"t"`
			M named[Tokens] `json:"m"`
		}
		var asMap struct {
			T map[string]int    `json:"t"`
			M map[string]Tokens `json:"m"`
		}
		if (json.Unmarshal([]byte(bad), &into) == nil) != (json.Unmarshal([]byte(bad), &asMap) == nil) {
			t.Errorf("%s: named and a map disagree on whether it reads", bad)
		}
	}
}

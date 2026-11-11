package provider

import (
	"encoding/json"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

// withModel asks for the plugin's model and leaves the rest of the
// request as it was, a "model" deeper in the body included.
func TestWithModelAsksForTheModel(t *testing.T) {
	for _, c := range []struct{ in, want string }{
		{`{"model":"a","messages":[{"role":"user","content":"<x>","model":"keep"}]}`, `{"model":"b","messages":[{"role":"user","content":"<x>","model":"keep"}]}`},
		{`{ "stream" : true , "model" : "a" }`, `{ "stream" : true , "model" : "b" }`},
		{`{"model":"a\"q","x":1}`, `{"model":"b","x":1}`},
		{`{"x":1}`, `{"model":"b","x":1}`},
		{`{"model":1,"x":1}`, `{"model":"b","x":1}`},
		{`{"model":"a","model":"c","x":1}`, `{"model":"b","x":1}`},
		{`{"model":"a",`, `{"model":"a",`},
		{`null`, `null`},
	} {
		got := string(withModel([]byte(c.in), "b"))
		if got == c.want {
			continue
		}
		var g, w any
		if json.Unmarshal([]byte(got), &g) != nil || json.Unmarshal([]byte(c.want), &w) != nil || !reflect.DeepEqual(g, w) {
			t.Errorf("withModel(%s) = %s, want %s", c.in, got, c.want)
		}
	}
}

// A 45 MB request is copied once to ask for another model, not decoded
// and marshalled again (#4004).
func TestWithModelCopiesOnce(t *testing.T) {
	body := []byte(`{"model":"a","messages":[{"role":"tool","content":"` + strings.Repeat("output <line>\\n", 3<<20) + `"}]}`)
	runtime.GC()
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	before := m.TotalAlloc
	out := withModel(body, "plugin-model")
	runtime.ReadMemStats(&m)
	n := m.TotalAlloc - before
	t.Logf("%d MiB body: withModel allocated %d MiB", len(body)>>20, n>>20)
	if n > uint64(len(body))*11/10 {
		t.Errorf("withModel allocated %d MiB for a %d MiB body", n>>20, len(body)>>20)
	}
	if !strings.HasPrefix(string(out), `{"model":"plugin-model","messages":`) {
		t.Errorf("withModel wrote %.60s", out)
	}
}

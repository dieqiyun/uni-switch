package gateway

import (
	"bufio"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// gatedCall is a Responses upstream that streams a call's first argument
// delta and then waits, before the rest of the call, until the test has
// seen that delta reach the client (or gives up).
type gatedCall struct {
	name  string        // the function the upstream calls
	first string        // its first argument delta
	rest  string        // the rest of its arguments
	seen  chan struct{} // closed by the test once the client has the first delta
	wrote chan struct{} // closed by the upstream as it goes on to the rest
}

func (g *gatedCall) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	io.ReadAll(r.Body)
	w.Header().Set("Content-Type", "text/event-stream")
	f := w.(http.Flusher)
	q := func(s string) string { b, _ := json.Marshal(s); return string(b) }
	write := func(lines ...string) {
		io.WriteString(w, sse(lines...))
		f.Flush()
	}
	write(`data: {"type":"response.created","response":{"id":"r1","model":"m"}}`,
		`data: {"type":"response.output_item.added","output_index":0,"item":{"type":"function_call","id":"fc_1","call_id":"call_1","name":"`+g.name+`","arguments":""}}`,
		`data: {"type":"response.function_call_arguments.delta","output_index":0,"item_id":"fc_1","delta":`+q(g.first)+`}`)
	select {
	case <-g.seen:
	case <-time.After(20 * time.Second):
	case <-r.Context().Done():
		return
	}
	close(g.wrote)
	args := q(g.first + g.rest)
	write(`data: {"type":"response.function_call_arguments.delta","output_index":0,"item_id":"fc_1","delta":`+q(g.rest)+`}`,
		`data: {"type":"response.function_call_arguments.done","output_index":0,"item_id":"fc_1","arguments":`+args+`}`,
		`data: {"type":"response.output_item.done","output_index":0,"item":{"type":"function_call","id":"fc_1","call_id":"call_1","name":"`+g.name+`","arguments":`+args+`,"status":"completed"}}`,
		`data: {"type":"response.completed","response":{"id":"r1","status":"completed","output":[],"usage":{"input_tokens":5,"output_tokens":5}}}`)
}

// A call's arguments reach Codex as the upstream writes them, on the
// translated path its web_search sends a request down (echo_ts on Discord:
// on Copilot, Codex saw nothing for the minutes the model wrote a patch or
// a command, then all of it at once). A function's arguments come as
// function_call_arguments deltas, a custom tool's input (apply_patch) as
// custom_tool_call_input deltas, each before the upstream's call is done;
// the finished items are what they were.
func TestCallArgsStreamWhenTranslated(t *testing.T) {
	const patch = "*** Begin Patch\n*** Add File: a.go\n+package a\n+// \"quoted\" \\ é 😀\n*** End Patch\n"
	whole, _ := json.Marshal(map[string]string{"input": patch})
	split := 24 // inside the patch: {"input":"*** Begin Pat
	for _, c := range []struct {
		name, fn     string
		first, rest  string
		delta        string // the event the client is given the call's text by
		wantFirst    string // what that event's first delta says
		wantItemType string
	}{
		{"function", "exec_command", `{"cmd":"go test ./...",`, `"yield_time_ms":1000}`,
			"response.function_call_arguments.delta", `{"cmd":"go test ./...",`, "function_call"},
		{"custom", "apply_patch", string(whole[:split]), string(whole[split:]),
			"response.custom_tool_call_input.delta", patch[:split-len(`{"input":"`)], "custom_tool_call"},
	} {
		t.Run(c.name, func(t *testing.T) {
			fresh(t)
			up := &gatedCall{name: c.fn, first: c.first, rest: c.rest, seen: make(chan struct{}), wrote: make(chan struct{})}
			vendor := httptest.NewServer(up)
			t.Cleanup(vendor.Close)
			if err := provider.Save(provider.Provider{ID: "rel", Name: "Rel", Key: "k", Responses: vendor.URL + "/v1", Models: []string{"m"}}); err != nil {
				t.Fatal(err)
			}
			srv := httptest.NewServer(New().Handler())
			t.Cleanup(srv.Close)
			body := `{"model":"rel/m","stream":true,"input":[{"role":"user","content":"add a.go"}],
				"tools":[{"type":"function","name":"exec_command","description":"Runs a command.","strict":false,
				  "parameters":{"type":"object","properties":{"cmd":{"type":"string"},"yield_time_ms":{"type":"number"}},"required":["cmd"]}},
				 {"type":"custom","name":"apply_patch","description":"Edits files.","format":{"type":"grammar","syntax":"lark","definition":"start: /.+/"}},
				 {"type":"web_search"}],"tool_choice":"auto"}`
			res, err := http.Post(srv.URL+"/v1/responses", "application/json", strings.NewReader(body))
			if err != nil {
				t.Fatal(err)
			}
			defer res.Body.Close()
			if res.StatusCode != 200 {
				b, _ := io.ReadAll(res.Body)
				t.Fatalf("%d %s", res.StatusCode, b)
			}
			var deltas []string
			var done map[string]any
			early := false
			sc := bufio.NewScanner(res.Body)
			sc.Buffer(make([]byte, 1<<20), 1<<20)
			for sc.Scan() {
				data, ok := strings.CutPrefix(sc.Text(), "data: ")
				if !ok {
					continue
				}
				var ev struct {
					Type  string         `json:"type"`
					Delta string         `json:"delta"`
					Item  map[string]any `json:"item"`
				}
				if json.Unmarshal([]byte(data), &ev) != nil {
					continue
				}
				switch {
				case ev.Type == c.delta:
					if len(deltas) == 0 {
						select {
						case <-up.wrote:
						default:
							early = true // the upstream is still holding the rest
						}
						close(up.seen)
					}
					deltas = append(deltas, ev.Delta)
				case ev.Type == "response.output_item.done" && ev.Item["type"] == c.wantItemType:
					done = ev.Item
				case ev.Type == "response.output_item.done" && done == nil:
					// another kind of item: what the call came back as
					done = ev.Item
				}
			}
			if !early {
				t.Fatalf("no %s before the upstream's call was done: deltas %q", c.delta, deltas)
			}
			if deltas[0] != c.wantFirst {
				t.Errorf("first delta %q, want %q", deltas[0], c.wantFirst)
			}
			if done == nil || done["type"] != c.wantItemType {
				t.Fatalf("finished item %v", done)
			}
			if c.wantItemType == "custom_tool_call" {
				if done["input"] != patch || strings.Join(deltas, "") != patch {
					t.Errorf("input %q, deltas %q, want %q", done["input"], strings.Join(deltas, ""), patch)
				}
			} else if done["arguments"] != c.first+c.rest || strings.Join(deltas, "") != c.first+c.rest {
				t.Errorf("arguments %q, deltas %q", done["arguments"], strings.Join(deltas, ""))
			}
		})
	}
}

// A custom tool's input read out of its arguments a piece at a time is
// the input the whole arguments hold, wherever the pieces are cut: in an
// escape, between a surrogate pair's halves, inside a character.
func TestCustomInputStreams(t *testing.T) {
	patch := "*** Begin Patch\n+\"q\" \\ é 😀 <a>&\t\n*** End Patch\n"
	marshalled, _ := json.Marshal(map[string]string{"input": patch})
	for _, c := range []struct{ args, want string }{
		{string(marshalled), patch},
		{`{"input":"\ud83d\ude00 \u00e9\u0041\/\n"}`, "😀 éA/\n"},
		{`{"path":"a.go","input":"x\"y"}`, `x"y`},
		{`{"meta":{"input":"no","list":["input"]},"input":"yes"}`, "yes"},
		{`{ "input" : "spaced" }`, "spaced"},
		{`"a lone string"`, "a lone string"},
		{`{"input":"lone \ud83d half"}`, "lone \ufffd half"},
		{`{"input":5}`, ""},
		{`{"other":"x"}`, ""},
		{`not json`, ""},
	} {
		for _, size := range []int{1, 2, 3, 5, 7, len(c.args)} {
			var s customStream
			var got strings.Builder
			for i := 0; i < len(c.args); i += size {
				got.WriteString(s.feed(c.args[i:min(i+size, len(c.args))]))
			}
			if got.String() != c.want {
				t.Errorf("%s in pieces of %d: %q, want %q", c.args, size, got.String(), c.want)
			}
			if c.want != "" && customInput(c.args) != c.want {
				t.Errorf("%s: customInput %q", c.args, customInput(c.args))
			}
		}
	}
}

// A Responses upstream's argument deltas are given as they come, except
// those that make the arguments whole JSON, which wait for the call's done
// events: what was given and what follows add up to the fuller arguments.
func TestResponsesCallArgsStream(t *testing.T) {
	var d responsesDecoder
	var got []string
	emit := func(ev Event) {
		if ev.Kind == KToolArgs {
			got = append(got, ev.Text)
		}
	}
	d.decode(`{"type":"response.output_item.added","item":{"type":"function_call","call_id":"c1","name":"f"}}`, emit)
	for _, x := range []string{`{"cmd":"a}`, `\"]{",`, `"n":[1,{}]`} {
		q, _ := json.Marshal(x)
		d.decode(`{"type":"response.function_call_arguments.delta","delta":`+string(q)+`}`, emit)
	}
	if want := []string{`{"cmd":"a}`, `\"]{",`, `"n":[1,{}]`}; strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("before the end %q, want %q", got, want)
	}
	d.decode(`{"type":"response.function_call_arguments.delta","delta":"}"}`, emit)
	if len(got) != 3 {
		t.Fatalf("the delta that makes it whole was given before the call's done: %q", got)
	}
	d.decode(`{"type":"response.output_item.done","item":{"type":"function_call","call_id":"c1","name":"f","arguments":"{\"cmd\":\"a}\\\"]{\",\"n\":[1,{}],\"more\":true}"}}`, emit)
	if all := strings.Join(got, ""); all != `{"cmd":"a}\"]{","n":[1,{}],"more":true}` {
		t.Fatalf("arguments %s", all)
	}
}

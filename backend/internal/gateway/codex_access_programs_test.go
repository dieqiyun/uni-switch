package gateway

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
	"github.com/tidwall/gjson"
)

// daybreakBackend stands in for the ChatGPT backend as OpenAI's Daybreak
// guide has the Responses API answer access_programs: a cyber program it
// doesn't know is a 400 invalid_access_program, null is refused as well.
// daybreak_red stands for a program the workspace hasn't got, which the
// real backend refuses inside a 200 stream. It keeps every body it was sent.
type daybreakBackend struct {
	mu     sync.Mutex
	bodies [][]byte
}

func (d *daybreakBackend) last(t *testing.T) []byte {
	t.Helper()
	d.mu.Lock()
	defer d.mu.Unlock()
	if len(d.bodies) == 0 {
		t.Fatal("the ChatGPT backend was asked nothing")
	}
	return d.bodies[len(d.bodies)-1]
}

func (d *daybreakBackend) asked() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return len(d.bodies)
}

func daybreak(t *testing.T) *daybreakBackend {
	t.Helper()
	d := &daybreakBackend{}
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/models") {
			io.WriteString(w, `{"models":[{"slug":"gpt-daybreak-blue-latest","display_name":"Daybreak Blue","visibility":"list","supported_reasoning_levels":[{"effort":"medium"}],"context_window":272000}]}`)
			return
		}
		b, _ := io.ReadAll(r.Body)
		d.mu.Lock()
		d.bodies = append(d.bodies, b)
		d.mu.Unlock()
		if ap := gjson.GetBytes(b, "access_programs"); ap.Exists() {
			if c := ap.Get("cyber"); ap.Type == gjson.Null || c.Exists() && c.String() != "standard" && c.String() != "daybreak_blue" && c.String() != "daybreak_red" {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(400)
				io.WriteString(w, `{"error":{"message":"Invalid access program for access_programs.cyber.","type":"invalid_request_error","param":"access_programs.cyber","code":"invalid_access_program"}}`)
				return
			}
		}
		w.Header().Set("Content-Type", "text/event-stream")
		if gjson.GetBytes(b, "access_programs.cyber").String() == "daybreak_red" {
			// what the real backend said for daybreak_blue to a workspace
			// without it: a 200 stream whose error event names the program
			io.WriteString(w, sse(
				`event: error`+"\n"+`data: {"type":"error","error":{"message":"The requested Cyber access program is not authorized for this workspace.","type":"invalid_request_error","param":"access_programs.cyber","code":"access_program_not_enabled"}}`))
			return
		}
		io.WriteString(w, sse(
			`data: {"type":"response.created","response":{"id":"r1","model":"gpt-daybreak-blue-latest"}}`,
			`data: {"type":"response.output_item.done","output_index":0,"item":{"type":"message","id":"msg_1","role":"assistant","content":[{"type":"output_text","text":"OK"}]}}`,
			`data: {"type":"response.completed","response":{"id":"r1","status":"completed","output":[],"usage":{"input_tokens":5,"output_tokens":1}}}`))
	}))
	t.Cleanup(up.Close)
	was := provider.CodexBase
	provider.CodexBase = up.URL + "/backend-api/codex"
	t.Cleanup(func() { provider.CodexBase = was })
	return d
}

// uni-switch-releases#18 (NonMirror, OpenCode): a client's access_programs, the
// Daybreak program Codex CLI sends the ChatGPT backend on each turn it was
// picked for (codex-rs ResponsesApiRequest.access_programs), reaches the
// backend on a Codex account's model on every path that asks it: Responses
// relayed (streamed) and made again (one body, which the account is
// streamed for), and Chat made again as Responses. The value goes as it
// came, so the backend's 400 invalid_access_program for a value it doesn't
// take reaches the client, the reporter's probe among them.
func TestCodexAccessProgramsReachBackend(t *testing.T) {
	codexSignedIn(t, "spare@example.com")
	d := daybreak(t)
	const blue = `{"cyber":"daybreak_blue"}`
	for _, c := range []struct{ name, path, body string }{
		{"responses streamed", "/v1/responses", `{"model":"codex/gpt-daybreak-blue-latest","stream":true,"input":"Respond with OK.","access_programs":` + blue + `}`},
		{"responses whole", "/v1/responses", `{"model":"codex/gpt-daybreak-blue-latest","stream":false,"input":"Respond with OK.","access_programs":` + blue + `}`},
		{"chat streamed", "/v1/chat/completions", `{"model":"codex/gpt-daybreak-blue-latest","stream":true,"messages":[{"role":"user","content":"Respond with OK."}],"access_programs":` + blue + `}`},
		{"chat whole", "/v1/chat/completions", `{"model":"codex/gpt-daybreak-blue-latest","messages":[{"role":"user","content":"Respond with OK."}],"access_programs":` + blue + `}`},
	} {
		code, out := post(t, c.path, c.body)
		if code != 200 {
			t.Fatalf("%s: %d %s", c.name, code, out)
		}
		if got := gjson.GetBytes(d.last(t), "access_programs").Raw; got != blue {
			t.Errorf("%s: the backend got access_programs %q in %s", c.name, got, d.last(t))
		}
	}

	// the reporter's probe, and null, which the guide says is refused: the
	// backend's own 400, its code and param as it said them, not a 200
	// from a request it never saw them in. The request is at fault, not
	// the account: the spare signed in beside it is never asked, and
	// nobody rests.
	for _, c := range []struct{ name, path, body string }{
		{"probe, responses whole", "/v1/responses", `{"model":"codex/gpt-daybreak-blue-latest","input":"Respond with OK.","stream":false,"access_programs":{"cyber":"__daybreak_forwarding_probe__"}}`},
		{"probe, responses streamed", "/v1/responses", `{"model":"codex/gpt-daybreak-blue-latest","input":"Respond with OK.","stream":true,"access_programs":{"cyber":"__daybreak_forwarding_probe__"}}`},
		{"probe, chat", "/v1/chat/completions", `{"model":"codex/gpt-daybreak-blue-latest","messages":[{"role":"user","content":"Respond with OK."}],"access_programs":{"cyber":"__daybreak_forwarding_probe__"}}`},
		{"probe, chat streamed", "/v1/chat/completions", `{"model":"codex/gpt-daybreak-blue-latest","stream":true,"messages":[{"role":"user","content":"Respond with OK."}],"access_programs":{"cyber":"__daybreak_forwarding_probe__"}}`},
		{"null, responses whole", "/v1/responses", `{"model":"codex/gpt-daybreak-blue-latest","input":"Respond with OK.","access_programs":null}`},
	} {
		before := d.asked()
		code, out := post(t, c.path, c.body)
		if code != 400 || gjson.Get(out, "error.code").String() != "invalid_access_program" || gjson.Get(out, "error.param").String() != "access_programs.cyber" {
			t.Errorf("%s: %d %s\nthe backend got %s", c.name, code, out, d.last(t))
		}
		if n := d.asked() - before; n != 1 {
			t.Errorf("%s: the backend was asked %d times, want once", c.name, n)
		}
		if r := restingNow(t); len(r) > 0 {
			t.Errorf("%s: resting after the request's own 400: %v", c.name, r)
		}
	}

	// a program the workspace hasn't got is refused inside the stream: a
	// client answered whole gets that refusal as a 400 with its code and
	// param, not a 502 after the same request was sent three times; one
	// answered as a stream gets it inside the stream. Asked once either
	// way, and nobody rests.
	for _, c := range []struct {
		name, path, body string
		whole            bool
	}{
		{"not enabled, responses whole", "/v1/responses", `{"model":"codex/gpt-daybreak-blue-latest","input":"Respond with OK.","stream":false,"access_programs":{"cyber":"daybreak_red"}}`, true},
		{"not enabled, chat whole", "/v1/chat/completions", `{"model":"codex/gpt-daybreak-blue-latest","messages":[{"role":"user","content":"Respond with OK."}],"access_programs":{"cyber":"daybreak_red"}}`, true},
		{"not enabled, chat streamed", "/v1/chat/completions", `{"model":"codex/gpt-daybreak-blue-latest","stream":true,"messages":[{"role":"user","content":"Respond with OK."}],"access_programs":{"cyber":"daybreak_red"}}`, false},
		{"not enabled, responses streamed", "/v1/responses", `{"model":"codex/gpt-daybreak-blue-latest","input":"Respond with OK.","stream":true,"access_programs":{"cyber":"daybreak_red"}}`, false},
	} {
		before := d.asked()
		code, out := post(t, c.path, c.body)
		if c.whole {
			if code != 400 || gjson.Get(out, "error.code").String() != "access_program_not_enabled" || gjson.Get(out, "error.param").String() != "access_programs.cyber" {
				t.Errorf("%s: %d %s", c.name, code, out)
			}
		} else if !strings.Contains(out, "not authorized for this workspace") {
			t.Errorf("%s: %d %s", c.name, code, out)
		}
		if n := d.asked() - before; n != 1 {
			t.Errorf("%s: the backend was asked %d times, want once", c.name, n)
		}
		if r := restingNow(t); len(r) > 0 {
			t.Errorf("%s: resting after the request's own refusal: %v", c.name, r)
		}
	}

	// none sent, none added
	if code, out := post(t, "/v1/responses", `{"model":"codex/gpt-daybreak-blue-latest","input":"hi"}`); code != 200 {
		t.Fatalf("without: %d %s", code, out)
	}
	if gjson.GetBytes(d.last(t), "access_programs").Exists() {
		t.Errorf("access_programs added: %s", d.last(t))
	}
}

// uni-switch-releases#18: the extraBody written for the built-in codex
// provider (by hand: its editor has no box for it) goes on its requests,
// as a key provider's does — filling in what the client left out, a
// "!name" replacing it — on every API the request came in on. What Codex
// CLI's request needs to be answered at all (store false, tool_choice
// auto, its sealed reasoning asked back) stays uni-switch's.
func TestCodexAccountTakesExtraBody(t *testing.T) {
	codexSignedIn(t)
	d := daybreak(t)
	os.MkdirAll(filepath.Dir(provider.Path()), 0o755)
	os.WriteFile(provider.Path(), mustJSON(map[string]any{"providers": []any{map[string]any{
		"id": "codex",
		"extraBody": map[string]any{
			"access_programs": map[string]any{"cyber": "__daybreak_forwarding_probe__"},
			"!store":          true,
			"!tool_choice":    "none",
			"!include":        []string{},
			"client_metadata": map[string]any{"session": "${sessionId}"},
		}}}}), 0o600)
	provider.ForgetAccounts()

	// the reporter's: a benign request, the probe from extraBody alone
	code, out := post(t, "/v1/responses", `{"model":"codex/gpt-daybreak-blue-latest","input":"Respond with OK.","stream":false}`)
	if code != 400 || gjson.Get(out, "error.code").String() != "invalid_access_program" {
		t.Fatalf("extraBody's probe: %d %s\nthe backend got %s", code, out, d.last(t))
	}
	if ap := gjson.GetBytes(d.last(t), "access_programs.cyber").String(); ap != "__daybreak_forwarding_probe__" {
		t.Fatalf("extraBody's access_programs didn't reach the backend: %s", d.last(t))
	}
	got := d.last(t)
	if gjson.GetBytes(got, "store").Bool() || gjson.GetBytes(got, "tool_choice").String() != "auto" ||
		!strings.Contains(gjson.GetBytes(got, "include").Raw, "reasoning.encrypted_content") {
		t.Errorf("extraBody set what Codex's request must carry: %s", got)
	}
	if !gjson.GetBytes(got, "client_metadata.session").Exists() || strings.Contains(string(got), "${sessionId}") {
		t.Errorf("client_metadata: %s", got)
	}

	// the client's own value is kept over extraBody's unforced one, on
	// Chat and on Anthropic's Messages too
	for _, c := range []struct{ name, path, body string }{
		{"responses", "/v1/responses", `{"model":"codex/gpt-daybreak-blue-latest","input":"hi","stream":true,"access_programs":{"cyber":"daybreak_blue"}}`},
		{"chat", "/v1/chat/completions", `{"model":"codex/gpt-daybreak-blue-latest","messages":[{"role":"user","content":"hi"}],"access_programs":{"cyber":"daybreak_blue"}}`},
	} {
		if code, out := post(t, c.path, c.body); code != 200 {
			t.Fatalf("%s: %d %s\nthe backend got %s", c.name, code, out, d.last(t))
		}
	}
	code, out = post(t, "/v1/messages", `{"model":"codex/gpt-daybreak-blue-latest","max_tokens":64,"messages":[{"role":"user","content":"hi"}]}`)
	// Anthropic's error shape has no code: the backend's words are said
	if code != 400 || !strings.Contains(out, "Invalid access program for access_programs.cyber") ||
		gjson.GetBytes(d.last(t), "access_programs.cyber").String() != "__daybreak_forwarding_probe__" {
		t.Errorf("messages: %d %s\nthe backend got %s", code, out, d.last(t))
	}

	// a program the workspace hasn't got, which the backend refuses inside
	// its stream, reaches a Messages client as the request's refusal,
	// whole or streamed: asked once, nobody resting
	os.WriteFile(provider.Path(), mustJSON(map[string]any{"providers": []any{map[string]any{
		"id": "codex", "extraBody": map[string]any{"access_programs": map[string]any{"cyber": "daybreak_red"}}}}}), 0o600)
	provider.ForgetAccounts()
	for _, stream := range []bool{false, true} {
		before := d.asked()
		code, out := post(t, "/v1/messages", `{"model":"codex/gpt-daybreak-blue-latest","max_tokens":64,"stream":`+map[bool]string{false: "false", true: "true"}[stream]+`,"messages":[{"role":"user","content":"hi"}]}`)
		if !strings.Contains(out, "invalid_request_error") || !strings.Contains(out, "not authorized for this workspace") || !stream && code != 400 {
			t.Errorf("not enabled, messages (stream %v): %d %s", stream, code, out)
		}
		if n := d.asked() - before; n != 1 {
			t.Errorf("not enabled, messages (stream %v): the backend was asked %d times, want once", stream, n)
		}
		if r := restingNow(t); len(r) > 0 {
			t.Errorf("not enabled, messages (stream %v): resting after the request's own refusal: %v", stream, r)
		}
	}
}

// json.RawMessage keeps a null the client sent as "null" (the guide's
// refused value), which Request.AccessPrograms carries on.
func TestAccessProgramsNullKept(t *testing.T) {
	r, err := parseResponses([]byte(`{"model":"m","input":"hi","access_programs":null}`))
	if err != nil {
		t.Fatal(err)
	}
	if string(r.AccessPrograms) != "null" {
		t.Fatalf("access_programs %q", r.AccessPrograms)
	}
	var m map[string]json.RawMessage
	json.Unmarshal(buildResponses(r, "m", "chatgpt.com", false), &m)
	if string(m["access_programs"]) != "null" {
		t.Fatalf("built %s", m["access_programs"])
	}
}

// TestCodexAccountLoadsInPlace (#4738, infinitex233): Claude Code's tool
// search through a ChatGPT account sends the tool it loaded to the backend
// in an additional_tools item where the search loaded it, so the tools
// and every earlier item stay as the request before sent them; set
// additional-tools=off, the tool goes in the tools as before.
func TestCodexAccountLoadsInPlace(t *testing.T) {
	codexSignedIn(t)
	d := daybreak(t)
	body := func(i int) string {
		return strings.Replace(tsRequests[i], `"model":"m1"`, `"model":"codex/gpt-daybreak-blue-latest"`, 1)
	}
	ask := func(i int) gjson.Result {
		t.Helper()
		if code, out := post(t, "/v1/messages", body(i)); code != 200 {
			t.Fatalf("request %d: %d %s", i+1, code, out)
		}
		return gjson.ParseBytes(d.last(t))
	}
	first, second := ask(0), ask(1)
	if got := tsNames(second.Get("tools")); strings.Join(got, ",") != strings.Join(tsNames(first.Get("tools")), ",") {
		t.Fatalf("the loaded tool changed the tools: %v, before %v", got, tsNames(first.Get("tools")))
	}
	var item gjson.Result
	second.Get("input").ForEach(func(_, it gjson.Result) bool {
		if it.Get("type").String() == "additional_tools" {
			item = it
		}
		return true
	})
	if strings.Join(tsNames(item.Get("tools")), ",") != "mcp__probe__read_a" {
		t.Fatalf("no additional_tools item with read_a: %s", second.Get("input").Raw)
	}

	os.MkdirAll(filepath.Dir(provider.Path()), 0o755)
	os.WriteFile(provider.Path(), mustJSON(map[string]any{"providers": []any{map[string]any{"id": "codex", "additionalToolsOff": true}}}), 0o600)
	provider.ForgetAccounts()
	off := ask(1)
	if strings.Contains(off.Get("input").Raw, "additional_tools") || !slices.Contains(tsNames(off.Get("tools")), "mcp__probe__read_a") {
		t.Fatalf("off, the tool goes in the tools: %s", off.Raw)
	}
}

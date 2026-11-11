package gateway

import (
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/tidwall/gjson"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// Claude Code's tool search as maximia's probe recorded it (#2660, 2.1.302):
// the first request offers ToolSearch and the placeholder that keeps
// deferred loading on; once ToolSearch answers with a tool_reference, the
// next request offers the loaded MCP tool too, defer_loading true, and the
// one after that loads a second tool the same way.
const tsTools0 = `{"name":"Bash","description":"Run a command","input_schema":{"type":"object","properties":{"command":{"type":"string"}},"required":["command"]}},
	{"name":"ToolSearch","description":"Fetches full schema definitions for deferred tools","input_schema":{"type":"object","properties":{"query":{"type":"string"},"max_results":{"type":"number"}},"required":["query","max_results"]}}`

const tsPlaceholder = `{"name":"DeferredToolPlaceholder","description":"","input_schema":{"type":"object"},"defer_loading":true}`

const tsReadA = `{"name":"mcp__probe__read_a","description":"Read A","input_schema":{"type":"object","properties":{"path":{"type":"string"}}},"defer_loading":true}`
const tsReadB = `{"name":"mcp__probe__read_b","description":"Read B","input_schema":{"type":"object","properties":{"path":{"type":"string"}}},"defer_loading":true}`

const tsSystem = `[{"type":"text","text":"You are Claude Code."}]`

const tsTurn1 = `{"role":"user","content":[{"type":"text","text":"load read_a with ToolSearch, call it, then load read_b and call it"}]}`
const tsSearchA = `{"role":"assistant","content":[{"type":"tool_use","id":"toolu_01","name":"ToolSearch","input":{"query":"select:mcp__probe__read_a","max_results":1}}]},
	{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_01","content":[{"type":"tool_reference","tool_name":"mcp__probe__read_a"}]}]}`
const tsCallA = `{"role":"assistant","content":[{"type":"tool_use","id":"toolu_02","name":"mcp__probe__read_a","input":{"path":"a"}}]},
	{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_02","content":"A"}]}`
const tsSearchB = `{"role":"assistant","content":[{"type":"tool_use","id":"toolu_03","name":"ToolSearch","input":{"query":"select:mcp__probe__read_b","max_results":1}}]},
	{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_03","content":[{"type":"tool_reference","tool_name":"mcp__probe__read_b"}]}]}`

func tsRequest(tools []string, msgs ...string) string {
	return fmt.Sprintf(`{"model":"m1","max_tokens":1000,"stream":true,"system":%s,"tools":[%s],"messages":[%s]}`,
		tsSystem, strings.Join(tools, ","), strings.Join(msgs, ","))
}

// the three requests of maximia's probe
var tsRequests = []string{
	tsRequest([]string{tsTools0, tsPlaceholder}, tsTurn1),
	tsRequest([]string{tsTools0, tsReadA, tsPlaceholder}, tsTurn1, tsSearchA),
	tsRequest([]string{tsTools0, tsReadA, tsReadB, tsPlaceholder}, tsTurn1, tsSearchA, tsCallA, tsSearchB),
}

func tsBuild(t *testing.T, body string, inPlace bool) gjson.Result {
	t.Helper()
	r, err := parseAnthropic([]byte(body))
	if err != nil {
		t.Fatal(err)
	}
	r.LoadedInPlace = inPlace
	return gjson.ParseBytes(buildResponses(r, "m1", "api.openai.com", false))
}

func tsNames(v gjson.Result) []string {
	var out []string
	v.ForEach(func(_, t gjson.Result) bool {
		out = append(out, t.Get("name").String())
		return true
	})
	return out
}

// TestLoadedToolsKeepThePrefix: a tool Claude Code's tool search loads goes
// to a Responses upstream that takes additional_tools in such an item after
// the result that loaded it, so the top-level tools, the instructions and
// every earlier input item stay as they were sent (#2660).
func TestLoadedToolsKeepThePrefix(t *testing.T) {
	var outs []gjson.Result
	for _, body := range tsRequests {
		outs = append(outs, tsBuild(t, body, true))
	}
	first := outs[0]
	for i, o := range outs {
		if got, want := o.Get("tools").Raw, first.Get("tools").Raw; got != want {
			t.Fatalf("request %d's tools changed:\n%s\nwant\n%s", i+1, got, want)
		}
		if o.Get("instructions").Raw != first.Get("instructions").Raw {
			t.Fatalf("request %d's instructions changed", i+1)
		}
		if i == 0 {
			continue
		}
		prev, cur := outs[i-1].Get("input").Array(), o.Get("input").Array()
		if len(cur) <= len(prev) {
			t.Fatalf("request %d's input is no longer than the one before", i+1)
		}
		for j := range prev {
			if prev[j].Raw != cur[j].Raw {
				t.Fatalf("request %d's input item %d changed:\n%s\nwas\n%s", i+1, j, cur[j].Raw, prev[j].Raw)
			}
		}
	}
	if got := tsNames(first.Get("tools")); strings.Join(got, ",") != "Bash,ToolSearch" {
		t.Fatalf("tools = %v", got)
	}

	// read_a right after the result that loaded it, read_b after its own
	in := outs[2].Get("input").Array()
	var kinds []string
	for _, it := range in {
		k := it.Get("type").String()
		if k == "additional_tools" {
			k += ":" + strings.Join(tsNames(it.Get("tools")), "+")
			if it.Get("role").String() != "developer" {
				t.Fatalf("item without the developer role: %s", it.Raw)
			}
		}
		kinds = append(kinds, k)
	}
	want := "message,function_call,function_call_output,additional_tools:mcp__probe__read_a,function_call,function_call_output,function_call,function_call_output,additional_tools:mcp__probe__read_b"
	if strings.Join(kinds, ",") != want {
		t.Fatalf("input = %s\nwant %s", strings.Join(kinds, ","), want)
	}
	loaded := in[3].Get("tools.0")
	if loaded.Get("type").String() != "function" || loaded.Get("parameters.properties.path.type").String() != "string" || loaded.Get("description").String() != "Read A" {
		t.Fatalf("loaded tool = %s", loaded.Raw)
	}
	// what the client is told of the load is as before
	if got := in[2].Get("output").String(); !strings.Contains(got, "mcp__probe__read_a is loaded") {
		t.Fatalf("output = %q", got)
	}
}

// TestLoadedToolsInOneItem: tools loaded by results answered together, by
// one result naming several, or named again by a later search go in one
// item after the last of those results, once each.
func TestLoadedToolsInOneItem(t *testing.T) {
	body := tsRequest([]string{tsTools0, tsReadA, tsReadB, tsPlaceholder}, tsTurn1,
		`{"role":"assistant","content":[
			{"type":"tool_use","id":"toolu_01","name":"ToolSearch","input":{"query":"select:mcp__probe__read_a,mcp__probe__read_b","max_results":5}},
			{"type":"tool_use","id":"toolu_02","name":"ToolSearch","input":{"query":"read b","max_results":5}}]}`,
		`{"role":"user","content":[
			{"type":"tool_result","tool_use_id":"toolu_01","content":[{"type":"tool_reference","tool_name":"mcp__probe__read_a"},{"type":"tool_reference","tool_name":"mcp__probe__read_b"}]},
			{"type":"tool_result","tool_use_id":"toolu_02","content":[{"type":"tool_reference","tool_name":"mcp__probe__read_b"}]}]}`,
		`{"role":"assistant","content":[{"type":"tool_use","id":"toolu_03","name":"ToolSearch","input":{"query":"select:mcp__probe__read_a","max_results":1}}]}`,
		`{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_03","content":[{"type":"tool_reference","tool_name":"mcp__probe__read_a"}]},{"type":"text","text":"go on"}]}`)
	o := tsBuild(t, body, true)
	var kinds []string
	for _, it := range o.Get("input").Array() {
		k := it.Get("type").String()
		if k == "additional_tools" {
			k += ":" + strings.Join(tsNames(it.Get("tools")), "+")
		}
		kinds = append(kinds, k)
	}
	want := "message,function_call,function_call,function_call_output,function_call_output,additional_tools:mcp__probe__read_a+mcp__probe__read_b,function_call,function_call_output,message"
	if strings.Join(kinds, ",") != want {
		t.Fatalf("input = %s\nwant %s", strings.Join(kinds, ","), want)
	}
	if got := tsNames(o.Get("tools")); strings.Join(got, ",") != "Bash,ToolSearch" {
		t.Fatalf("tools = %v", got)
	}
}

// TestLoadedToolsWhereNotTaken: where the item isn't taken the request is
// built as before #2660: every tool top-level, no additional_tools. So is a
// deferred tool no result in the history loaded (a compacted history), and
// a tool a reference names that wasn't deferred.
func TestLoadedToolsWhereNotTaken(t *testing.T) {
	for i, body := range tsRequests {
		o := tsBuild(t, body, false)
		if strings.Contains(o.Raw, "additional_tools") {
			t.Fatalf("request %d: %s", i+1, o.Raw)
		}
		want := []string{"Bash", "ToolSearch", "mcp__probe__read_a", "mcp__probe__read_b"}[:2+i]
		if got := tsNames(o.Get("tools")); !reflect.DeepEqual(got, want) {
			t.Fatalf("request %d's tools = %v, want %v", i+1, got, want)
		}
	}
	compacted := tsRequest([]string{tsTools0, tsReadA, tsPlaceholder}, `{"role":"user","content":"summary of what was done"}`)
	o := tsBuild(t, compacted, true)
	if strings.Contains(o.Raw, "additional_tools") || strings.Join(tsNames(o.Get("tools")), ",") != "Bash,ToolSearch,mcp__probe__read_a" {
		t.Fatalf("compacted: %s", o.Raw)
	}
	notDeferred := tsRequest([]string{tsTools0, `{"name":"mcp__probe__read_a","input_schema":{"type":"object"}}`}, tsTurn1, tsSearchA)
	o = tsBuild(t, notDeferred, true)
	if strings.Contains(o.Raw, "additional_tools") || len(o.Get("tools").Array()) != 3 {
		t.Fatalf("not deferred: %s", o.Raw)
	}
}

// TestLoadsInPlaceScope: OpenAI's own API and a ChatGPT (Codex) account
// are sent the item (#4738), and a provider the user said takes it; xAI, a
// relay, another vendor's sign-in, Azure and a plugin aren't, nor one the
// user turned it off for.
func TestLoadsInPlaceScope(t *testing.T) {
	for _, c := range []struct {
		p    provider.Provider
		want bool
	}{
		{provider.Provider{ID: "openai", Responses: "https://api.openai.com/v1"}, true},
		{provider.Provider{ID: "openai-eu", Responses: "https://eu.api.openai.com/v1"}, true},
		{provider.Provider{ID: "lookalike", Responses: "https://api.openai.com.example.net/v1"}, false},
		{provider.Provider{ID: "xai", Responses: "https://api.x.ai/v1"}, false},
		{provider.Provider{ID: "relay", Responses: "https://relay.example.com/v1"}, false},
		{provider.Provider{ID: "relay", Responses: "https://relay.example.com/v1", AdditionalTools: true}, true},
		{provider.Provider{ID: "codex", Responses: "https://chatgpt.com/backend-api/codex", Account: &provider.Account{Agent: "codex"}}, true},
		{provider.Provider{ID: "codex", Responses: "https://chatgpt.com/backend-api/codex", Account: &provider.Account{Agent: "codex"}, AdditionalToolsOff: true}, false},
		{provider.Provider{ID: "openai", Responses: "https://api.openai.com/v1", AdditionalToolsOff: true}, false},
		{provider.Provider{ID: "copilot", Responses: "https://api.githubcopilot.com", Account: &provider.Account{Agent: "copilot"}}, false},
		{provider.Provider{ID: "copilot", Responses: "https://api.githubcopilot.com", Account: &provider.Account{Agent: "copilot"}, AdditionalTools: true}, true},
		{provider.Provider{ID: "az", Responses: "https://x.openai.azure.com/openai/v1"}, false},
	} {
		if got := loadsInPlace(c.p); got != c.want {
			t.Errorf("%s %s: %v, want %v", c.p.ID, c.p.Responses, got, c.want)
		}
	}
}

// TestLoadedToolsThroughTheGateway: a provider set to take the item gets
// it from /v1/messages; one that isn't gets the request as before; one
// that turns the item away is asked again with every tool top-level and
// isn't sent it again.
func TestLoadedToolsThroughTheGateway(t *testing.T) {
	f := &fake{t: t, reply: sse(
		`data: {"type":"response.created","response":{"id":"r1","model":"m1"}}`,
		`data: {"type":"response.completed","response":{"id":"r1","output":[],"usage":{"input_tokens":5,"output_tokens":2}}}`)}
	setup(t, provider.Responses, f)
	ask := func() gjson.Result {
		t.Helper()
		if code, out := post(t, "/v1/messages", tsRequests[1]); code != 200 {
			t.Fatalf("status %d: %s", code, out)
		}
		return gjson.ParseBytes(f.got)
	}
	if got := ask(); strings.Contains(got.Raw, "additional_tools") || len(got.Get("tools").Array()) != 3 {
		t.Fatalf("not set, the request is as before: %s", got.Raw)
	}

	p, err := provider.Find("fake")
	if err != nil {
		t.Fatal(err)
	}
	p.AdditionalTools = true
	if err := provider.Save(*p); err != nil {
		t.Fatal(err)
	}
	got := ask()
	if !strings.Contains(got.Raw, `"additional_tools"`) || strings.Join(tsNames(got.Get("tools")), ",") != "Bash,ToolSearch" {
		t.Fatalf("set, the loaded tool goes where it was loaded: %s", got.Raw)
	}

	f.calls = 0
	f.refuse = func(b []byte) (int, string) {
		if strings.Contains(string(b), `"additional_tools"`) {
			return http.StatusBadRequest, `{"error":{"message":"Invalid value: 'additional_tools'. Supported values are: 'message', 'function_call'.","type":"invalid_request_error","param":"input[3].type"}}`
		}
		return 0, ""
	}
	srv := New()
	for i := range 2 {
		if code, out := postTo(t, srv, "/v1/messages", tsRequests[1]); code != 200 {
			t.Fatalf("try %d: status %d: %s", i, code, out)
		}
		got := gjson.ParseBytes(f.got)
		if strings.Contains(got.Raw, "additional_tools") || len(got.Get("tools").Array()) != 3 {
			t.Fatalf("refused, asked again as before: %s", got.Raw)
		}
	}
	if f.calls != 3 {
		t.Fatalf("calls = %d, want the refused one, its retry, and the next without the item", f.calls)
	}
}

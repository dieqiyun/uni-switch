package gateway

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/usage"
)

// rootsHub is a hub with uni-switch's image server (a Roots tool) and another,
// each call's caller kept.
func rootsHub(t *testing.T) (*fakeHub, chan HubCaller) {
	t.Helper()
	got := make(chan HubCaller, 8)
	f := &fakeHub{on: true, tools: map[string][]HubTool{"claude": {
		{Server: "uni-switch-image", Tool: "generate_image", Spec: hubSpec("generate_image"), Roots: true},
		{Server: "fs", Tool: "read", Spec: hubSpec("read")},
	}}}
	f.call = func(ctx context.Context, server, tool string) (json.RawMessage, error) {
		got <- HubCallerOf(ctx)
		return json.RawMessage(`{"content":[{"type":"text","text":"ran ` + server + `"}]}`), nil
	}
	withHub(t, f)
	return f, got
}

// hubEvents reads a stream's data lines, one message each.
func hubEvents(t *testing.T, res *http.Response) <-chan mcpMessage {
	t.Helper()
	out := make(chan mcpMessage, 8)
	go func() {
		defer close(out)
		sc := bufio.NewScanner(res.Body)
		for sc.Scan() {
			if d, ok := strings.CutPrefix(sc.Text(), "data: "); ok {
				var m mcpMessage
				json.Unmarshal([]byte(d), &m)
				out <- m
			}
		}
	}()
	return out
}

func nextEvent(t *testing.T, ev <-chan mcpMessage, what string) mcpMessage {
	t.Helper()
	select {
	case m, ok := <-ev:
		if !ok {
			t.Fatalf("the stream ended before %s", what)
		}
		return m
	case <-time.After(10 * time.Second):
		t.Fatalf("no %s in 10s", what)
	}
	return mcpMessage{}
}

func hubPostTo(t *testing.T, url, body, accept, ua string) *http.Response {
	t.Helper()
	r, _ := http.NewRequest(http.MethodPost, url, strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Accept", accept)
	r.Header.Set("User-Agent", ua)
	res, err := http.DefaultClient.Do(r)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { res.Body.Close() })
	return res
}

const bothAccept = "application/json, text/event-stream"

// uni-switch-image behind the hub saves into the calling agent's project: a
// call of it asks the agent roots/list on the call's own event stream
// (Claude Code answers so), and the image server is called with the
// file roots the agent answered and the agent's name. A tool that
// doesn't write into the project is answered as before, in JSON, and
// asks nothing.
func TestHubAsksTheAgentItsRoots(t *testing.T) {
	fresh(t)
	_, got := rootsHub(t)
	srv := httptest.NewServer(localGuard(New().Handler()))
	t.Cleanup(srv.Close)
	url := srv.URL + HubMCPPath + "claude"

	res := hubPostTo(t, url, toolsCall("uni-switch-image__generate_image", `{"prompt":"a uni-switch"}`), bothAccept, "claude-code/2.1.314 (sdk-cli)")
	if res.StatusCode != 200 || !strings.HasPrefix(res.Header.Get("Content-Type"), "text/event-stream") {
		t.Fatalf("%d %s", res.StatusCode, res.Header.Get("Content-Type"))
	}
	ev := hubEvents(t, res)
	ask := nextEvent(t, ev, "roots/list")
	if ask.Method != "roots/list" || len(ask.ID) == 0 {
		t.Fatalf("asked %+v, want roots/list", ask)
	}
	ans := hubPostTo(t, url, `{"jsonrpc":"2.0","id":`+string(ask.ID)+`,"result":{"roots":[{"uri":"file:///work/proj","name":"proj"},{"uri":"https://example.com/x"}]}}`, bothAccept, "claude-code/2.1.314 (sdk-cli)")
	if ans.StatusCode != http.StatusAccepted {
		t.Fatalf("the answer: %d", ans.StatusCode)
	}
	done := nextEvent(t, ev, "the call's result")
	if string(done.ID) != "3" || !strings.Contains(string(done.Result), "ran uni-switch-image") {
		t.Fatalf("result %s %s", done.ID, done.Result)
	}
	c := <-got
	if strings.Join(c.Roots, " ") != "file:///work/proj" || c.Client != "claude-code/2.1.314" {
		t.Fatalf("called for %+v, want the file root and claude-code/2.1.314", c)
	}

	// another server: JSON, as before, with no caller
	res = hubPostTo(t, url, toolsCall("fs__read", `{"q":"x"}`), bothAccept, "claude-code/2.1.314 (sdk-cli)")
	if !strings.HasPrefix(res.Header.Get("Content-Type"), "application/json") {
		t.Fatalf("fs__read answered as %s", res.Header.Get("Content-Type"))
	}
	if c := <-got; len(c.Roots) != 0 || c.Client != "" {
		t.Fatalf("fs__read called for %+v", c)
	}
}

// An agent that takes no event stream back isn't asked, and one that
// doesn't answer holds the call no longer than hubRootsWait: the image
// server is called with no roots (it saves into ~/Pictures/uni-switch then).
func TestHubRootsUnasked(t *testing.T) {
	fresh(t)
	_, got := rootsHub(t)
	old := hubRootsWait
	hubRootsWait = 200 * time.Millisecond
	t.Cleanup(func() { hubRootsWait = old })
	srv := httptest.NewServer(localGuard(New().Handler()))
	t.Cleanup(srv.Close)
	url := srv.URL + HubMCPPath + "claude"
	call := toolsCall("uni-switch-image__generate_image", `{}`)

	res := hubPostTo(t, url, call, "application/json", "some-client/1")
	if !strings.HasPrefix(res.Header.Get("Content-Type"), "application/json") {
		t.Fatalf("JSON only: answered as %s", res.Header.Get("Content-Type"))
	}
	if c := <-got; len(c.Roots) != 0 || c.Client != "some-client/1" {
		t.Fatalf("JSON only: called for %+v", c)
	}

	start := time.Now()
	ev := hubEvents(t, hubPostTo(t, url, call, bothAccept, "some-client/1"))
	if m := nextEvent(t, ev, "roots/list"); m.Method != "roots/list" {
		t.Fatalf("asked %+v", m)
	}
	if m := nextEvent(t, ev, "the call's result"); !strings.Contains(string(m.Result), "ran uni-switch-image") {
		t.Fatalf("result %s", m.Result)
	}
	if c := <-got; len(c.Roots) != 0 {
		t.Fatalf("unanswered: called for %+v", c)
	}
	if d := time.Since(start); d < hubRootsWait {
		t.Errorf("the call didn't wait for the answer: %s", d)
	}
}

// Codex declares no roots and answers roots/list with none, but names
// its thread in each call's _meta, and in a git repository the repository
// too: the image server is given that folder, else the one the thread
// runs in. A call from another computer is given no folder
// of this one, whatever it names.
func TestHubRootsOfACodexThread(t *testing.T) {
	fresh(t)
	codex := t.TempDir()
	t.Setenv("CODEX_HOME", codex)
	const id = "019a1111-2222-7333-8444-555566667777"
	day := filepath.Join(codex, "sessions", "2026", "10", "30")
	os.MkdirAll(day, 0o755)
	os.WriteFile(filepath.Join(day, "rollout-2026-10-30T09-00-00-"+id+".jsonl"),
		[]byte(`{"timestamp":"2026-10-30T09:00:00Z","type":"session_meta","payload":{"id":"`+id+`","cwd":"/work/proj"}}`+"\n"), 0o644)
	f, got := rootsHub(t)
	f.tools["codex"] = f.tools["claude"]
	srv := httptest.NewServer(localGuard(New().Handler()))
	t.Cleanup(srv.Close)
	call := `{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"uni-switch-image__generate_image","arguments":{},"_meta":{"threadId":"` + id + `"}}}`

	ev := hubEvents(t, hubPostTo(t, srv.URL+HubMCPPath+"codex", call, bothAccept, "codex-mcp-client/0.165.0"))
	ask := nextEvent(t, ev, "roots/list")
	hubPostTo(t, srv.URL+HubMCPPath+"codex", `{"jsonrpc":"2.0","id":`+string(ask.ID)+`,"result":{"roots":[]}}`, bothAccept, "codex-mcp-client/0.165.0")
	nextEvent(t, ev, "the call's result")
	if c := <-got; strings.Join(c.Roots, " ") != "file:///work/proj" || c.Client != "codex-mcp-client/0.165.0" {
		t.Fatalf("called for %+v, want the thread's folder", c)
	}

	// in a git repository Codex names it in the turn's metadata, which is
	// read before any rollout (a thread this computer has none of here)
	inRepo := `{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"uni-switch-image__generate_image","arguments":{},"_meta":{"threadId":"019a0000-0000-7000-8000-000000000000","x-codex-turn-metadata":{"workspaces":{"/work/repo":{"has_changes":false},"relative":{}}}}}}`
	ev = hubEvents(t, hubPostTo(t, srv.URL+HubMCPPath+"codex", inRepo, bothAccept, "codex-mcp-client/0.165.0"))
	ask = nextEvent(t, ev, "roots/list")
	hubPostTo(t, srv.URL+HubMCPPath+"codex", `{"jsonrpc":"2.0","id":`+string(ask.ID)+`,"result":{"roots":[]}}`, bothAccept, "codex-mcp-client/0.165.0")
	nextEvent(t, ev, "the call's result")
	if c := <-got; strings.Join(c.Roots, " ") != "file:///work/repo" {
		t.Fatalf("called for %+v, want the turn's workspace", c)
	}

	_, secrets := newCaller(t, "Laptop")
	h := localGuard(New().Handler())
	m := readMCP(t, hubPost(h, "codex", "192.0.2.7:4000", call, "Authorization", "Bearer "+secrets[0], "Accept", "application/json"))
	if m.Result.IsError {
		t.Fatalf("remote call: %+v", m)
	}
	if c := <-got; len(c.Roots) != 0 {
		t.Fatalf("a call from another computer was given %v", c.Roots)
	}
}

func TestFileURI(t *testing.T) {
	for in, want := range map[string]string{"/work/my proj": "file:///work/my%20proj", `C:\Users\me\p`: "file:///C:/Users/me/p"} {
		if filepath.Separator == '/' && strings.Contains(in, `\`) {
			continue // a Windows path is one name elsewhere
		}
		if got := fileURI(in); got != want {
			t.Errorf("fileURI(%q) = %q, want %q", in, got, want)
		}
	}
}

// uni-switch's image server draws through the gateway on a connection of its
// own, so the hub's row of its call learns the model from the result's
// _meta, as the server's own report gave it before the hub. Another
// server's _meta names nothing: only uni-switch's own tools say a model.
func TestHubRowNamesTheImageModel(t *testing.T) {
	fresh(t)
	f, _ := rootsHub(t)
	f.call = func(ctx context.Context, server, tool string) (json.RawMessage, error) {
		return json.RawMessage(`{"content":[{"type":"text","text":"ran ` + server + `"}],"_meta":{"` + HubModelMeta + `":"art/painter-` + server + `"}}`), nil
	}
	srv := httptest.NewServer(localGuard(New().Handler()))
	t.Cleanup(srv.Close)
	url := srv.URL + HubMCPPath + "claude"
	hubPostTo(t, url, toolsCall("uni-switch-image__generate_image", `{}`), "application/json", "claude-code/2.1.314 (sdk-cli)")
	hubPostTo(t, url, toolsCall("fs__read", `{}`), "application/json", "claude-code/2.1.314 (sdk-cli)")
	var rows []usage.Record
	for end := time.Now().Add(10 * time.Second); time.Now().Before(end) && len(rows) < 2; time.Sleep(5 * time.Millisecond) {
		rows = mcpRows(t)
	}
	got := map[string]string{}
	for _, r := range rows {
		got[r.Requested] = r.Provider + "/" + r.Model
	}
	if got["uni-switch-image__generate_image"] != "art/painter-uni-switch-image" || got["fs__read"] != "/" {
		t.Fatalf("rows name %v", got)
	}
}

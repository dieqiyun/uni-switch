package library

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/agent"
)

// The fake server uni-switch runs for Codex: this test binary again, with
// UNI_SWITCH_FAKE_RUN naming a log it writes a line to for each start (its
// pid and the client named in the initialize it was given).
func TestFakeRunServer(t *testing.T) {
	logf := os.Getenv("UNI_SWITCH_FAKE_RUN")
	if logf == "" {
		t.Skip("run by the run tests")
	}
	out := bufio.NewWriter(os.Stdout)
	say := func(v any) {
		b, _ := json.Marshal(v)
		out.Write(append(b, '\n'))
		out.Flush()
	}
	sc := bufio.NewScanner(os.Stdin)
	var asked json.RawMessage // the tools/call waiting for the client's roots
	for sc.Scan() {
		var m struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
			Params struct {
				Name       string `json:"name"`
				ClientInfo struct {
					Name string `json:"name"`
				} `json:"clientInfo"`
			} `json:"params"`
			Result json.RawMessage `json:"result"`
		}
		json.Unmarshal(sc.Bytes(), &m)
		switch {
		case m.Method == "initialize":
			l, _ := os.OpenFile(logf, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
			fmt.Fprintf(l, "%d %s\n", os.Getpid(), m.Params.ClientInfo.Name)
			l.Close()
			say(map[string]any{"jsonrpc": "2.0", "id": m.ID, "result": map[string]any{"protocolVersion": "2025-06-18", "capabilities": map[string]any{"tools": map[string]any{"listChanged": true}}, "serverInfo": map[string]any{"name": "fake", "version": "1"}}})
		case m.Method == "tools/list":
			say(map[string]any{"jsonrpc": "2.0", "id": m.ID, "result": map[string]any{"tools": []any{map[string]any{"name": "echo"}, map[string]any{"name": fmt.Sprint("pid", os.Getpid())}}}})
		case m.Method == "tools/call" && m.Params.Name == "ask":
			asked = m.ID
			say(map[string]any{"jsonrpc": "2.0", "id": 77, "method": "roots/list"})
		case m.Method == "tools/call" && m.Params.Name == "change":
			say(map[string]any{"jsonrpc": "2.0", "method": "notifications/tools/list_changed"})
			say(map[string]any{"jsonrpc": "2.0", "id": m.ID, "result": map[string]any{"content": []any{}}})
		case m.Method == "tools/call":
			say(map[string]any{"jsonrpc": "2.0", "id": m.ID, "result": map[string]any{"content": []any{map[string]any{"type": "text", "text": fmt.Sprint(os.Getpid())}}}})
		case m.Method == "" && string(m.ID) == "77":
			say(map[string]any{"jsonrpc": "2.0", "id": asked, "result": map[string]any{"content": []any{map[string]any{"type": "text", "text": "roots " + string(m.Result)}}}})
		}
	}
	os.Exit(0)
}

// runFixture is the fake server given as "fake", served as the gateway
// serves it, and the log of its starts.
func runFixture(t *testing.T) (*httptest.Server, string, runSpec) {
	t.Helper()
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	logf := filepath.Join(t.TempDir(), "starts")
	spec := runSpec{Command: self, Args: []string{"-test.run=^TestFakeRunServer$"}, Env: map[string]string{"UNI_SWITCH_FAKE_RUN": logf}}
	resetRuns(t)
	runGiven(map[string]runSpec{"fake": spec})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ServeRun(w, r, strings.TrimPrefix(r.URL.Path, RunPath))
	}))
	t.Cleanup(srv.Close)
	return srv, logf, spec
}

// resetRuns ends every session's server, before the test and after it.
func resetRuns(t *testing.T) {
	end := func() {
		runs.Lock()
		all := runs.sessions
		runs.sessions, runs.given, runs.said, runs.wired = nil, nil, nil, nil
		runs.Unlock()
		for _, s := range all {
			s.mu.Lock()
			c := s.child
			s.mu.Unlock()
			s.stop()
			if c != nil {
				<-c.done
			}
		}
	}
	end()
	t.Cleanup(end)
}

func starts(t *testing.T, logf string) []string {
	t.Helper()
	b, _ := os.ReadFile(logf)
	return strings.Fields(strings.ReplaceAll(string(b), "\n", " | "))
}

func startCount(logf string) int {
	b, _ := os.ReadFile(logf)
	return strings.Count(string(b), "\n")
}

// call posts one message as Codex does, and is the reply's body and the
// session id it was given.
func call(t *testing.T, srv *httptest.Server, sid, body string) (string, string) {
	t.Helper()
	req, _ := http.NewRequest("POST", srv.URL+RunPath+"fake", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	if sid != "" {
		req.Header.Set("Mcp-Session-Id", sid)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 && resp.StatusCode != 202 {
		t.Fatalf("%s: %d %s", body, resp.StatusCode, b)
	}
	got := string(b)
	// an event stream's last message is the reply
	if strings.HasPrefix(resp.Header.Get("Content-Type"), "text/event-stream") {
		var last string
		for _, l := range strings.Split(got, "\n") {
			if d, ok := strings.CutPrefix(l, "data: "); ok {
				last = d
			}
		}
		got = last
	}
	return got, resp.Header.Get("Mcp-Session-Id")
}

const runInit = `{"jsonrpc":"2.0","id":0,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"%s","version":"1"}}}`

// A second thread's session is initialized and listed from what the first
// one's server said, with no server of its own; its server starts on its
// first call, initialized as that thread itself asked.
func TestRunStartsOnCall(t *testing.T) {
	srv, logf, _ := runFixture(t)
	r, a := call(t, srv, "", fmt.Sprintf(runInit, "thread-a"))
	if a == "" || !strings.Contains(r, `"serverInfo"`) || !strings.Contains(r, `"id":0`) {
		t.Fatalf("initialize: %s (session %q)", r, a)
	}
	call(t, srv, a, `{"jsonrpc":"2.0","method":"notifications/initialized"}`)
	tools, _ := call(t, srv, a, `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)
	if !strings.Contains(tools, `"echo"`) || !strings.Contains(tools, `"id":1`) {
		t.Fatalf("tools/list: %s", tools)
	}
	if n := startCount(logf); n != 1 {
		t.Fatalf("%d starts for the first session: %v", n, starts(t, logf))
	}
	for i := range 5 {
		r, b := call(t, srv, "", fmt.Sprintf(runInit, fmt.Sprint("idle-", i)))
		if b == "" || b == a || !strings.Contains(r, `"serverInfo"`) {
			t.Fatalf("initialize %d: %s (session %q)", i, r, b)
		}
		call(t, srv, b, `{"jsonrpc":"2.0","method":"notifications/initialized"}`)
		if got, _ := call(t, srv, b, `{"jsonrpc":"2.0","id":"x","method":"tools/list"}`); !strings.Contains(got, `"id":"x"`) || !strings.Contains(got, `"echo"`) {
			t.Fatalf("tools/list %d: %s", i, got)
		}
		if got, _ := call(t, srv, b, `{"jsonrpc":"2.0","id":2,"method":"ping"}`); !strings.Contains(got, `"result":{}`) {
			t.Fatalf("ping: %s", got)
		}
	}
	if n := startCount(logf); n != 1 {
		t.Fatalf("idle sessions started servers: %v", starts(t, logf))
	}
	r, b := call(t, srv, "", fmt.Sprintf(runInit, "thread-b"))
	call(t, srv, b, `{"jsonrpc":"2.0","method":"notifications/initialized"}`)
	got, _ := call(t, srv, b, `{"jsonrpc":"2.0","id":9,"method":"tools/call","params":{"name":"echo","arguments":{}}}`)
	if !strings.Contains(got, `"id":9`) || !strings.Contains(got, `"text"`) {
		t.Fatalf("tools/call: %s (init %s)", got, r)
	}
	if s := starts(t, logf); startCount(logf) != 2 || !strings.Contains(strings.Join(s, " "), "thread-b") {
		t.Fatalf("starts: %v", s)
	}
	// the first session keeps its own server
	got2, _ := call(t, srv, a, `{"jsonrpc":"2.0","id":10,"method":"tools/call","params":{"name":"echo","arguments":{}}}`)
	if startCount(logf) != 2 || got2 == got {
		t.Fatalf("session a: %s, b: %s, starts %v", got2, got, starts(t, logf))
	}
}

// A session's server ends once it has been idle runIdle, and the next call
// starts it again, initialized as the session was.
func TestRunIdleEnds(t *testing.T) {
	srv, logf, _ := runFixture(t)
	_, a := call(t, srv, "", fmt.Sprintf(runInit, "thread-a"))
	first, _ := call(t, srv, a, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"echo"}}`)
	runs.Lock()
	s := runs.sessions[a]
	runs.Unlock()
	c := s.running()
	if c == nil {
		t.Fatal("no server after a call")
	}
	runSweep(time.Now().Add(runIdle / 2))
	if s.running() == nil {
		t.Fatal("ended before it was idle")
	}
	runSweep(time.Now().Add(runIdle + time.Minute))
	select {
	case <-c.done:
	case <-time.After(30 * time.Second):
		t.Fatal("an idle server wasn't ended")
	}
	again, _ := call(t, srv, a, `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"echo"}}`)
	if !strings.Contains(again, `"id":2`) || again == first {
		t.Fatalf("after idle: %s (was %s)", again, first)
	}
	if got := starts(t, logf); startCount(logf) != 2 || strings.Count(strings.Join(got, " "), "thread-a") != 2 {
		t.Fatalf("starts: %v", got)
	}
	// DELETE ends the session and its server
	c = s.running()
	req, _ := http.NewRequest("DELETE", srv.URL+RunPath+"fake", nil)
	req.Header.Set("Mcp-Session-Id", a)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	select {
	case <-c.done:
	case <-time.After(30 * time.Second):
		t.Fatal("DELETE left the server running")
	}
}

// What the server asks the client mid-call comes on the call's stream, and
// the client's answer goes to the server.
func TestRunServerAsks(t *testing.T) {
	srv, _, _ := runFixture(t)
	_, a := call(t, srv, "", fmt.Sprintf(runInit, "thread-a"))
	req, _ := http.NewRequest("POST", srv.URL+RunPath+"fake", strings.NewReader(`{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"ask"}}`))
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set("Mcp-Session-Id", a)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	rd := bufio.NewReader(resp.Body)
	next := func() string {
		for {
			l, err := rd.ReadString('\n')
			if err != nil {
				t.Fatalf("stream: %v", err)
			}
			if d, ok := strings.CutPrefix(strings.TrimSpace(l), "data: "); ok {
				return d
			}
		}
	}
	if q := next(); !strings.Contains(q, `"roots/list"`) || !strings.Contains(q, `"id":77`) {
		t.Fatalf("asked: %s", q)
	}
	call(t, srv, a, `{"jsonrpc":"2.0","id":77,"result":{"roots":[]}}`)
	if r := next(); !strings.Contains(r, `"id":5`) || !strings.Contains(r, `roots {\"roots\":[]}`) {
		t.Fatalf("reply: %s", r)
	}
}

// A server saying its tools changed has the next session's tools/list go
// to a server again.
func TestRunListChanged(t *testing.T) {
	srv, _, _ := runFixture(t)
	_, a := call(t, srv, "", fmt.Sprintf(runInit, "a"))
	before, _ := call(t, srv, a, `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)
	_, b := call(t, srv, "", fmt.Sprintf(runInit, "b"))
	call(t, srv, b, `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"change"}}`)
	// the notification is read before the reply after it
	after, _ := call(t, srv, b, `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)
	if before == after {
		t.Fatalf("tools/list answered from before the change: %s", after)
	}
}

// A server given no longer, or with another command, has its sessions'
// servers ended; a new session of one gone is refused.
func TestRunGivenChanged(t *testing.T) {
	srv, logf, spec := runFixture(t)
	_, a := call(t, srv, "", fmt.Sprintf(runInit, "a"))
	call(t, srv, a, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"echo"}}`)
	runs.Lock()
	s := runs.sessions[a]
	runs.Unlock()
	c := s.running()
	runGiven(map[string]runSpec{"fake": spec}) // the same: kept
	if s.running() != c {
		t.Fatal("a sync giving the same server ended it")
	}
	changed := spec
	changed.Args = append([]string{"-test.v"}, spec.Args...)
	runGiven(map[string]runSpec{"fake": changed})
	<-c.done
	call(t, srv, a, `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"echo"}}`)
	if startCount(logf) != 2 {
		t.Fatalf("starts after the change: %v", starts(t, logf))
	}
	c = s.running()
	runGiven(map[string]runSpec{})
	<-c.done
	req, _ := http.NewRequest("POST", srv.URL+RunPath+"fake", strings.NewReader(fmt.Sprintf(runInit, "c")))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("a server given no longer: %d", resp.StatusCode)
	}
	req, _ = http.NewRequest("POST", srv.URL+RunPath+"fake", strings.NewReader(`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"echo"}}`))
	req.Header.Set("Mcp-Session-Id", a)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound || s.running() != nil {
		t.Fatalf("a session of a server given no longer: %d", resp.StatusCode)
	}
}

// A session uni-switch didn't see start (Codex kept it over a restart of
// uni-switch's) is taken up as it is.
func TestRunUnknownSession(t *testing.T) {
	srv, logf, _ := runFixture(t)
	got, sid := call(t, srv, "old-session", `{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"echo"}}`)
	if sid != "old-session" || !strings.Contains(got, `"id":4`) || strings.Contains(got, `"error"`) {
		t.Fatalf("%s (session %q)", got, sid)
	}
	if startCount(logf) != 1 {
		t.Fatalf("starts: %v", starts(t, logf))
	}
}

// Codex on uni-switch is given a command's server as the gateway's, and a
// sync after reads it back as uni-switch's own; off uni-switch it gets the
// command again.
func TestRunCodexEntry(t *testing.T) {
	h := sandbox(t)
	resetRuns(t)
	runOn = true
	wired := true
	was := runWired
	runWired = func(a *agent.Agent) bool { return wired }
	t.Cleanup(func() { runOn, runWired = false, was })
	if _, err := SaveServer("", Server{Name: "mem", Transport: "stdio", Command: "node", Args: []string{"server.js"}, Agents: []string{"codex", "claude"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := SaveServer("", Server{Name: "keyed", Transport: "stdio", Command: "node", Env: map[string]string{"UNI_SWITCH_TEST_UNSET_VAR": "${UNI_SWITCH_TEST_UNSET_VAR}"}, Agents: []string{"codex"}}); err != nil {
		t.Fatal(err)
	}
	codex := read(t, filepath.Join(h, ".codex/config.toml"))
	if !strings.Contains(codex, RunPath+"mem") || strings.Contains(codex, `"server.js"`) {
		t.Fatalf("codex on uni-switch:\n%s", codex)
	}
	if !strings.Contains(codex, `UNI_SWITCH_TEST_UNSET_VAR`) || strings.Contains(codex, RunPath+"keyed") {
		t.Fatalf("a server with a variable uni-switch hasn't:\n%s", codex)
	}
	if claude := read(t, filepath.Join(h, ".claude.json")); !strings.Contains(claude, `"server.js"`) || strings.Contains(claude, RunPath) {
		t.Fatalf("claude:\n%s", claude)
	}
	runs.Lock()
	_, given := runs.given["mem"]
	runs.Unlock()
	if !given {
		t.Fatal("the sync didn't record the server it gave")
	}
	if r, err := Sync(); err != nil || len(r.Changed) != 0 || len(r.Problems) != 0 {
		t.Fatalf("a second sync: %+v %v", r, err)
	}
	if v, err := Read(nil); err != nil {
		t.Fatal(err)
	} else {
		for _, f := range v.FoundServers {
			t.Errorf("found as the user's: %+v", f)
		}
	}
	wired = false
	if _, err := Sync(); err != nil {
		t.Fatal(err)
	}
	codex = read(t, filepath.Join(h, ".codex/config.toml"))
	if strings.Contains(codex, RunPath) || !strings.Contains(codex, `"server.js"`) {
		t.Fatalf("codex off uni-switch:\n%s", codex)
	}
	runs.Lock()
	_, given = runs.given["mem"]
	runs.Unlock()
	if given {
		t.Fatal("a server given by its command again is still served")
	}
}

// A server the user keeps direct (#4382, cuipengcx90: cocoindex reads its
// project from the folder it starts in) is given to Codex on uni-switch as its
// command, as to every other agent, with the hub on or off: Codex starts it
// in the thread's folder, where uni-switch's /mcp/run would start it from home.
// No cwd is written, which is what makes Codex use the thread's.
func TestRunDirectServerIsCodexsCommand(t *testing.T) {
	h := sandbox(t)
	t.Cleanup(hubStopAll)
	resetRuns(t)
	runOn = true
	was := runWired
	runWired = func(a *agent.Agent) bool { return true }
	t.Cleanup(func() { runOn, runWired = false, was })
	if _, err := SaveServer("", Server{Name: "cocoindex", Transport: "stdio", Command: "ccc", Args: []string{"mcp"}, Env: map[string]string{"K": "v"}, Agents: []string{"codex", "claude"}, Hub: HubDirect}); err != nil {
		t.Fatal(err)
	}
	if _, err := SaveServer("", Server{Name: "mem", Transport: "stdio", Command: "node", Args: []string{"server.js"}, Agents: []string{"codex"}}); err != nil {
		t.Fatal(err)
	}
	check := func(when string) {
		t.Helper()
		e, err := targetByID("codex").MCP.entries()
		if err != nil {
			t.Fatal(err)
		}
		codex := read(t, filepath.Join(h, ".codex/config.toml"))
		c := e["cocoindex"]
		if c["command"] != "ccc" || c["url"] != nil || c["cwd"] != nil || fmt.Sprint(c["args"]) != "[mcp]" || fmt.Sprint(c["env"]) != "map[K:v]" {
			t.Fatalf("%s: codex's direct entry %v\n%s", when, c, codex)
		}
		runs.Lock()
		_, given := runs.given["cocoindex"]
		runs.Unlock()
		if given {
			t.Fatalf("%s: a direct server is served at %scocoindex", when, RunPath)
		}
		// before a sync has recorded what it gave, too
		runs.Lock()
		given2 := runs.given
		runs.given = nil
		runs.Unlock()
		_, before := runSpecOf("cocoindex")
		_, shared := runSpecOf("mem")
		runs.Lock()
		runs.given = given2
		runs.Unlock()
		if before || !shared {
			t.Fatalf("%s: before a sync /mcp/run would start the direct server %v, the shared one %v", when, before, shared)
		}
	}
	check("hub off")
	if codex := read(t, filepath.Join(h, ".codex/config.toml")); !strings.Contains(codex, RunPath+"mem") {
		t.Fatalf("a shared server isn't run by uni-switch any more:\n%s", codex)
	}
	if _, err := SetMCPHub(true); err != nil {
		t.Fatal(err)
	}
	check("hub on")
	// shared again: uni-switch runs it for Codex, as before
	if _, err := ServerHub("cocoindex", false); err != nil {
		t.Fatal(err)
	}
	if _, err := SetMCPHub(false); err != nil {
		t.Fatal(err)
	}
	if codex := read(t, filepath.Join(h, ".codex/config.toml")); !strings.Contains(codex, RunPath+"cocoindex") {
		t.Fatalf("cocoindex shared again:\n%s", codex)
	}
}

package gateway

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// turnsFollowed has turns followed for a test: a turn as long as the
// length returned (set it to change), a short quiet, and each turn's end
// sent on the channel returned. The test waits out a timer still going
// before the hooks are put back.
func turnsFollowed(t *testing.T, after time.Duration) (chan TurnEnd, *atomic.Int64) {
	t.Helper()
	ends := make(chan TurnEnd, 8)
	length := new(atomic.Int64)
	length.Store(int64(after))
	oldDone, oldAfter, oldQuiet := TurnDone, turnAfter, turnQuiet
	TurnDone = func(e TurnEnd) { ends <- e }
	turnAfter = func() time.Duration { return time.Duration(length.Load()) }
	turnQuiet = 50 * time.Millisecond
	t.Cleanup(func() {
		time.Sleep(4 * turnQuiet)
		TurnDone, turnAfter, turnQuiet = oldDone, oldAfter, oldQuiet
	})
	return ends, length
}

// noEnd fails when a turn's end is said within a while: four times the
// quiet, room enough for a timer late on a busy machine.
func noEnd(t *testing.T, ends chan TurnEnd, why string) {
	t.Helper()
	select {
	case e := <-ends:
		t.Fatalf("%s: a turn's end was said: %+v", why, e)
	case <-time.After(4 * turnQuiet):
	}
}

func gotEnd(t *testing.T, ends chan TurnEnd) TurnEnd {
	t.Helper()
	select {
	case e := <-ends:
		return e
	case <-time.After(10 * time.Second):
		t.Fatal("the turn's end was never said")
	}
	return TurnEnd{}
}

// An agent's turn is a run of requests, each reply but the last asking it
// to run a tool. Its end is said once the reply that called none has
// ended and nothing more came: not after a reply that called a tool, not
// for a subagent's last reply while its parent waits, and not for a turn
// shorter than the length set. Through the gateway, Chat to Chat and
// Anthropic to Chat, as an agent sends them.
func TestTurnDoneSaidAtTheTurnsEnd(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	var tool atomic.Bool // whether the next reply calls a tool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		if tool.Load() {
			io.WriteString(w, `data: {"id":"c1","choices":[{"index":0,"delta":{"role":"assistant","tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"Bash","arguments":"{\"command\":\"ls\"}"}}]}}]}`+"\n\n")
			io.WriteString(w, `data: {"id":"c1","choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}`+"\n\n")
		} else {
			// a relay's delta that names no tool call: "tool_calls":[] is a key, not a call
			io.WriteString(w, `data: {"id":"c2","choices":[{"index":0,"delta":{"role":"assistant","content":"done: \"tool_use\"","tool_calls":[]}}]}`+"\n\n")
			io.WriteString(w, `data: {"id":"c2","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`+"\n\n")
		}
		io.WriteString(w, "data: [DONE]\n\n")
	}))
	defer srv.Close()
	if err := provider.Save(provider.Provider{ID: "plan", Name: "Plan", Chat: srv.URL + "/v1", Models: []string{"m1"}, Key: "k"}); err != nil {
		t.Fatal(err)
	}
	s := New()
	send := func(path, body, session, sub string, calls bool) {
		tool.Store(calls)
		req := httptest.NewRequest("POST", path, strings.NewReader(body))
		req.Header.Set(SessionHeader, session)
		if sub != "" {
			req.Header.Set("x-claude-code-agent-id", sub)
		}
		rec := httptest.NewRecorder()
		s.Handler().ServeHTTP(rec, req)
		if rec.Code != 200 {
			t.Fatalf("%s: %d %s", path, rec.Code, rec.Body)
		}
	}
	chat := `{"model":"plan/m1","stream":true,"messages":[{"role":"user","content":"hi"}]}`
	messages := `{"model":"plan/m1","stream":true,"max_tokens":100,"system":"You are Claude Code.\n - Primary working directory: /Users/u/work/uni-switch\n","messages":[{"role":"user","content":"hi"}]}`

	for _, c := range []struct{ name, path, body, folder string }{
		{"chat", "/v1/chat/completions", chat, ""},
		{"anthropic", "/v1/messages", messages, "/Users/u/work/uni-switch"},
	} {
		t.Run(c.name, func(t *testing.T) {
			ends, length := turnsFollowed(t, time.Nanosecond)
			session := "s-" + c.name
			// the turn runs from its first request: the clock starts
			// before it, so a slow first request on a busy machine is in
			// both (#1629)
			start := time.Now()
			send(c.path, c.body, session, "", true)
			noEnd(t, ends, "after a reply that called a tool")
			// a subagent's last reply, its parent still waiting on it
			send(c.path, c.body, session, "agent-1", false)
			noEnd(t, ends, "after a subagent's last reply")
			send(c.path, c.body, session, "", false)
			e := gotEnd(t, ends)
			if e.Session != session || e.Agent == "" || e.Folder != c.folder {
				t.Fatalf("end %+v", e)
			}
			if e.Took < 8*turnQuiet || e.Took > time.Since(start) {
				t.Fatalf("took %v: the turn ran from its first request to its last reply", e.Took)
			}
			noEnd(t, ends, "said once")

			// a turn shorter than the length set isn't said
			length.Store(int64(time.Hour))
			send(c.path, c.body, session, "", false)
			noEnd(t, ends, "a short turn")
		})
	}
}

// Off (TurnDone unset, or no length) follows nothing: nothing is kept.
func TestTurnDoneOff(t *testing.T) {
	ends, _ := turnsFollowed(t, 0)
	var ts turns
	r := &Route{turn: true, Agent: "claude", Session: "s"}
	ts.begin(r, time.Now())
	ts.end(r, true, time.Now())
	if len(ts.m) != 0 {
		t.Fatalf("followed while off: %+v", ts.m)
	}
	noEnd(t, ends, "off")
}

// A request still going holds the turn open, whatever another's reply
// was; a session idle past turnStale starts its next turn over.
func TestTurnDoneWaitsForEveryRequest(t *testing.T) {
	ends, _ := turnsFollowed(t, time.Minute)
	var ts turns
	t0 := time.Now().Add(-10 * time.Minute)
	a, b := &Route{turn: true, Agent: "codex", Session: "s"}, &Route{turn: true, Agent: "codex", Session: "s"}
	ts.begin(a, t0)
	ts.begin(b, t0.Add(time.Second))
	ts.end(a, true, t0.Add(2*time.Minute))
	noEnd(t, ends, "one still going")
	ts.end(b, true, t0.Add(3*time.Minute))
	if e := gotEnd(t, ends); e.Took != 3*time.Minute || e.Agent != "codex" {
		t.Fatalf("end %+v", e)
	}

	// a turn whose last reply was read as a tool call, then the user away
	c := &Route{turn: true, Agent: "codex", Session: "s"}
	ts.begin(c, t0)
	ts.end(c, false, t0.Add(time.Second))
	d := &Route{turn: true, Agent: "codex", Session: "s"}
	ts.begin(d, t0.Add(time.Second+turnStale+time.Minute))
	ts.end(d, true, t0.Add(time.Second+turnStale+time.Minute+30*time.Second))
	noEnd(t, ends, "the stale turn's start isn't the new one's")
}

// Concurrent tasks of an agent that sends no session id are told apart by
// the conversation a request belongs to (#2001): the short one's end reads
// its own length, not the long one's, and is said when it ends — not held
// for the long one still going.
func TestTurnDoneConcurrentTasksByTheirConversation(t *testing.T) {
	ends, _ := turnsFollowed(t, time.Minute)
	var ts turns
	long := &Route{turn: true, Agent: "codex", Conv: "uni-switch-long"}
	short := &Route{turn: true, Agent: "codex", Conv: "uni-switch-short"}
	t0 := time.Now().Add(-time.Hour)
	ts.begin(long, t0)
	// the short task runs while the long one is going, and ends first
	ts.begin(short, t0.Add(30*time.Minute))
	ts.end(short, true, t0.Add(30*time.Minute+time.Minute))
	if e := gotEnd(t, ends); e.Took != time.Minute {
		t.Fatalf("the short task's end took %v, want its own minute", e.Took)
	}
	// the long one still going: no second end yet, its own when it ends
	noEnd(t, ends, "the long task still going")
	ts.end(long, true, t0.Add(time.Hour))
	if e := gotEnd(t, ends); e.Took != time.Hour {
		t.Fatalf("the long task's end took %v, want its own hour", e.Took)
	}
}

// Tasks of the same session still share one turn, as before (#2001): a
// session's end waits for every request of it, and its span is the turn's.
func TestTurnDoneSameSessionStillOneTurn(t *testing.T) {
	ends, _ := turnsFollowed(t, time.Minute)
	var ts turns
	t0 := time.Now().Add(-time.Hour)
	a := &Route{turn: true, Agent: "codex", Session: "s"}
	b := &Route{turn: true, Agent: "codex", Session: "s"}
	ts.begin(a, t0)
	ts.begin(b, t0.Add(time.Minute))
	ts.end(a, true, t0.Add(2*time.Minute))
	noEnd(t, ends, "b still going in the same session")
	ts.end(b, true, t0.Add(3*time.Minute))
	if e := gotEnd(t, ends); e.Took != 3*time.Minute {
		t.Fatalf("took %v, want the session's whole turn", e.Took)
	}
}

// A reply's tool calls are seen in every wire format, split anywhere, and
// never in text that only mentions one.
func TestToolScan(t *testing.T) {
	for _, c := range []struct {
		name, body string
		want       bool
	}{
		{"anthropic block", `event: content_block_start` + "\n" + `data: {"type":"content_block_start","index":1,"content_block":{"type":"tool_use","id":"toolu_1","name":"Bash","input":{}}}`, true},
		{"anthropic stop", `{"type":"message_delta","delta":{"stop_reason": "tool_use"}}`, true},
		{"anthropic end_turn", `{"type":"message_delta","delta":{"stop_reason":"end_turn"}}`, false},
		{"anthropic server tool", `{"type":"content_block_start","content_block":{"type":"server_tool_use","name":"web_search"}}`, false},
		{"chat", `{"choices":[{"delta":{},"finish_reason":"tool_calls"}]}`, true},
		{"chat legacy", `{"choices":[{"finish_reason":"function_call"}]}`, true},
		{"chat empty calls", `{"choices":[{"delta":{"content":"hi","tool_calls":[]},"finish_reason":"stop"}]}`, false},
		{"responses", `{"type":"response.output_item.added","item":{"type":"function_call","name":"shell"}}`, true},
		{"responses custom", `{"type":"response.output_item.done","item":{"type":"custom_tool_call","name":"apply_patch"}}`, true},
		{"responses shell", `{"type":"response.output_item.added","item":{"type":"local_shell_call"}}`, true},
		{"responses search", `{"type":"response.output_item.added","item":{"type":"web_search_call"}}`, false},
		{"responses text", `{"type":"response.completed","response":{"status":"completed","output":[{"type":"message"}],"tools":[{"type":"function","name":"shell"}]}}`, false},
		{"gemini", `{"candidates":[{"content":{"parts":[{"functionCall" : {"name":"ls","args":{}}}]}}]}`, true},
		{"escaped mention", `{"delta":{"text":"set \"type\":\"tool_use\" and \"functionCall\": here"}}`, false},
	} {
		for _, at := range []int{0, 1, 17, len(c.body) / 2, len(c.body) - 3} {
			var s toolScan
			at = min(max(at, 0), len(c.body))
			s.add([]byte(c.body[:at]))
			s.add([]byte(c.body[at:]))
			if s.called() != c.want {
				t.Errorf("%s split at %d: called %v", c.name, at, s.called())
			}
		}
	}
	var none *toolScan
	none.add([]byte(`"type":"tool_use"`))
	if none.called() {
		t.Fatal("a nil scan saw a call")
	}
}

// Codex signed in with ChatGPT goes to its backend apart from routing:
// its turn's end is said there too, a function_call item read as a tool
// call though the response still says "completed".
func TestTurnDoneCodexBackend(t *testing.T) {
	setup(t, provider.Chat, &fake{t: t})
	var tool atomic.Bool
	chatgpt(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header()["Content-Type"] = nil // as the ChatGPT backend sends it
		item := `{"type":"message","role":"assistant","content":[{"type":"output_text","text":"done"}]}`
		if tool.Load() {
			item = `{"type":"function_call","name":"shell","call_id":"c1","arguments":"{}"}`
		}
		io.WriteString(w, sse(
			`data: {"type":"response.created","response":{"id":"r1"}}`,
			`data: {"type":"response.output_item.done","item":`+item+`}`,
			`data: {"type":"response.completed","response":{"id":"r1","status":"completed","usage":{"input_tokens":9,"output_tokens":2}}}`))
	})
	ends, _ := turnsFollowed(t, time.Nanosecond)
	s := New()
	post := func(calls bool) {
		tool.Store(calls)
		rec := httptest.NewRecorder()
		req := httptest.NewRequest("POST", CodexPath+"/responses", strings.NewReader(`{"model":"gpt-5.5","stream":true,"input":[
		  {"type":"message","role":"user","content":[{"type":"input_text","text":"<environment_context>\n  <cwd>/home/u/proj</cwd>\n</environment_context>"}]}]}`))
		req.Header.Set("Authorization", "Bearer chatgpt-token")
		req.Header.Set("chatgpt-account-id", "acct-1")
		req.Header.Set("session_id", "codex-thread")
		s.Handler().ServeHTTP(rec, req)
		if rec.Code != 200 {
			t.Fatalf("%d %s", rec.Code, rec.Body)
		}
	}
	post(true)
	noEnd(t, ends, "after a function_call")
	post(false)
	if e := gotEnd(t, ends); e.Session != "codex-thread" || e.Folder != "/home/u/proj" {
		t.Fatalf("end %+v", e)
	}
}

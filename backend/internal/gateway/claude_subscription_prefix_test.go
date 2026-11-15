package gateway

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"

	"path/filepath"
	"regexp"

	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/claudetui"

	"github.com/dieqiyun/uni-switch/backend/internal/testenv"
)

// prefixHarness is a gateway whose Claude subscription runs a stand-in
// for Claude Code that keeps its session as Claude Code does: a line for
// each user message it is told and each reply, with its uuid and the
// session's id. It logs its arguments, the tools its MCP helper offers
// (<pid>.tools) and each line it is told, and answers "pid P had H turn
// T": H the lines its session had as it started (a branch's copy, or a
// resumed session), T the turns it was told. A turn that says CALLTOOL
// calls the client's read tool first and says what the call was answered.
// With a file "late" in its log folder, it tells each reply's message
// with its uuid a while after the reply's stream ended, as the
// interactive mode reads it from the session it wrote.
type prefixHarness struct {
	t      *testing.T
	s      *Server
	srv    *httptest.Server
	logs   string
	config string
}

// prefixTools is the client's tools, as the harness's requests offer them.
const prefixTools = `[{"name":"read","description":"Read a file","input_schema":{"type":"object","properties":{"path":{"type":"string"}}}},{"name":"bash","description":"Run a command","input_schema":{"type":"object","properties":{"command":{"type":"string"}}}}]`

var prefixAnswer = regexp.MustCompile(`pid (\d+) had (\d+) turn (\d+)( call [a-z]+)?`)

// ask posts messages with extra fields (`"tools":…,` and the like) and
// gives the answer, "pid P had H turn T", and the whole body.
func (h *prefixHarness) ask(extra, messages string) (string, string) {
	h.t.Helper()
	res, err := http.Post(h.srv.URL+"/v1/messages", "application/json", strings.NewReader(
		`{"model":"group/orchestrator","max_tokens":1000,"stream":true,"system":"be brief",`+extra+`"messages":`+messages+`}`))
	if err != nil {
		h.t.Fatal(err)
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	if res.StatusCode != 200 {
		h.t.Fatalf("%d %.600s", res.StatusCode, b)
	}
	return prefixAnswer.FindString(string(b)), string(b)
}

// log is what the run of pid was started with and told.
func (h *prefixHarness) log(pid string) string {
	b, _ := os.ReadFile(filepath.Join(h.logs, pid))
	return string(b)
}

// pids are the runs started so far.
func (h *prefixHarness) pids() []string {
	entries, _ := os.ReadDir(h.logs)
	var pids []string
	for _, e := range entries {
		if !strings.Contains(e.Name(), ".") {
			pids = append(pids, e.Name())
		}
	}
	return pids
}

// tools is the tools the run of pid's MCP helper offered its agent.
func (h *prefixHarness) tools(pid string) []bridgeTool {
	h.t.Helper()
	b, err := os.ReadFile(filepath.Join(h.logs, pid+".tools"))
	if err != nil {
		h.t.Fatal(err)
	}
	var tools []bridgeTool
	if err := json.Unmarshal(b, &tools); err != nil {
		h.t.Fatalf("%v: %s", err, b)
	}
	return tools
}

func answerPID(t *testing.T, answer string) string {
	t.Helper()
	m := prefixAnswer.FindStringSubmatch(answer)
	if m == nil {
		t.Fatalf("answer %q", answer)
	}
	return m[1]
}

// A branch's session is the run's lines as far as the one it is cut
// after, each its own session's; a line still being written is not there
// yet, and one never written is an error, not a branch of the whole.
func TestClaudeCutSessionCopiesAsFarAsTheLine(t *testing.T) {
	was := cutWait
	cutWait = 50 * time.Millisecond
	t.Cleanup(func() { cutWait = was })
	dirs := []string{t.TempDir(), t.TempDir()}
	lines := `{"type":"user","uuid":"a","sessionId":"S"}` + "\n" +
		`{"type":"assistant","parentUuid":"b","uuid":"b","sessionId":"S","message":{"content":"\"uuid\":\"c\""}}` + "\n" +
		`{"type":"user","parentUuid":"b","uuid":"c","sessionId":"S"}` + "\n" +
		`{"type":"assistant","uuid":"d","sessionId":"S"`
	if err := os.WriteFile(filepath.Join(dirs[1], "S.jsonl"), []byte(lines), 0o600); err != nil {
		t.Fatal(err)
	}
	id, err := cutSession(dirs, "S", "b")
	if err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(filepath.Join(dirs[1], id+".jsonl"))
	want := strings.ReplaceAll(strings.Join(strings.SplitAfter(lines, "\n")[:2], ""), `"sessionId":"S"`, `"sessionId":"`+id+`"`)
	if id == "S" || string(got) != want {
		t.Fatalf("copy %s:\n%s\nwant\n%s", id, got, want)
	}
	if src, _ := os.ReadFile(filepath.Join(dirs[1], "S.jsonl")); string(src) != lines {
		t.Fatalf("the run's session changed: %s", src)
	}
	for _, at := range []string{"d", "z"} {
		if id, err := cutSession(dirs, "S", at); err == nil {
			t.Errorf("cut at %s: %s, want an error", at, id)
		}
	}
}

// interactive makes the harness's runs Claude Code's interactive mode:
// its helper is a stand-in that runs the stand-in Claude Code as asked.
func (h *prefixHarness) interactive() {
	h.t.Helper()
	claudeTUIRuns = true
	h.t.Cleanup(func() { claudeTUIRuns = false })
	helper := filepath.Join(h.t.TempDir(), "uni-switch")
	testenv.Program(h.t, helper, `#!/bin/sh
[ "$1" = "`+claudetui.Command+`" ] || exit 9
shift; claude=$1; shift
exec "$claude" "$@"
`)
	claudeTUIHelper = helper
	h.t.Cleanup(func() { claudeTUIHelper = "" })
}

// A run that heard a line of an earlier turn last, its new reply's lines
// not yet in the session as the next turn comes: cutReply waits for the
// reply's lines, as it would for the line, before it cuts after the
// earlier line, which would leave the last turn out.
func TestClaudeCutReplyWaitsForTheReplysLines(t *testing.T) {
	was := cutWait
	cutWait = 5 * time.Second
	t.Cleanup(func() { cutWait = was })
	dir := t.TempDir()
	f := filepath.Join(dir, "S.jsonl")
	early := `{"type":"user","uuid":"u1","sessionId":"S","message":{"role":"user","content":"hi"}}` + "\n" +
		`{"type":"assistant","uuid":"a1","sessionId":"S","message":{"id":"msg_1","role":"assistant","content":[{"type":"text","text":"hello"}]}}` + "\n" +
		`{"type":"user","uuid":"u2","sessionId":"S","message":{"role":"user","content":"and now"}}` + "\n"
	late := `{"type":"assistant","uuid":"a2","sessionId":"S","message":{"id":"msg_2","role":"assistant","content":[{"type":"text","text":"done"}]}}` + "\n"
	if err := os.WriteFile(f, []byte(early), 0o600); err != nil {
		t.Fatal(err)
	}
	written := make(chan struct{})
	go func() {
		defer close(written)
		// the cut is looking by now: it found a1 and no line of msg_2
		time.Sleep(200 * time.Millisecond)
		os.WriteFile(f, []byte(early+late), 0o600)
	}()
	id, err := cutReply([]string{dir}, "S", "a1", "msg_2")
	<-written
	if err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(filepath.Join(dir, id+".jsonl"))
	if want := strings.ReplaceAll(early+late, `"sessionId":"S"`, `"sessionId":"`+id+`"`); string(got) != want {
		t.Errorf("copy\n%s\nwant\n%s", got, want)
	}
}

// cutReply keeps a reply whole: Claude Code writes a line a content
// block, each under the message's id, and the line the run heard last as
// the side question began may be the turn's tool call. The copy goes as
// far as the reply's last line, past the call's result, and leaves the
// prompt suggestion and its answer out; a sub-agent's line of the same
// id (isSidechain) is no line of the reply.
func TestClaudeCutReplyKeepsTheWholeReply(t *testing.T) {
	was := cutWait
	cutWait = 50 * time.Millisecond
	t.Cleanup(func() { cutWait = was })
	dir := t.TempDir()
	// the shape of a Claude Code 2.1.318 session, trimmed
	rows := []string{
		`{"parentUuid":null,"isSidechain":false,"userType":"external","cwd":"/w","sessionId":"S","version":"2.1.318","type":"user","message":{"role":"user","content":"read main.go"},"uuid":"u1","timestamp":"2026-11-03T10:00:00.000Z"}`,
		`{"parentUuid":"u1","isSidechain":false,"userType":"external","cwd":"/w","sessionId":"S","version":"2.1.318","message":{"id":"msg_01A","type":"message","role":"assistant","model":"claude-opus-5-5","content":[{"type":"tool_use","id":"toolu_01","name":"mcp__uni-switch__read","input":{"path":"main.go"}}],"stop_reason":null},"requestId":"req_1","type":"assistant","uuid":"a1","timestamp":"2026-11-03T10:00:02.000Z"}`,
		`{"parentUuid":"a1","isSidechain":false,"userType":"external","cwd":"/w","sessionId":"S","version":"2.1.318","type":"user","message":{"role":"user","content":[{"tool_use_id":"toolu_01","type":"tool_result","content":[{"type":"text","text":"package main"}]}]},"uuid":"r1","timestamp":"2026-11-03T10:00:05.000Z","toolUseResult":[{"type":"text","text":"package main"}]}`,
		`{"parentUuid":"r1","isSidechain":false,"userType":"external","cwd":"/w","sessionId":"S","version":"2.1.318","message":{"id":"msg_01B","type":"message","role":"assistant","model":"claude-opus-5-5","content":[{"type":"thinking","thinking":"short file","signature":"sig"}],"stop_reason":null},"requestId":"req_2","type":"assistant","uuid":"a2","timestamp":"2026-11-03T10:00:06.000Z"}`,
		`{"parentUuid":"a2","isSidechain":true,"userType":"external","cwd":"/w","sessionId":"S","version":"2.1.318","message":{"id":"msg_01B","type":"message","role":"assistant","model":"claude-opus-5-5","content":[{"type":"text","text":"a sub-agent's"}]},"type":"assistant","uuid":"x1","timestamp":"2026-11-03T10:00:06.500Z"}`,
		`{"parentUuid":"a2","isSidechain":false,"userType":"external","cwd":"/w","sessionId":"S","version":"2.1.318","message":{"id":"msg_01B","type":"message","role":"assistant","model":"claude-opus-5-5","content":[{"type":"text","text":"It is a main package."}],"stop_reason":"end_turn"},"requestId":"req_2","type":"assistant","uuid":"a3","timestamp":"2026-11-03T10:00:07.000Z"}`,
		`{"parentUuid":"a3","isSidechain":false,"userType":"external","cwd":"/w","sessionId":"S","version":"2.1.318","type":"system","subtype":"turn_duration","durationMs":7000,"uuid":"d1","timestamp":"2026-11-03T10:00:07.100Z"}`,
		`{"parentUuid":"d1","isSidechain":false,"userType":"external","cwd":"/w","sessionId":"S","version":"2.1.318","type":"user","message":{"role":"user","content":` + strconv.Quote(suggestionMode) + `},"uuid":"u2","timestamp":"2026-11-03T10:00:07.300Z"}`,
		`{"parentUuid":"u2","isSidechain":false,"userType":"external","cwd":"/w","sessionId":"S","version":"2.1.318","message":{"id":"msg_01C","type":"message","role":"assistant","model":"claude-opus-5-5","content":[{"type":"text","text":"run the tests"}],"stop_reason":"end_turn"},"requestId":"req_3","type":"assistant","uuid":"a4","timestamp":"2026-11-03T10:00:08.000Z"}`,
	}
	lines := strings.Join(rows, "\n") + "\n"
	if err := os.WriteFile(filepath.Join(dir, "S.jsonl"), []byte(lines), 0o600); err != nil {
		t.Fatal(err)
	}
	upTo := func(n int) string {
		return strings.ReplaceAll(strings.Join(rows[:n], "\n")+"\n", `"sessionId":"S"`, `"sessionId":"ID"`)
	}
	for _, c := range []struct {
		name, line, msg string
		want            int
	}{
		{"the call heard last", "a1", "msg_01B", 6},
		{"the reply's first block heard last", "a2", "msg_01B", 6},
		{"the reply's last line heard", "a3", "msg_01B", 6},
		{"no reply known", "a1", "", 2},
		{"the reply's lines not in the session", "a1", "msg_none", 2},
		{"no line heard, the reply's id only", "", "msg_01B", 6},
		{"no line heard, the call's reply", "", "msg_01A", 2},
	} {
		id, err := cutReply([]string{dir}, "S", c.line, c.msg)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		got, _ := os.ReadFile(filepath.Join(dir, id+".jsonl"))
		if want := strings.ReplaceAll(upTo(c.want), `"sessionId":"ID"`, `"sessionId":"`+id+`"`); string(got) != want {
			t.Errorf("%s: copy\n%s\nwant\n%s", c.name, got, want)
		}
	}
	if id, err := cutReply([]string{dir}, "S", "zz", "msg_none"); err == nil {
		t.Errorf("a cut at no line nor reply: %s, want an error", id)
	}
	if id, err := cutReply([]string{dir}, "S", "", "msg_none"); err == nil {
		t.Errorf("a cut at no line and a reply not in the session: %s, want an error", id)
	}
	if id, err := cutReply([]string{dir}, "S", "", ""); err == nil {
		t.Errorf("a cut at neither a line nor a reply: %s, want an error", id)
	}
}

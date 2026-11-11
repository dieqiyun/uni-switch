package gateway

import (
	"fmt"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// unloadable stands in for Claude Code given --resume on a session it
// can't load: it says so and exits 1, as Claude Code 2.1.295 does ("No
// conversation found with session ID"). It reads one line first, so the
// line uni-switch wrote is taken and uni-switch hears of the end only as the
// process goes: the order in which the turn was answered 502 "Claude Code
// ended", with no reason (FrierenF on Discord).
const unloadable = `if [ -n "$file" ] && [ "$sid" != "s$$" ] && [ ! -s "$file" ]; then
  read -r line
  echo "No conversation found with session ID: $sid" >&2
  exit 1
fi`

// safeguards is what Claude Code in auto mode sends with each turn; a
// run is told it with a control request before the turn (setSafeguards).
const safeguards = `"safeguards":[{"type":"dangerous_tool_use","classifier_context":"The user allows file edits in the project."}],`

// A conversation's saved session Claude Code can't go on from is let go,
// and a new Claude Code is told the whole conversation, with or without
// the safety context: the turn was answered 502 "Claude Code ended".
func TestClaudeUnloadableSessionStartsAnew(t *testing.T) {
	for _, extra := range []string{safeguards, ""} {
		t.Run(fmt.Sprintf("safeguards=%t", extra != ""), func(t *testing.T) {
			h := newScriptHarness(t, "ok", unloadable)
			h.extra = extra
			h.ask("rules A", "a1")
			var sidA string
			for pid := range h.runs() {
				sidA = "s" + pid
			}
			for i := range idleMost {
				h.ask(fmt.Sprintf("rules %d", i), "x")
			}
			eventually(t, "A's session on the shelf", func() bool {
				h.s.subscription.mu.Lock()
				defer h.s.subscription.mu.Unlock()
				return len(h.s.subscription.shelf) == 1
			})
			// the file is there (saved), but nothing Claude Code can load
			files, _ := filepath.Glob(filepath.Join(h.config, "projects", "*", sidA+".jsonl"))
			if len(files) != 1 {
				t.Fatalf("A's session file: %v", files)
			}
			if err := os.Truncate(files[0], 0); err != nil {
				t.Fatal(err)
			}

			before := h.runs()
			h.ask("rules A", "a1", "a2") // fails the test on anything but 200
			var tried, answered []string
			for pid, log := range h.runs() {
				if _, ok := before[pid]; ok {
					continue
				}
				if strings.Contains(log, "--resume "+sidA) {
					tried = append(tried, log)
				} else {
					answered = append(answered, log)
				}
			}
			if len(tried) != 1 || len(answered) != 1 {
				t.Fatalf("want one run resumed and one started anew:\nresumed %q\nanew %q", tried, answered)
			}
			if told := answered[0]; !strings.Contains(told, "a1") || !strings.Contains(told, "a2") {
				t.Fatalf("the new run was not told the whole conversation:\n%s", told)
			}
			eventually(t, "the session no Claude Code can load stayed", func() bool { return !containsStr(h.sessions(), sidA) })
		})
	}
}

// A Claude Code that ends before it answers uni-switch's control request
// says why: its exit status and the last it wrote to stderr, where the
// client was told "Claude Code ended" alone.
func TestClaudeEndedRunSaysWhy(t *testing.T) {
	h := newScriptHarness(t, "ok", `read -r line
echo "Error: the account's sign-in is gone; run claude auth login" >&2
exit 3`)
	body := `{"model":"claude-sonnet-5","max_tokens":100,` + safeguards + `"messages":[{"role":"user","content":"hi"}]}`
	rec := httptest.NewRecorder()
	var u Usage
	code, msg := h.s.serveClaudeSubscription(rec, httptest.NewRequest("POST", "/v1/messages", strings.NewReader(body)), provider.Anthropic, h.p, "claude-sonnet-5", []byte(body), &u)
	if code != 502 {
		t.Fatalf("%d %s", code, msg)
	}
	for _, want := range []string{"initialize", "exit status 3", "sign-in is gone; run claude auth login"} {
		if !strings.Contains(msg, want) || !strings.Contains(rec.Body.String(), want) {
			t.Errorf("the error doesn't say %q: %s\n%s", want, msg, rec.Body)
		}
	}
}

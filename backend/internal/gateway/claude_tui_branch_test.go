package gateway

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// A branch of a claude -p run's session, made before the interactive mode
// was turned on, goes to a new interactive Claude Code given the whole
// conversation (a transcript of its own, replaySession): -p's prompt isn't the interactive mode's, so the cache has
// nothing of it. It was refused with the interactive mode on.
func TestClaudeTUIBranchOfPrintRunIsToldWhole(t *testing.T) {
	h := tuiScriptHarness(t, "")
	h.ask("rules", "a0")
	// the run waits for the conversation's next turn: its session's
	// folders are where a -p run's would be
	var sessions []string
	h.s.subscription.mu.Lock()
	for _, run := range h.s.subscription.idle {
		run.mu.Lock()
		sessions = run.sessions
		run.mu.Unlock()
	}
	h.s.subscription.mu.Unlock()
	if len(sessions) == 0 {
		t.Fatal("no run waits with its session's folders")
	}
	if err := os.MkdirAll(sessions[0], 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sessions[0], "s-print.jsonl"), []byte(`{"type":"assistant","uuid":"u-print","sessionId":"s-print"}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	before := h.runs()
	msgs := []Message{
		{Role: "user", Parts: []Part{{Kind: Text, Text: "first words"}}},
		{Role: "assistant", Parts: []Part{{Kind: Text, Text: "ok"}}},
		{Role: "user", Parts: []Part{{Kind: Text, Text: "later words"}}},
	}
	from := &savedSession{session: "s-print", sessions: sessions, at: time.Now(), since: msgs[2:], cut: "u-print", gone: true}
	req := &Request{Messages: msgs, branch: from}
	run, _, err := h.s.subscription.start(context.Background(), req, "claude-sonnet-5", h.config, "", from)
	if err != nil {
		t.Fatal(err)
	}
	if !run.tui {
		t.Fatal("a branch of a -p run's session ran as -p")
	}
	var log string
	eventually(t, "the branch's run told its turn", func() bool {
		log = newRun(h, before)
		return strings.Contains(log, "later words")
	})
	// given the whole conversation: as a transcript of its own
	// (replaySession), never the -p run's session
	args, _, _ := strings.Cut(log, "\n")
	_, sid, resumed := strings.Cut(args, "--resume ")
	sid, _, _ = strings.Cut(sid, " ")
	whole := strings.Contains(log, "first words")
	if resumed {
		b, _ := os.ReadFile(filepath.Join(sessions[len(sessions)-1], sid+".jsonl"))
		whole = sid != "s-print" && strings.Contains(string(b), "first words") && !strings.Contains(log, "first words")
	}
	if !whole {
		t.Fatalf("the branch's run wasn't a new one given the whole conversation: %s", log)
	}
	if req.branch != nil {
		t.Fatal("the request still names the -p run's branch")
	}
}

// newRun is the log of the run h started since before.
func newRun(h *resumeHarness, before map[string]string) string {
	for name, log := range h.runs() {
		if _, ok := before[name]; !ok {
			return log
		}
	}
	return ""
}

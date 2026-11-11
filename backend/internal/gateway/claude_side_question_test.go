package gateway

import (
	"bytes"
	"encoding/json"
	"log"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// cc290 is a request as Claude Code 2.1.290 sends one, from its requests
// to a stand-in Anthropic: its billing line first in the system prompt,
// cch changing each request, the system prompt after it, adaptive
// thinking, a "system" message after the first user message and after
// each turn's, and each reply's thinking sent back with its signature.
func cc290(cch, msgs string) string {
	return `{"model":"claude-opus-5-5","max_tokens":64000,` +
		`"thinking":{"type":"adaptive","display":"updates"},"output_config":{"effort":"high"},` +
		`"context_management":{"edits":[{"type":"clear_thinking_20251015","keep":"all"}]},` +
		`"metadata":{"user_id":"{\"device_id\":\"d\",\"session_id\":\"cfd14856-1107-4d9e-956e-39ff90f1abc3\"}"},` +
		`"system":[{"type":"text","text":"x-anthropic-billing-header: cc_version=2.1.290.758; cc_entrypoint=cli; cch=` + cch + `;"},` +
		`{"type":"text","text":"You are Claude Code, Anthropic's official CLI for Claude.","cache_control":{"type":"ephemeral","ttl":"1h"}}],` +
		`"tools":[{"name":"Bash","description":"Run a command","input_schema":{"type":"object","properties":{"command":{"type":"string"}}}}],` +
		`"messages":[` + msgs + `],"stream":true}`
}

// cc290Reply is a reply as Claude Code sends it back: its thinking, with
// the signature, then its text.
func cc290Reply(text string) string {
	return `{"role":"assistant","content":[{"type":"thinking","thinking":"thinking it over","signature":"sig"},{"type":"text","text":` + strconv.Quote(text) + `}]}`
}

// cc290User is the user's message, and the system message Claude Code
// puts after it.
func cc290User(text string) string {
	return rmsg("user", text) + `,{"role":"system","content":"<system-reminder>\n<total_tokens>1000000 tokens left</total_tokens>\n</system-reminder>"}`
}

const suggestionMode = "[SUGGESTION MODE: Suggest what the user might naturally type next into Claude Code.]\n\nFIRST: Look at the user's recent messages and original request.\n\nReply with ONLY the suggestion, no quotes or explanation."

func (h *rewindHarness) ask290(cch, msgs string) string {
	h.t.Helper()
	body := cc290(cch, msgs)
	rec := httptest.NewRecorder()
	var u Usage
	if code, msg := h.s.serveClaudeSubscription(rec, httptest.NewRequest("POST", "/v1/messages", strings.NewReader(body)), provider.Anthropic, h.p, "claude-opus-5-5", []byte(body), &u); code != 200 {
		h.t.Fatalf("%d %s", code, msg)
	}
	for _, line := range strings.Split(rec.Body.String(), "\n") {
		if i := strings.Index(line, `"text":"pid `); i >= 0 {
			var d struct {
				Delta struct{ Text string } `json:"delta"`
			}
			if json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &d) == nil && d.Delta.Text != "" {
				return d.Delta.Text
			}
		}
	}
	h.t.Fatalf("no answer: %s", rec.Body)
	return ""
}

// Claude Code asks its prompt suggestion after each turn as the
// conversation so far and one more user message, "[SUGGESTION MODE: …",
// sharing the turn's cache. Its run took it as the conversation's next
// turn, and kept the suggestion's reply as where it waited, so the user's
// real next turn found no run, and a new Claude Code was told the whole
// conversation, written to the cache again (#1638: 41 messages, 88k
// tokens written, minutes after a run's last reply, with nothing in the
// log). The real turn goes on in the same Claude Code, taken back to
// before the suggestion and told only the user's new message.
func TestClaudeSideQuestionKeepsTheConversationsRun(t *testing.T) {
	h := newRewindHarness(t, "ok")
	first := h.ask290("a1", rmsg("user", "fix the bug")+`,{"role":"system","content":"<system-reminder>\n# Environment\n</system-reminder>"}`)
	pid := pidOf(t, first)
	conv := rmsg("user", "fix the bug") + `,{"role":"system","content":"<system-reminder>\n# Environment\n</system-reminder>"},` + cc290Reply(first) + `,` + cc290User("and the tests")
	second := h.ask290("b2", conv)
	if second != "pid "+pid+" turn 2" {
		t.Fatalf("second: %q", second)
	}
	conv += `,` + cc290Reply(second)
	// the prompt suggestion: the conversation and one message more, no
	// system message after it
	if got := h.ask290("c3", conv+`,`+rmsg("user", suggestionMode)); got != "pid "+pid+" turn 3" {
		t.Fatalf("the suggestion: %q", got)
	}
	// the user's own next turn, minutes later
	third := h.ask290("d4", conv+`,`+cc290User("now commit it"))
	if third != "pid "+pid+" turn 4" {
		t.Fatalf("the turn after the suggestion went to another Claude Code: %q\n%s", third, h.read())
	}
	var suggestion, rewind, next map[string]any
	for _, m := range h.lines() {
		b, _ := json.Marshal(m)
		switch s := string(b); {
		case m["type"] == "user" && strings.Contains(s, "SUGGESTION MODE"):
			suggestion = m
		case m["type"] == "control_request" && strings.Contains(s, "rewind_conversation"):
			rewind = m
		case m["type"] == "user" && strings.Contains(s, "now commit it"):
			next = m
		}
	}
	if suggestion == nil || rewind == nil || next == nil {
		t.Fatalf("Claude Code was told:\n%s", h.read())
	}
	req, _ := rewind["request"].(map[string]any)
	if suggestion["uuid"] == nil || req["target_message_uuid"] != suggestion["uuid"] {
		t.Fatalf("rewound to %v, the suggestion was %v", req, suggestion["uuid"])
	}
	if b, _ := json.Marshal(next); strings.Contains(string(b), "fix the bug") || strings.Contains(string(b), "SUGGESTION") {
		t.Fatalf("the next turn was told more than its new message: %s", b)
	}
	// and the conversation goes on in it after that, suggestions and all
	conv += `,` + cc290User("now commit it") + `,` + cc290Reply(third)
	if got := h.ask290("e5", conv+`,`+rmsg("user", suggestionMode)); got != "pid "+pid+" turn 5" {
		t.Fatalf("the second suggestion: %q", got)
	}
	if got := h.ask290("f6", conv+`,`+cc290User("push it")); got != "pid "+pid+" turn 6" {
		t.Fatalf("the turn after the second suggestion: %q\n%s", got, h.read())
	}
}

// A run that can't be taken back from the side question is let go, and
// the turn told whole to a new one, as before.
func TestClaudeSideQuestionNotRewound(t *testing.T) {
	h := newRewindHarness(t, "refuse")
	first := h.ask290("a1", cc290User("fix the bug"))
	pid := pidOf(t, first)
	conv := cc290User("fix the bug") + `,` + cc290Reply(first) + `,` + cc290User("and the tests")
	second := h.ask290("b2", conv)
	conv += `,` + cc290Reply(second)
	h.ask290("c3", conv+`,`+rmsg("user", suggestionMode))
	third := h.ask290("d4", conv+`,`+cc290User("now commit it"))
	if pidOf(t, third) == pid || !strings.HasSuffix(third, " turn 1") {
		t.Fatalf("the turn: %q, first %q", third, first)
	}
	h.s.subscription.mu.Lock()
	n := len(h.s.subscription.runs)
	h.s.subscription.mu.Unlock()
	if n != 1 {
		t.Fatalf("%d runs, the one that refused kept", n)
	}
}

// syncLog is the gateway's log, kept for a test to read.
type syncLog struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (l *syncLog) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}

func (l *syncLog) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.String()
}

func keepLog(t *testing.T) *syncLog {
	l := &syncLog{}
	old := log.Writer()
	log.SetOutput(l)
	t.Cleanup(func() { log.SetOutput(old) })
	return l
}

// A turn after a reply that finds no run says why in the log, before a
// new Claude Code is told the whole conversation (#1638: nothing said
// why, so a run gone and a key that no longer matched looked the same).
func TestClaudeNoRunSaysWhy(t *testing.T) {
	t.Run("gone", func(t *testing.T) {
		h := newRewindHarness(t, "ok")
		logs := keepLog(t)
		first := h.ask290("a1", cc290User("fix the bug"))
		h.s.subscription.mu.Lock()
		var run *subscriptionRun
		for _, r := range h.s.subscription.idle {
			run = r
		}
		h.s.subscription.mu.Unlock()
		if run == nil {
			t.Fatal("no run waits after the first turn")
		}
		run.timer.Reset(time.Millisecond) // it has waited as long as it may
		eventually(t, "the run let go", func() bool {
			h.s.subscription.mu.Lock()
			defer h.s.subscription.mu.Unlock()
			return len(h.s.subscription.runs) == 0
		})
		h.ask290("b2", cc290User("fix the bug")+`,`+cc290Reply(first)+`,`+cc290User("and?"))
		if want := "no run waits for this turn: its run ended"; !strings.Contains(logs.String(), want) || !strings.Contains(logs.String(), "it waited as long as it may") {
			t.Fatalf("the log says:\n%s", logs)
		}
	})
	t.Run("another key", func(t *testing.T) {
		h := newRewindHarness(t, "ok")
		logs := keepLog(t)
		first := h.ask290("a1", cc290User("fix the bug"))
		// the client's tools changed since
		body := strings.Replace(cc290("b2", cc290User("fix the bug")+`,`+cc290Reply(first)+`,`+cc290User("and?")), `"name":"Bash"`, `"name":"Shell"`, 1)
		var u Usage
		h.s.serveClaudeSubscription(httptest.NewRecorder(), httptest.NewRequest("POST", "/v1/messages", strings.NewReader(body)), provider.Anthropic, h.p, "claude-opus-5-5", []byte(body), &u)
		if want := "no run waits for this turn: its run waits under another key"; !strings.Contains(logs.String(), want) {
			t.Fatalf("the log says:\n%s", logs)
		}
	})
}

// Tool results that find no run say what became of the run that made
// their calls (#1638's first case: "tool results no run waiting" after a
// reply cut short, with nothing to tell a run let go from one never had).
func TestClaudeToolResultsNoRunSaysWhy(t *testing.T) {
	b := &subscriptionBridge{runs: map[string]*subscriptionRun{}, idle: map[string]*subscriptionRun{}, calls: map[string]*subscriptionRun{}}
	answered := &subscriptionRun{token: "t1", shelved: true, results: []string{"toolu_a"}, endWhy: "the client went away during a turn it can't take back"}
	waiting := &subscriptionRun{token: "t2", shelved: true, endWhy: "it waited as long as it may"}
	b.runs["t1"], b.runs["t2"], b.calls["toolu_b"] = answered, waiting, waiting
	b.removeRun(answered)
	b.removeRun(waiting)
	req := func(id string) *Request {
		return &Request{Messages: []Message{
			{Role: "user", Parts: []Part{{Kind: Text, Text: "go"}}},
			{Role: "assistant", Parts: []Part{{Kind: ToolCall, CallID: id}}},
			{Role: "user", Parts: []Part{{Kind: ToolResult, CallID: id, Text: "ok"}}},
		}}
	}
	if got := b.whyCallsLost(req("toolu_a")); !strings.Contains(got, "the run that made the calls ended") || !strings.Contains(got, "can't take back") {
		t.Fatalf("results it was handed: %q", got)
	}
	if got := b.whyCallsLost(req("toolu_b")); !strings.Contains(got, "it waited as long as it may") {
		t.Fatalf("results it waited on: %q", got)
	}
	if got := b.whyCallsLost(req("toolu_c")); got != "" {
		t.Fatalf("calls no run made: %q", got)
	}
}

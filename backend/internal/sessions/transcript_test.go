package sessions

import (
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

// A session's conversation is read whole from its file, in order: a
// Claude Code one's prompts, words, reasoning, calls of tools and their
// results; meta lines are left out, and the file is left as it was (#845).
func TestTranscriptClaude(t *testing.T) {
	file := filepath.Join(t.TempDir(), "sess1.jsonl")
	writeLines(t, file,
		claudeUser(1, `"Fix the login bug"`),
		claudeBlocks("m1", 2, `{"type":"thinking","thinking":"The user wants the login fixed."}`),
		claudeBlocks("m1", 4, `{"type":"text","text":"I'll read the file first."}`),
		claudeBlocks("m1", 5, `{"type":"tool_use","id":"t1","name":"Read","input":{"file_path":"/a/login.go"}}`),
		claudeUser(9, `[{"type":"tool_result","tool_use_id":"t1","content":"package login"}]`),
		`{"type":"user","isMeta":true,"timestamp":"`+stamp(9)+`","message":{"role":"user","content":"meta, left out"}}`,
		claudeBlocks("m2", 12, `{"type":"text","text":"Fixed: the check was inverted."}`),
		claudeUser(20, `"Thanks"`),
	)
	before, _ := os.ReadFile(file)
	tr, err := TranscriptOf(Session{Agent: "claude", ID: "sess1", Path: file})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"user/text: Fix the login bug",
		"assistant/thinking: The user wants the login",
		"assistant/text: I'll read the file first",
		`assistant/tool_use/Read: { "file_path": "/a/login`,
		"tool/tool_result: package login",
		"assistant/text: Fixed: the check was inv",
		"user/text: Thanks",
	}
	if got := kinds(tr.Parts); !reflect.DeepEqual(got, want) {
		t.Fatalf("parts\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	if after, _ := os.ReadFile(file); string(after) != string(before) {
		t.Fatal("the session file was changed")
	}
}

// A Codex rollout's items, all of them, in order.
func TestTranscriptCodex(t *testing.T) {
	file := filepath.Join(t.TempDir(), "rollout.jsonl")
	writeLines(t, file,
		`{"type":"session_meta","payload":{"id":"x"}}`,
		`{"type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"List the files"}]}}`,
		`{"type":"response_item","payload":{"type":"reasoning","summary":[{"type":"summary_text","text":"Run ls."}]}}`,
		`{"type":"response_item","payload":{"type":"function_call","name":"shell","arguments":"{\"command\":[\"ls\"]}","call_id":"c1"}}`,
		`{"type":"response_item","payload":{"type":"function_call_output","call_id":"c1","output":"a.go\nb.go"}}`,
		`{"type":"response_item","payload":{"type":"message","role":"assistant","content":[{"type":"output_text","text":"Two files."}]}}`,
		`{"type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"Thanks"}]}}`,
	)
	tr, err := TranscriptOf(Session{Agent: "codex", ID: "x", Path: file})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"user/text: List the files",
		"assistant/thinking: Run ls.",
		`assistant/tool_use/shell: { "command": [ "ls" ] }`,
		"tool/tool_result: a.go b.go",
		"assistant/text: Two files.",
		"user/text: Thanks",
	}
	if got := kinds(tr.Parts); !reflect.DeepEqual(got, want) {
		t.Fatalf("parts\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

// A Pi session is a tree: the transcript is the branch it stands on, from
// its root to the last entry written, and a branch left behind isn't in it.
func TestTranscriptPiBranch(t *testing.T) {
	file := filepath.Join(t.TempDir(), "pi.jsonl")
	writeLines(t, file,
		`{"type":"session","version":3,"id":"s","cwd":"/w"}`,
		`{"type":"model_change","id":"e1","parentId":null,"provider":"anthropic","modelId":"claude-opus-5-5"}`,
		`{"type":"message","id":"e2","parentId":"e1","message":{"role":"user","content":[{"type":"text","text":"Rename foo"}]}}`,
		`{"type":"message","id":"e3","parentId":"e2","message":{"role":"assistant","content":[{"type":"text","text":"an answer left behind"}]}}`,
		`{"type":"message","id":"e4","parentId":"e2","message":{"role":"assistant","content":[{"type":"thinking","thinking":"grep first"},{"type":"toolCall","id":"t1","name":"bash","arguments":{"command":"grep -r foo"}}]}}`,
		`{"type":"message","id":"e5","parentId":"e4","message":{"role":"toolResult","toolCallId":"t1","toolName":"bash","content":[{"type":"text","text":"a.go: foo()"}]}}`,
		`{"type":"message","id":"e6","parentId":"e5","message":{"role":"assistant","content":[{"type":"text","text":"Renamed."}]}}`,
		`{"type":"message","id":"e7","parentId":"e6","message":{"role":"user","content":"Good"}}`,
	)
	tr, err := TranscriptOf(Session{Agent: "pi", ID: "s", Path: file})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"user/text: Rename foo",
		"assistant/thinking: grep first",
		`assistant/tool_use/bash: { "command": "grep -r fo`,
		"tool/tool_result/bash: a.go: foo()",
		"assistant/text: Renamed.",
		"user/text: Good",
	}
	if got := kinds(tr.Parts); !reflect.DeepEqual(got, want) {
		t.Fatalf("parts\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

// An omp session, as listed: its title line first, a compaction's summary.
func TestTranscriptOmp(t *testing.T) {
	setupAgents(t)
	s, ok := Find("omp", ompMain)
	if !ok {
		t.Fatal("no omp session")
	}
	tr, err := TranscriptOf(s)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"user/text: Port the parser",
		"assistant/text: ok",
		"tool/tool_result/task: done",
		"assistant/text: ok",
		"user/context: Ported.",
	}
	if got := kinds(tr.Parts); !reflect.DeepEqual(got, want) {
		t.Fatalf("parts\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

// An agent whose sessions aren't lines of a file says so.
func TestTranscriptUnsupported(t *testing.T) {
	if _, err := TranscriptOf(Session{Agent: "opencode", ID: "x", Path: "/x"}); err != ErrNoTranscript {
		t.Fatalf("opencode: %v", err)
	}
}

// A long session is cut once it has said enough, and says it was.
func TestTranscriptCut(t *testing.T) {
	file := filepath.Join(t.TempDir(), "long.jsonl")
	line := claudeUser(1, `"`+strings.Repeat("x", 7000)+`"`)
	var lines []string
	for range transcriptMax/7000 + 5 {
		lines = append(lines, line)
	}
	writeLines(t, file, lines...)
	tr, err := TranscriptOf(Session{Agent: "claude", ID: "l", Path: file})
	if err != nil {
		t.Fatal(err)
	}
	if !tr.Cut || len(tr.Parts) >= len(lines) {
		t.Fatalf("cut %v, %d of %d parts", tr.Cut, len(tr.Parts), len(lines))
	}
}

// A Pi session can be carried on in oh-my-pi, which forks it from its file
// as it is; an omp session can't be carried into Pi (Pi wants its header
// first), and nothing is offered when omp isn't installed.
func TestCarryPiToOmp(t *testing.T) {
	setupMore(t)
	there := true
	old := ompThere
	ompThere = func() bool { return there }
	t.Cleanup(func() { ompThere = old })
	p := find(t, List(0), "pi", piFirst)
	want := []Carry{{Agent: "omp", Command: "cd '/work/pi' && omp --fork " + shellQuote(p.Path)}}
	if runtime.GOOS == "windows" {
		want[0].Command = "Set-Location -LiteralPath '/work/pi'; omp --fork '" + p.Path + "'"
	}
	if !reflect.DeepEqual(p.Carry, want) {
		t.Fatalf("pi carry %+v, want %+v", p.Carry, want)
	}
	if m, ok := findManaged(ListAgent("pi"), piFirst); !ok || !reflect.DeepEqual(m.Carry, want) {
		t.Fatalf("managed pi carry %+v", m.Carry)
	}
	if c := carries(Session{Agent: "omp", ID: "o", Cwd: "/w", Path: "/w/o.jsonl"}); c != nil {
		t.Fatalf("omp carried: %+v", c)
	}
	oldThere := agentThere
	agentThere = func(string) bool { return false }
	t.Cleanup(func() { agentThere = oldThere })
	if c := carries(Session{Agent: "claude", ID: "c", Cwd: "/w", Path: "/w/c.jsonl"}); c != nil {
		t.Fatalf("claude carried with no codex: %+v", c)
	}
	there = false
	if c := carries(p); c != nil {
		t.Fatalf("carried with no omp: %+v", c)
	}
}

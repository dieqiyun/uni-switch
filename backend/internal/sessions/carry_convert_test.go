package sessions

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The fixtures are real sessions of the same project, made in a sandbox
// home: Claude Code 2.1.310 running Bash, Write, Read and Edit over two
// turns, and Codex 0.162.1 running exec_command (apply_patch among them)
// over two. Only the attachments' text (the system prompt and instruction
// files Claude Code attached) is taken out; their lines stay, as the chain
// of parents runs through them.
const (
	carryCwd    = "/Users/yetone/workspace/projects/uni-switch-wt/carry-sb/proj"
	carryClaude = "5b0c7a1e-3f2d-4c8b-9a6e-1d2c3b4a5f60"
	carryCodex  = "01a1712c-4d18-79a3-8dad-145c6b40351e"
)

func carrySetup(t *testing.T) (claudeSrc, codexSrc string) {
	claude, codex := setup(t)
	name, err := claudeProjectName(carryCwd)
	if err != nil {
		t.Fatal(err)
	}
	claudeSrc = filepath.Join(claude, "projects", name, carryClaude+".jsonl")
	codexSrc = filepath.Join(codex, "sessions", "2026", "10", "25", "rollout-2026-10-25T11-48-11-"+carryCodex+".jsonl")
	for src, to := range map[string]string{"testdata/carry/claude-tools.jsonl": claudeSrc, "testdata/carry/codex-tools.jsonl": codexSrc} {
		b, err := os.ReadFile(src)
		if err != nil {
			t.Fatal(err)
		}
		os.MkdirAll(filepath.Dir(to), 0o755)
		if err := os.WriteFile(to, b, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	os.WriteFile(filepath.Join(codex, "config.toml"), []byte("model_provider = \"uni-switch\"\n"), 0o644)
	old := agentThere
	agentThere = func(string) bool { return true }
	t.Cleanup(func() {
		agentThere = old
		carriedMu.Lock()
		carried = map[carryKey]CarryResult{}
		carriedMu.Unlock()
	})
	Reset()
	return
}

func readLines(t *testing.T, path string) []map[string]any {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var out []map[string]any
	for _, l := range bytes.Split(bytes.TrimSpace(b), []byte("\n")) {
		var m map[string]any
		if err := json.Unmarshal(l, &m); err != nil {
			t.Fatalf("line %q: %v", l, err)
		}
		out = append(out, m)
	}
	return out
}

func sum(t *testing.T, path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// A Claude Code session is carried on in Codex: a new rollout in Codex's
// own folder, which Codex's own reader takes as a rollout (its provider the
// one Codex uses now, no base_instructions of uni-switch's), the Bash call as
// Codex's exec_command with its output, the other tools as what the agent
// said, the thinking left behind, and the Claude Code file as it was.
func TestCarryClaudeToCodex(t *testing.T) {
	src, _ := carrySetup(t)
	before := sum(t, src)
	s, ok := Find("claude", carryClaude)
	if !ok {
		t.Fatal("claude session not found")
	}
	if len(s.Carry) != 1 || s.Carry[0] != (Carry{Agent: "codex", Convert: true}) {
		t.Fatalf("carry %+v", s.Carry)
	}
	r, err := CarryOver(s, "codex")
	if err != nil {
		t.Fatal(err)
	}
	if r.Agent != "codex" || !strings.HasPrefix(r.Path, filepath.Join(CodexDir(), "sessions")+string(filepath.Separator)) ||
		!strings.HasSuffix(r.Path, "-"+r.ID+".jsonl") || !strings.Contains(r.Command, "codex resume "+r.ID) {
		t.Fatalf("result %+v", r)
	}
	if sum(t, src) != before {
		t.Fatal("the source was changed")
	}
	raw := sum(t, r.Path)
	lines := readLines(t, r.Path)
	meta := lines[0]["payload"].(map[string]any)
	if lines[0]["type"] != "session_meta" || meta["id"] != r.ID || meta["cwd"] != carryCwd || meta["model_provider"] != "uni-switch" || meta["base_instructions"] != nil {
		t.Fatalf("session_meta %v", lines[0])
	}
	if !strings.HasPrefix(raw, `{"timestamp":`) {
		t.Fatalf("a line's fields not in Codex's order: %.80s", raw)
	}
	for _, no := range []string{"The user wants me to", "thinking", "signature", "reasoning"} {
		if strings.Contains(raw, no) {
			t.Fatalf("%q carried over", no)
		}
	}
	var users, says []string
	var call, out map[string]any
	for _, l := range lines[1:] {
		p := l["payload"].(map[string]any)
		switch {
		case l["type"] == "event_msg" && p["type"] == "user_message":
			users = append(users, p["message"].(string))
		case l["type"] == "event_msg" && p["type"] == "agent_message":
			says = append(says, p["message"].(string))
		case p["type"] == "function_call":
			call = p
		case p["type"] == "function_call_output":
			out = p
		}
	}
	if len(users) != 2 || !strings.HasPrefix(users[0], "My dog is called Biscuit.") || !strings.HasPrefix(users[1], "Use the Read tool") {
		t.Fatalf("users %q", users)
	}
	var args struct{ Cmd, Workdir string }
	if call == nil || call["name"] != "exec_command" || json.Unmarshal([]byte(call["arguments"].(string)), &args) != nil ||
		args.Cmd != "wc -l calc.py" || args.Workdir != carryCwd || out == nil || out["call_id"] != call["call_id"] || out["output"] != "       6 calc.py" {
		t.Fatalf("call %v output %v", call, out)
	}
	joined := strings.Join(says, "\n")
	for _, want := range []string{"[Write]", "[Read]", "[Edit]", "def sub(a, b)", "calc.py has 6 lines.", "Done. Added the `sub(a, b)` function"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("%q not said in %q", want, joined)
		}
	}
	// uni-switch lists it as Codex's, and reads it as Codex's
	Reset()
	c, ok := Find("codex", r.ID)
	if !ok || c.Cwd != carryCwd || !strings.HasPrefix(c.Title, "My dog is called Biscuit") || len(c.Models) != 0 {
		t.Fatalf("listed %+v (found %v)", c, ok)
	}
	// asked again for the same file, the same session
	again, err := CarryOver(s, "codex")
	if err != nil || again != r {
		t.Fatalf("again %+v %v, want %+v", again, err, r)
	}
}

// A Codex session is carried on in Claude Code: a new session in the
// project's folder of Claude Code's, its lines chained by parentUuid, the
// shell calls as Bash with their output, Codex's context and reasoning
// left behind, and no line of it counted as a call of a model.
func TestCarryCodexToClaude(t *testing.T) {
	_, src := carrySetup(t)
	before := sum(t, src)
	s, ok := Find("codex", carryCodex)
	if !ok {
		t.Fatal("codex session not found")
	}
	r, err := CarryOver(s, "claude")
	if err != nil {
		t.Fatal(err)
	}
	name, _ := claudeProjectName(carryCwd)
	if r.Path != filepath.Join(ClaudeDir(), "projects", name, r.ID+".jsonl") || !strings.Contains(r.Command, "claude --resume "+r.ID) {
		t.Fatalf("result %+v", r)
	}
	if sum(t, src) != before {
		t.Fatal("the source was changed")
	}
	raw := sum(t, r.Path)
	if !strings.HasPrefix(raw, `{"parentUuid":null,"isSidechain":false,"type":"user"`) {
		t.Fatalf("first line %.120s", raw)
	}
	for _, no := range []string{"environment_context", "skills_instructions", "Answer. Also maybe note", "Need edit.", "encrypted_content", "\\u003c"} {
		if strings.Contains(raw, no) {
			t.Fatalf("%q carried over", no)
		}
	}
	lines := readLines(t, r.Path)
	parent := any(nil)
	var users []string
	var bash, ids []string
	results := map[string]string{}
	for _, l := range lines {
		if l["parentUuid"] != parent || l["sessionId"] != r.ID || l["cwd"] != carryCwd {
			t.Fatalf("line %v after %v", l, parent)
		}
		parent = l["uuid"]
		m := l["message"].(map[string]any)
		if l["type"] == "user" {
			if text, ok := m["content"].(string); ok {
				users = append(users, text)
			} else {
				for _, b := range m["content"].([]any) {
					b := b.(map[string]any)
					results[b["tool_use_id"].(string)] = b["content"].(string)
				}
			}
			continue
		}
		if m["model"] != "<synthetic>" {
			t.Fatalf("assistant line %v", m)
		}
		for _, b := range m["content"].([]any) {
			b := b.(map[string]any)
			if b["type"] == "tool_use" {
				in := b["input"].(map[string]any)
				if b["name"] != "Bash" {
					t.Fatalf("tool %v", b)
				}
				bash = append(bash, in["command"].(string))
				ids = append(ids, b["id"].(string))
			}
		}
	}
	if len(users) != 2 || !strings.HasPrefix(users[0], "My favourite colour is teal.") || !strings.HasPrefix(users[1], "Now add a function mul") {
		t.Fatalf("users %q", users)
	}
	if len(bash) != 2 || bash[0] != "cat calc.py" || !strings.HasPrefix(bash[1], "apply_patch <<'PATCH'") || len(results) != 2 {
		t.Fatalf("bash %q results %q", bash, results)
	}
	for _, id := range ids {
		if !strings.Contains(results[id], "def add(a, b):") {
			t.Fatalf("result of %s: %q", id, results[id])
		}
	}
	// uni-switch lists it as Claude Code's, with nothing spent
	Reset()
	c, ok := Find("claude", r.ID)
	if !ok || c.Cwd != carryCwd || !strings.HasPrefix(c.Title, "My favourite colour is teal") || len(c.Models) != 0 || c.Cost != 0 {
		t.Fatalf("listed %+v (found %v)", c, ok)
	}
	// and it is carried back the way it came
	if len(c.Carry) != 1 || c.Carry[0].Agent != "codex" {
		t.Fatalf("carry back %+v", c.Carry)
	}
}

// Nothing is offered for a session that can't be read here, one in WSL, or
// for an agent that isn't installed; and only what carries offers is made.
func TestCarryConvertOffered(t *testing.T) {
	carrySetup(t)
	base := Session{Agent: "claude", ID: "c", Cwd: "/w", Path: "/w/c.jsonl"}
	if c := carries(base); len(c) != 1 || c[0] != (Carry{Agent: "codex", Convert: true}) {
		t.Fatalf("carries %+v", c)
	}
	for _, s := range []Session{
		{Agent: "claude", ID: "c", Cwd: "/w", Path: "/w/c.jsonl", ReadOnly: true},
		{Agent: "claude", ID: "c", Cwd: "/w", Path: "/w/c.jsonl", WSL: "Ubuntu"},
		{Agent: "claude", ID: "c", Path: "/w/c.jsonl"},
		{Agent: "opencode", ID: "o", Cwd: "/w", Path: "/w/o"},
	} {
		if c := carries(s); c != nil {
			t.Fatalf("%+v carried: %+v", s, c)
		}
	}
	agentThere = func(a string) bool { return a != "codex" }
	if c := carries(base); c != nil {
		t.Fatalf("carried with no codex: %+v", c)
	}
	if _, err := CarryOver(base, "codex"); err == nil {
		t.Fatal("converted what wasn't offered")
	}
	if _, err := CarryOver(Session{Agent: "codex", ID: "x", Cwd: "/w", Path: "/w/x.jsonl"}, "omp"); err == nil {
		t.Fatal("converted for an agent with no writer")
	}
}

// A pasted image goes over as the target's own image, and a compaction
// stands for what came before it: the history Codex kept in its place.
func TestCarryImagesAndCompaction(t *testing.T) {
	dir := t.TempDir()
	img := "iVBORw0KGgo="
	cc := filepath.Join(dir, "c.jsonl")
	os.WriteFile(cc, []byte(`{"parentUuid":null,"isSidechain":false,"type":"user","message":{"role":"user","content":[{"type":"text","text":"what is this?"},{"type":"image","source":{"type":"base64","media_type":"image/png","data":"`+img+`"}}]},"uuid":"a","timestamp":"2026-10-25T03:50:25.170Z"}`+"\n"+
		`{"parentUuid":"a","isSidechain":false,"type":"assistant","message":{"id":"msg_1","role":"assistant","content":[{"type":"thinking","thinking":"hmm","signature":"sig"},{"type":"text","text":"a cat"}]},"uuid":"b","timestamp":"2026-10-25T03:50:26.170Z"}`+"\n"+
		`{"parentUuid":"b","isSidechain":true,"type":"user","message":{"role":"user","content":"side"},"uuid":"s","timestamp":"2026-10-25T03:50:27.170Z"}`+"\n"), 0o644)
	items, err := claudeCarryItems(cc)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 || items[0].Text != "what is this?" || len(items[0].Images) != 1 || items[0].Images[0] != "data:image/png;base64,"+img || items[1].Text != "a cat" {
		t.Fatalf("items %+v", items)
	}
	r, err := writeCodexCarry(dir, "/w", items, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	got, err := codexCarryItems(r.Path)
	if err != nil || len(got) != 2 || len(got[0].Images) != 1 || got[0].Images[0] != items[0].Images[0] || got[1].Text != "a cat" {
		t.Fatalf("codex read back %+v %v", got, err)
	}

	cx := filepath.Join(dir, "x.jsonl")
	os.WriteFile(cx, []byte(`{"timestamp":"2026-10-25T03:48:11.208Z","type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"first, long ago"}]}}`+"\n"+
		`{"timestamp":"2026-10-25T03:48:12.208Z","type":"compacted","payload":{"message":"","replacement_history":[{"type":"message","role":"user","content":[{"type":"input_text","text":"kept"}]},{"type":"message","role":"user","content":[{"type":"input_image","image_url":"data:image/png;base64,`+img+`"}]}]}}`+"\n"+
		`{"timestamp":"2026-10-25T03:48:13.208Z","type":"response_item","payload":{"type":"message","role":"assistant","content":[{"type":"output_text","text":"after"}]}}`+"\n"), 0o644)
	got, err = codexCarryItems(cx)
	if err != nil || len(got) != 3 || got[0].Text != "kept" || len(got[1].Images) != 1 || got[2].Text != "after" {
		t.Fatalf("compacted %+v %v", got, err)
	}
	t.Setenv("CLAUDE_CONFIG_DIR", dir)
	r, err = writeClaudeCarry("/w", got, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	back, err := claudeCarryItems(r.Path)
	if err != nil || len(back) != 3 || back[0].Text != "kept" || len(back[1].Images) != 1 || back[1].Images[0] != "data:image/png;base64,"+img || back[2].Text != "after" {
		t.Fatalf("claude read back %+v %v", back, err)
	}
}

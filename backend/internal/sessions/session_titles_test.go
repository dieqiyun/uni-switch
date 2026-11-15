package sessions

import (
	"os"
	"path/filepath"
	"testing"
)

// A session goes by the name its agent shows for it: Codex's thread name,
// the one a Claude Code session was renamed to, else the title the agent
// made, and the first prompt only when there is none (TJHHHH on Discord).
func TestSessionTitles(t *testing.T) {
	claude, codex := setup(t)
	index := `{"id":"01a0bdc9-b5fd-7e63-9658-68bc8dce5ecd","thread_name":"Codex 起的名字","updated_at":"2026-09-23T00:27:30Z"}` + "\n"
	if err := os.WriteFile(filepath.Join(codex, "session_index.jsonl"), []byte(index), 0o644); err != nil {
		t.Fatal(err)
	}
	ss := List(0)
	cx, cc := ss[0], ss[1]
	if cx.Title != "Codex 起的名字" {
		t.Fatalf("codex: the thread name, not the first prompt: %q", cx.Title)
	}
	if cc.Title != "Fix login bug" {
		t.Fatalf("claude: the title Claude Code made, not the first prompt: %q", cc.Title)
	}

	path := filepath.Join(claude, "projects", "-work-app", "11111111-2222-3333-4444-555555555555.jsonl")
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	f.WriteString(`{"type":"custom-title","customTitle":"Login, renamed","sessionId":"11111111-2222-3333-4444-555555555555"}` + "\n")
	f.Close()
	if got := List(0)[1].Title; got != "Login, renamed" {
		t.Fatalf("claude: the name it was renamed to: %q", got)
	}
}

package gui

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/sessions"
)

// The Sessions page reads a subagent's conversation by its session and its
// id among the subagents that session lists (#2261), never by a path.
func TestSessionSubagentTranscript(t *testing.T) {
	h := sandboxHome(t)
	sessions.Reset()
	t.Cleanup(sessions.Reset)
	const id = "11111111-2222-3333-4444-555555555555"
	proj := filepath.Join(h, ".claude", "projects", "-work-app")
	write := func(p, s string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(s), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(filepath.Join(proj, id+".jsonl"),
		`{"parentUuid":null,"isSidechain":false,"type":"user","message":{"role":"user","content":"Find the callers"},"uuid":"u1","timestamp":"2026-09-20T10:00:00.000Z","cwd":"/work/app","sessionId":"`+id+`"}`+"\n")
	write(filepath.Join(proj, id, "subagents", "agent-a1.jsonl"),
		`{"parentUuid":null,"isSidechain":true,"agentId":"a1","type":"user","message":{"role":"user","content":"Search the repo"},"uuid":"s1","timestamp":"2026-09-20T10:01:00.000Z","cwd":"/work/app","sessionId":"`+id+`"}`+"\n"+
			`{"parentUuid":"s1","isSidechain":true,"agentId":"a1","message":{"model":"claude-haiku-4-5-20251001","id":"msg_s1","type":"message","role":"assistant","content":[{"type":"text","text":"found three"}],"usage":{"input_tokens":10,"cache_creation_input_tokens":0,"cache_read_input_tokens":0,"output_tokens":5}},"type":"assistant","uuid":"s2","timestamp":"2026-09-20T10:02:00.000Z","cwd":"/work/app","sessionId":"`+id+`"}`+"\n")
	write(filepath.Join(proj, id, "subagents", "agent-a1.meta.json"), `{"agentType":"Explore","description":"Search the repo","spawnDepth":1}`)

	mux := http.NewServeMux()
	sessionRoutes(mux, folderOnly{})
	sessionManageRoutes(mux, folderOnly{})
	get := func(path string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		return w
	}

	var list struct {
		Sessions []sessions.Session `json:"sessions"`
	}
	if err := json.Unmarshal(get("/api/sessions/manage?agent=claude").Body.Bytes(), &list); err != nil {
		t.Fatal(err)
	}
	if len(list.Sessions) != 1 || len(list.Sessions[0].Subagents) != 1 || list.Sessions[0].Subagents[0].ID != "a1" || list.Sessions[0].Subagents[0].Kind != "Explore" {
		t.Fatalf("listing %+v", list.Sessions)
	}
	if strings.Contains(get("/api/sessions/manage?agent=claude").Body.String(), "agent-a1.jsonl") {
		t.Fatal("a subagent's path is listed")
	}

	var tr sessions.Transcript
	w := get("/api/sessions/transcript?agent=claude&id=" + id + "&sub=a1")
	if err := json.Unmarshal(w.Body.Bytes(), &tr); err != nil {
		t.Fatal(err)
	}
	if len(tr.Parts) != 2 || tr.Parts[0].Text != "Search the repo" || tr.Parts[1].Text != "found three" {
		t.Fatalf("subagent transcript: %s", w.Body)
	}
	// without sub, the session's own
	w = get("/api/sessions/transcript?agent=claude&id=" + id)
	if !strings.Contains(w.Body.String(), "Find the callers") || strings.Contains(w.Body.String(), "found three") {
		t.Fatalf("session transcript: %s", w.Body)
	}
	for _, sub := range []string{"nope", "../" + id, "a1.meta"} {
		if w := get("/api/sessions/transcript?agent=claude&id=" + id + "&sub=" + sub); w.Code == http.StatusOK || strings.Contains(w.Body.String(), "Find the callers") {
			t.Fatalf("sub %q: %d %s", sub, w.Code, w.Body)
		}
	}
}

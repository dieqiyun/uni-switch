package gui

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/sessions"
)

// The Agents page calls cursor-agent Cursor CLI (@G_Z_Y on X took "Cursor"
// for the editor), but the chats Sessions lists under cursor are the Cursor
// editor's too (its agent-transcripts in ~/.cursor/projects): they are
// Cursor's there, not the CLI's.
func TestCursorEditorChatsAreNotCalledCursorCLI(t *testing.T) {
	h := sandboxHome(t)
	data := filepath.Join(h, ".cursor")
	t.Setenv("CURSOR_DATA_DIR", data)
	id := "11111111-2222-4333-8444-555555555555"
	p := filepath.Join(data, "projects", "work", "agent-transcripts", id, id+".jsonl")
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	body := `{"role":"user","message":{"content":[{"type":"text","text":"<user_query>hi</user_query>"}]}}` + "\n" +
		`{"role":"assistant","message":{"content":[{"type":"text","text":"hello"}]}}` + "\n"
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	sessions.Reset()
	t.Cleanup(sessions.Reset)

	mux := http.NewServeMux()
	sessionRoutes(mux, folderOnly{})
	sessionManageRoutes(mux, folderOnly{})
	get := func(path string, v any) {
		t.Helper()
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		if w.Code != 200 {
			t.Fatalf("%s: %d %s", path, w.Code, w.Body)
		}
		if err := json.Unmarshal(w.Body.Bytes(), v); err != nil {
			t.Fatal(err)
		}
	}
	type named struct {
		Agent string `json:"agent"`
		Name  string `json:"name"`
	}
	var list struct{ Sessions []named }
	get("/api/sessions", &list)
	found := false
	for _, s := range list.Sessions {
		if s.Agent == "cursor" {
			found = true
			if s.Name != "Cursor" {
				t.Errorf("the editor's chat is listed as %q, want Cursor", s.Name)
			}
		}
	}
	if !found {
		t.Fatalf("the editor's chat is not listed: %+v", list.Sessions)
	}
	var manage struct{ Agents []named }
	get("/api/sessions/manage", &manage)
	found = false
	for _, a := range manage.Agents {
		if a.Agent == "cursor" {
			found = true
			if a.Name != "Cursor" {
				t.Errorf("the Sessions page's agent is %q, want Cursor", a.Name)
			}
		}
	}
	if !found {
		t.Fatalf("no cursor among the page's agents: %+v", manage.Agents)
	}
}

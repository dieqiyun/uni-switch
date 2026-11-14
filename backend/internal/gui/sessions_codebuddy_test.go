package gui

import (
	"encoding/json"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/sessions"
)

// CodeBuddy Code's own sessions (~/.codebuddy/projects, the real `codebuddy
// -p` runs in the sessions package's testdata) reach the Sessions page and
// Usage > Sessions under CodeBuddy Code's name and icon, with the command that
// resumes them, and the page offers them to the trash (#2657).
func TestCodeBuddySessionsListedUnderTheirName(t *testing.T) {
	h := sandboxHome(t)
	src := filepath.Join("..", "sessions", "testdata", "codebuddy")
	dst := filepath.Join(h, ".codebuddy")
	if err := filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		if d.IsDir() {
			return os.MkdirAll(filepath.Join(dst, rel), 0o755)
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(dst, rel), b, 0o644)
	}); err != nil {
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
		Agent  string `json:"agent"`
		Name   string `json:"name"`
		Icon   string `json:"icon"`
		Resume string `json:"resume"`
	}
	var list struct{ Sessions []named }
	get("/api/sessions", &list)
	n := 0
	for _, s := range list.Sessions {
		if s.Agent != "codebuddy" {
			continue
		}
		n++
		if s.Name != "CodeBuddy" || s.Icon != "codebuddy-color" {
			t.Errorf("listed as %q (%s), want CodeBuddy (codebuddy-color)", s.Name, s.Icon)
		}
		if !strings.Contains(s.Resume, "codebuddy --resume ") {
			t.Errorf("resume %q", s.Resume)
		}
	}
	if n != 2 {
		t.Fatalf("want 2 CodeBuddy Code sessions, got %d: %+v", n, list.Sessions)
	}
	var manage struct {
		Agents []struct {
			named
			Deletable bool `json:"deletable"`
		}
	}
	get("/api/sessions/manage", &manage)
	found := false
	for _, a := range manage.Agents {
		if a.Agent == "codebuddy" {
			found = true
			if a.Name != "CodeBuddy" {
				t.Errorf("the Sessions page's agent is %q, want CodeBuddy", a.Name)
			}
			if !a.Deletable {
				t.Error("the Sessions page offers no trash for CodeBuddy Code's sessions")
			}
		}
	}
	if !found {
		t.Fatalf("no codebuddy among the Sessions page's agents: %+v", manage.Agents)
	}
}

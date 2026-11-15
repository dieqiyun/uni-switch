package gui

import (
	"encoding/json"
	"github.com/dieqiyun/uni-switch/backend/internal/sessions"
	"github.com/dieqiyun/uni-switch/backend/internal/usage"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The user-facing lists and Routing use Desktop's metadata even when no
// transcript or gateway recording exists, including after a rename.
func TestDesktopSessionTitlesOnPages(t *testing.T) {
	sandboxHome(t)
	sessions.Reset()
	t.Cleanup(sessions.Reset)
	const id = "578bd3d1-834b-43fb-b40c-ed9d17ad7731"
	const prior = "578bd3d1-834b-43fb-b40c-ed9d17ad7732"
	dirs := sessions.DesktopDataDirs()
	path := filepath.Join(dirs[len(dirs)-1], "claude-code-sessions", "account", "org", "local_fixture.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	write := func(name string) {
		t.Helper()
		b, _ := json.Marshal(map[string]any{"sessionId": "local_fixture", "cliSessionId": id, "priorCliSessionIds": []string{prior}, "title": name})
		if err := os.WriteFile(path, b, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("Desktop 会话标题")
	for _, session := range []string{id, prior, "remote-only"} {
		usage.Append(usage.Record{Time: time.Now().Add(-time.Hour), Agent: "claude-desktop", Session: session, Provider: "deepseek", Model: "deepseek-flash", Input: 40, Output: 9, Status: 200})
	}
	mux := http.NewServeMux()
	sessionRoutes(mux, folderOnly{})
	sessionManageRoutes(mux, folderOnly{})
	traceRoutes(mux)
	check := func(name, remoteName string) {
		t.Helper()
		for _, path := range []string{"/api/sessions", "/api/sessions/manage?agent=claude-desktop", "/api/sessions/one?key=claude-desktop:" + id} {
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
			if w.Code != 200 {
				t.Fatalf("%s: %d %s", path, w.Code, w.Body)
			}
			var out struct {
				sessions.Session
				Sessions []sessions.Session `json:"sessions"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
				t.Fatal(err)
			}
			if out.ID != "" {
				out.Sessions = []sessions.Session{out.Session}
			}
			for _, s := range out.Sessions {
				want := name
				if s.ID == "remote-only" {
					want = remoteName
				}
				if s.Title != want || s.Input != 40 || s.Output != 9 || !s.Gateway || !s.ReadOnly || s.Path != "" || s.Resume != "" {
					t.Fatalf("%s: %+v, want title %q and unchanged ledger/capabilities", path, s, want)
				}
			}
			if !strings.Contains(path, "/one?") && len(out.Sessions) != 3 {
				t.Fatalf("%s: got %d sessions, want 3", path, len(out.Sessions))
			}
		}
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest("POST", "/api/gateway/session-titles", strings.NewReader(`{"sessions":["claude-desktop:`+id+`","claude-desktop:`+prior+`","claude:`+id+`"]}`)))
		var out struct {
			Titles map[string]string `json:"titles"`
		}
		if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &out) != nil || len(out.Titles) != 2 || out.Titles["claude-desktop:"+id] != name || out.Titles["claude-desktop:"+prior] != name {
			t.Fatalf("routing names: %d %s", w.Code, w.Body)
		}
	}
	check("Desktop 会话标题", "remote-only")
	write("Desktop renamed")
	// what was read stands for a second (desktopTitlesHeld); Reset is that
	// second gone by
	sessions.Reset()
	check("Desktop renamed", "remote-only")
	// Keep upstream's newly added first-prompt title when no Desktop name
	// is available, while a saved Desktop name still takes precedence.
	if err := sessions.SetGatewayRecording(true, false); err != nil {
		t.Fatal(err)
	}
	for _, session := range []string{id, "remote-only"} {
		if err := sessions.SaveGatewayTurn(sessions.GatewayTurn{Agent: "claude-desktop", Session: session, Time: time.Now().Add(-time.Hour),
			Input: []sessions.Part{{Role: "user", Kind: "text", Text: "Recorded remote prompt"}}}); err != nil {
			t.Fatal(err)
		}
	}
	check("Desktop renamed", "Recorded remote prompt")
}

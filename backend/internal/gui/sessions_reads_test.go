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
	"github.com/dieqiyun/uni-switch/backend/internal/settings"
)

// The Sessions page's consent and Settings' rows (RustCats on X): a fresh
// install's /api/sessions/reads lists the agents found, none on, and
// sessions/manage lists no session until one is allowed; allow selected
// reads that agent only, and Settings turns it off again.
func TestSessionReadsRoutes(t *testing.T) {
	h := sandboxHome(t)
	claude := filepath.Join(h, ".claude")
	t.Setenv("CLAUDE_CONFIG_DIR", claude)
	f := filepath.Join(claude, "projects", "-work-app", "sess1.jsonl")
	if err := os.MkdirAll(filepath.Dir(f), 0o755); err != nil {
		t.Fatal(err)
	}
	line := `{"type":"user","timestamp":"2026-09-20T10:00:00Z","message":{"role":"user","content":"my secret plan"},"cwd":"/work/app","sessionId":"sess1"}`
	if err := os.WriteFile(f, []byte(line+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// a fresh install as SettleReads leaves it: decided, not asked
	if err := settings.SetSessionReads(func(*settings.SessionReads) *settings.SessionReads {
		return &settings.SessionReads{}
	}); err != nil {
		t.Fatal(err)
	}
	sessions.Reset()
	t.Cleanup(sessions.Reset)

	mux := http.NewServeMux()
	sessionManageRoutes(mux, folderOnly{})
	call := func(method, path, body string) []byte {
		t.Helper()
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest(method, path, strings.NewReader(body)))
		if w.Code != 200 {
			t.Fatalf("%s %s: %d %s", method, path, w.Code, w.Body)
		}
		return w.Body.Bytes()
	}
	var r readsJSON
	json.Unmarshal(call("GET", "/api/sessions/reads", ""), &r)
	if r.Asked || r.All || len(r.Found) != 1 || r.Found[0].Agent != "claude" || r.Found[0].On {
		t.Fatalf("fresh reads = %+v, want claude found and off", r)
	}
	type manage struct {
		Sessions []struct {
			ID string `json:"id"`
		} `json:"sessions"`
		Reads readsJSON `json:"reads"`
	}
	var m manage
	json.Unmarshal(call("GET", "/api/sessions/manage", ""), &m)
	if len(m.Sessions) != 0 || m.Reads.Asked || len(m.Reads.Found) != 1 {
		t.Fatalf("not asked: manage = %+v", m)
	}
	if strings.Contains(string(call("GET", "/api/sessions/manage", "")), "secret") {
		t.Fatal("manage carries the session's text before it was allowed")
	}

	// allow selected, Claude Code on
	json.Unmarshal(call("POST", "/api/sessions/reads", `{"all":false,"agents":{"claude":true}}`), &r)
	if !r.Asked || r.All || !r.Found[0].On {
		t.Fatalf("after allow selected: %+v", r)
	}
	m = manage{}
	json.Unmarshal(call("GET", "/api/sessions/manage", ""), &m)
	if len(m.Sessions) != 1 || m.Sessions[0].ID != "sess1" {
		t.Fatalf("claude allowed: manage = %+v", m)
	}

	// Settings turns it off
	json.Unmarshal(call("POST", "/api/sessions/reads", `{"agents":{"claude":false}}`), &r)
	m = manage{}
	json.Unmarshal(call("GET", "/api/sessions/manage", ""), &m)
	if len(m.Sessions) != 0 || !m.Reads.Asked || m.Reads.Found[0].On {
		t.Fatalf("claude turned off: manage = %+v", m)
	}

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest("POST", "/api/sessions/reads", strings.NewReader("{")))
	if w.Code == 200 {
		t.Fatal("a body that isn't JSON was taken")
	}
}

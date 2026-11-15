package gui

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/sessions"
	"github.com/dieqiyun/uni-switch/backend/internal/usage"
)

// A gateway session whose agent keeps it too is one session, in the count
// as in the list, though nothing was said in the agent's own file: the
// count told them apart through List, which leaves such a file out (and
// stops at 5000), while the list kept every one (#2776).
func TestManageCountsAGatewaySessionTheAgentKeepsOnce(t *testing.T) {
	h := sandboxHome(t)
	sessions.Reset()
	t.Cleanup(sessions.Reset)
	const id = "3c9e1f2a-5b6d-4e7f-8a9b-0c1d2e3f4a5b"
	dir := filepath.Join(h, ".claude", "projects", "-work-app")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	line := `{"type":"file-history-snapshot","messageId":"m","snapshot":{"messageId":"m","trackedFileBackups":{},"timestamp":"2026-09-19T08:00:00.000Z"},"isSnapshotUpdate":false}` + "\n"
	if err := os.WriteFile(filepath.Join(dir, id+".jsonl"), []byte(line), 0o644); err != nil {
		t.Fatal(err)
	}
	usage.Append(usage.Record{Time: time.Now().Add(-2 * time.Hour), Agent: "claude", Session: id, Provider: "deepseek", Model: "deepseek-flash", Input: 40, Output: 9, Status: 200})
	mux := http.NewServeMux()
	sessionManageRoutes(mux, folderOnly{})
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest("GET", "/api/sessions/manage?agent=claude", nil))
	if w.Code != 200 {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	var m struct {
		Agents []struct {
			Agent string `json:"agent"`
			Count int    `json:"count"`
		} `json:"agents"`
		Agent    string `json:"agent"`
		Sessions []struct {
			ID      string `json:"id"`
			Gateway bool   `json:"gateway"`
		} `json:"sessions"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &m); err != nil {
		t.Fatal(err)
	}
	if m.Agent != "claude" || len(m.Sessions) != 1 || m.Sessions[0].ID != id || m.Sessions[0].Gateway {
		t.Fatalf("listed %q: %+v", m.Agent, m.Sessions)
	}
	if len(m.Agents) != 1 || m.Agents[0].Count != 1 {
		t.Fatalf("one session listed, counted %+v", m.Agents)
	}
}

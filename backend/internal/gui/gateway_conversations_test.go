package gui

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/sessions"
	"github.com/dieqiyun/uni-switch/backend/internal/settings"
	"github.com/dieqiyun/uni-switch/backend/internal/usage"
)

func TestGatewayConversationAPI(t *testing.T) {
	sandboxHome(t)
	sessions.Reset()
	t.Cleanup(sessions.Reset)
	mux := http.NewServeMux()
	sessionRoutes(mux, folderOnly{})
	sessionManageRoutes(mux, folderOnly{})
	request := func(method, path, body string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest(method, path, strings.NewReader(body)))
		return w
	}
	type manageList struct {
		Sessions []struct {
			sessions.Session
			Gateway bool `json:"gateway"`
		} `json:"sessions"`
		Recording bool `json:"recording"`
		Recorded  bool `json:"recorded"`
	}
	manage := func() (manageList, string) {
		w := request("GET", "/api/sessions/manage?agent=opencode", "")
		var list manageList
		if err := json.Unmarshal(w.Body.Bytes(), &list); err != nil {
			t.Fatal(err)
		}
		return list, w.Body.String()
	}
	// The Sessions page offers to delete kept text only when some is kept.
	if list, body := manage(); list.Recorded {
		t.Fatalf("nothing kept yet, but recorded: %s", body)
	}
	if w := request("POST", "/api/sessions/recording", `{"on":true}`); w.Code != 200 || !settings.Load().GatewayConversations {
		t.Fatalf("consent: %d %s", w.Code, w.Body)
	}
	usage.Append(usage.Record{Time: time.Now(), Agent: "opencode", Session: "remote-only", Provider: "fake", Model: "m", Status: 200})
	if err := sessions.SaveGatewayTurn(sessions.GatewayTurn{Agent: "opencode", Session: "remote-only", Time: time.Now(), Input: []sessions.Part{{Role: "user", Kind: "text", Text: "remote question"}}}); err != nil {
		t.Fatal(err)
	}
	list, body := manage()
	if !list.Recording || !list.Recorded || len(list.Sessions) != 1 || !list.Sessions[0].Gateway || !list.Sessions[0].Transcript || !list.Sessions[0].ReadOnly || list.Sessions[0].Resume != "" {
		t.Fatalf("listing: %s", body)
	}
	w := request("GET", "/api/sessions/transcript?agent=opencode&id=remote-only", "")
	var tr sessions.Transcript
	if err := json.Unmarshal(w.Body.Bytes(), &tr); err != nil {
		t.Fatal(err)
	}
	if tr.Source != "gateway" || len(tr.Parts) != 1 || tr.Parts[0].Text != "remote question" {
		t.Fatalf("transcript: %s", w.Body)
	}
	if w := request("GET", "/api/sessions/transcript?agent=codex&id=remote-only", ""); strings.Contains(w.Body.String(), "remote question") {
		t.Fatal("cross-agent disclosure")
	}
	if w := request("POST", "/api/sessions/recording", `{"clear":true}`); w.Code != 200 {
		t.Fatalf("clear: %s", w.Body)
	}
	w = request("GET", "/api/sessions/transcript?agent=opencode&id=remote-only", "")
	if strings.Contains(w.Body.String(), "remote question") || settings.Load().GatewayConversations {
		t.Fatal("clear retained content or consent")
	}
	if list, body := manage(); list.Recorded || list.Recording {
		t.Fatalf("cleared, but still recorded: %s", body)
	}
	if _, ok := usage.GatewaySessionByID("opencode", "remote-only", nil); !ok {
		t.Fatal("clear removed usage")
	}
	guard := webGuard("test-session", "secret", 0, mux)
	w = httptest.NewRecorder()
	guard.ServeHTTP(w, httptest.NewRequest("GET", "/api/sessions/transcript?agent=opencode&id=remote-only", nil))
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("unguarded transcript: %d", w.Code)
	}
}

func TestGatewaySessionsNativeWinsBeforeLimits(t *testing.T) {
	h := sandboxHome(t)
	claude := filepath.Join(h, ".claude")
	t.Setenv("CLAUDE_CONFIG_DIR", claude)
	proj := filepath.Join(claude, "projects", "-work")
	if err := os.MkdirAll(proj, 0o755); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	for id, offset := range map[string]int{"old-native": -4, "new-native": -1} {
		at := now.AddDate(0, 0, offset).Format(time.RFC3339)
		line := fmt.Sprintf(`{"type":"user","message":{"role":"user","content":"hello"},"uuid":"u","timestamp":%q,"cwd":"/work","sessionId":%q}`+"\n"+
			`{"type":"assistant","message":{"model":"claude-sonnet-5","id":"msg_1","role":"assistant","content":[{"type":"text","text":"done"}],"usage":{"input_tokens":10,"output_tokens":2}},"uuid":"a","timestamp":%q,"cwd":"/work","sessionId":%q}`+"\n", at, id, at, id)
		if err := os.WriteFile(filepath.Join(proj, id+".jsonl"), []byte(line), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	sessions.Reset()
	t.Cleanup(sessions.Reset)
	usage.Append(usage.Record{Time: now, Agent: "claude", Session: "old-native", Provider: "fake", Model: "m", Input: 50, Status: 200})
	mux := http.NewServeMux()
	sessionRoutes(mux, folderOnly{})
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest("GET", "/api/sessions?limit=1", nil))
	var out sessionsJSON
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if len(out.Sessions) != 1 || out.Sessions[0].ID != "new-native" || out.Sessions[0].ReadOnly {
		t.Fatalf("limit exposed a duplicate gateway projection: %s", w.Body)
	}
}

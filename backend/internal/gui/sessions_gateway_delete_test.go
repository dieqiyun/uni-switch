package gui

import (
	"encoding/json"
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

// A Claude Code session seen only through the gateway, its files on another
// computer, was listed with no delete (lc on Discord: "只可列出？不可删除？").
// Delete now moves what uni-switch keeps of it, its row and its recorded text,
// to uni-switch's trash; Restore brings both back; a later request lists it
// again; one still going on is left; and clearing the recorded
// conversations erases the trashed text too.
func TestSessionsDeleteGatewaySession(t *testing.T) {
	sandboxHome(t)
	sessions.Reset()
	t.Cleanup(sessions.Reset)
	if err := sessions.SetGatewayRecording(true, false); err != nil {
		t.Fatal(err)
	}
	const id, busy = "0b6f2a4e-8d1c-4f7a-9c3e-2a5d7e9f1b3c", "7c1d9e2f-3a4b-4c5d-8e6f-9a0b1c2d3e4f"
	then := time.Now().Add(-2 * time.Hour)
	usage.Append(usage.Record{Time: then, Agent: "claude", Session: id, Provider: "deepseek", Model: "deepseek-flash", Input: 40, Output: 9, Status: 200})
	usage.Append(usage.Record{Time: time.Now(), Agent: "claude", Session: busy, Provider: "deepseek", Model: "deepseek-flash", Input: 4, Output: 1, Status: 200})
	if err := sessions.SaveGatewayTurn(sessions.GatewayTurn{Agent: "claude", Session: id, Time: then, Model: "deepseek-flash", Status: 200,
		Input:  []sessions.Part{{Role: "user", Kind: "text", Text: "fix the build"}},
		Output: []sessions.Part{{Role: "assistant", Kind: "text", Text: "done"}}}); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	sessionManageRoutes(mux, folderOnly{})
	type row struct {
		ID        string `json:"id"`
		Gateway   bool   `json:"gateway"`
		Deletable bool   `json:"deletable"`
		Size      int64  `json:"size"`
	}
	type manage struct {
		Agents []struct {
			Agent     string `json:"agent"`
			Deletable bool   `json:"deletable"`
		} `json:"agents"`
		Sessions []row `json:"sessions"`
		Trash    []struct {
			Key     string `json:"key"`
			ID      string `json:"id"`
			Gateway bool   `json:"gateway"`
			Size    int64  `json:"size"`
		} `json:"trash"`
		Recorded bool `json:"recorded"`
	}
	call := func(method, path, body string, out any) {
		t.Helper()
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest(method, path, strings.NewReader(body)))
		if w.Code != 200 {
			t.Fatalf("%s %s: %d %s", method, path, w.Code, w.Body)
		}
		if err := json.Unmarshal(w.Body.Bytes(), out); err != nil {
			t.Fatal(err)
		}
	}
	list := func() (manage, map[string]row) {
		t.Helper()
		var m manage
		call("GET", "/api/sessions/manage?agent=claude", "", &m)
		rows := map[string]row{}
		for _, s := range m.Sessions {
			rows[s.ID] = s
		}
		return m, rows
	}
	m, rows := list()
	if len(m.Agents) != 1 || !m.Agents[0].Deletable {
		t.Fatalf("Claude Code with only gateway sessions isn't deletable: %+v", m.Agents)
	}
	if r := rows[id]; !r.Gateway || !r.Deletable || r.Size == 0 {
		t.Fatalf("gateway session row %+v: want gateway, deletable, its recorded text's size", r)
	}
	if r := rows[busy]; !r.Gateway || r.Size != 0 {
		t.Fatalf("unrecorded gateway session row %+v", r)
	}

	var del struct {
		Deleted []string `json:"deleted"`
		Refused []struct {
			ID     string `json:"id"`
			Active bool   `json:"active"`
		} `json:"refused"`
	}
	call("POST", "/api/sessions/delete", `{"agent":"claude","ids":["`+id+`","`+busy+`"]}`, &del)
	if len(del.Deleted) != 1 || del.Deleted[0] != id || len(del.Refused) != 1 || del.Refused[0].ID != busy || !del.Refused[0].Active {
		t.Fatalf("delete: %+v", del)
	}
	m, rows = list()
	if _, ok := rows[id]; ok {
		t.Fatal("a deleted gateway session is still listed")
	}
	if _, ok := rows[busy]; !ok {
		t.Fatal("the session still going on was taken off the list")
	}
	if tr, _ := sessions.GatewayTranscript("claude", id); len(tr.Parts) != 0 {
		t.Fatalf("its recorded text is still readable: %+v", tr.Parts)
	}
	if len(m.Trash) != 1 || !m.Trash[0].Gateway || m.Trash[0].ID != id || m.Trash[0].Size == 0 {
		t.Fatalf("trash: %+v", m.Trash)
	}
	var api struct {
		Sessions []struct {
			ID string `json:"id"`
		} `json:"sessions"`
	}
	routes := http.NewServeMux()
	sessionRoutes(routes, folderOnly{})
	w := httptest.NewRecorder()
	routes.ServeHTTP(w, httptest.NewRequest("GET", "/api/sessions", nil))
	json.Unmarshal(w.Body.Bytes(), &api)
	for _, s := range api.Sessions {
		if s.ID == id {
			t.Fatal("the Usage page's list still has the deleted gateway session")
		}
	}

	var back map[string]string
	call("POST", "/api/sessions/restore", `{"key":"`+m.Trash[0].Key+`"}`, &back)
	if _, rows = list(); rows[id].ID == "" || rows[id].Size == 0 {
		t.Fatalf("restored session %+v: want listed with its text", rows[id])
	}
	if tr, _ := sessions.GatewayTranscript("claude", id); len(tr.Parts) == 0 {
		t.Fatal("restore didn't bring its recorded text back")
	}

	// deleted again, then used again: listed again
	call("POST", "/api/sessions/delete", `{"agent":"claude","ids":["`+id+`"]}`, &del)
	if _, rows = list(); rows[id].ID != "" {
		t.Fatal("deleted twice, still listed")
	}
	usage.Append(usage.Record{Time: then.Add(time.Hour), Agent: "claude", Session: id, Provider: "deepseek", Model: "deepseek-flash", Input: 5, Output: 1, Status: 200})
	if _, rows = list(); rows[id].ID == "" {
		t.Fatal("a request after the delete didn't list the session again")
	}

	// clearing the recorded conversations clears the trashed text too
	var rec map[string]bool
	call("POST", "/api/sessions/recording", `{"clear":true}`, &rec)
	m, _ = list()
	if m.Recorded {
		t.Fatal("recorded text is still said to be kept")
	}
	left, _ := filepath.Glob(filepath.Join(settings.Dir(), "trash", "sessions", "gateway", "*", "files", "*"))
	if len(left) != 0 {
		t.Fatalf("trashed gateway text survived clearing: %v", left)
	}
	if _, err := os.Stat(filepath.Join(settings.Dir(), "gateway-conversations")); !os.IsNotExist(err) {
		t.Fatalf("store after clear: %v", err)
	}
}

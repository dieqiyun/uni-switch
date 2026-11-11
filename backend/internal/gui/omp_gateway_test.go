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
	"github.com/dieqiyun/uni-switch/backend/internal/usage"
)

// Jumy (Discord, 10-30): an omp task through uni-switch's gateway was counted
// far past what it spent. omp's subagent (the task tool's Kid.jsonl) asks
// with its own session id in x-session-affinity, so the gateway records
// its calls under that id, while the main session counts the same calls
// from Kid.jsonl. The Usage page's sessions count them once: the main
// session's, with no gateway session beside it for the subagent. The
// lines are omp 18.9.1's, as it wrote them for a task run through uni-switch.
func TestOmpSubagentCountedOnceWithTheGateway(t *testing.T) {
	h := sandboxHome(t)
	const (
		mainID = "01a18ae4-555c-75c6-bf5d-5944c8844363"
		kidID  = "01a18ae4-5671-77fb-9d9b-b943fc780e73"
	)
	dir := filepath.Join(h, ".omp", "agent", "sessions", "--work--")
	stem := "2026-10-30T03-39-42-300Z_" + mainID
	if err := os.MkdirAll(filepath.Join(dir, stem), 0o755); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	at := func(d time.Duration) string { return now.Add(d).UTC().Format("2006-01-02T15:04:05.000Z") }
	title := `{"type":"title","v":1,"title":"","updatedAt":%q,"pad":"                "}` + "\n"
	user := `{"type":"message","id":"u%d","parentId":null,"timestamp":%q,"message":{"role":"user","content":[{"type":"text","text":"spawn a child"}],"timestamp":%d}}` + "\n"
	reply := `{"type":"message","id":"a%d","parentId":"u0","timestamp":%q,"message":{"role":"assistant","content":[{"type":"text","text":"ok"}],"api":"openai-completions","provider":"uni-switch","model":"fake-model","usage":{"input":200,"output":50,"cacheRead":800,"cacheWrite":0,"totalTokens":1050,"cost":{"input":0,"output":0,"cacheRead":0,"cacheWrite":0,"total":0}},"stopReason":"stop","timestamp":%d,"responseId":"x"}}` + "\n"
	write := func(path, header string, replies int, start time.Duration) {
		var b strings.Builder
		fmt.Fprintf(&b, title, at(start))
		b.WriteString(header)
		fmt.Fprintf(&b, user, 0, at(start), now.Add(start).UnixMilli())
		for i := range replies {
			d := start + time.Duration(i+1)*time.Second
			fmt.Fprintf(&b, reply, i, at(d), now.Add(d).UnixMilli())
		}
		if err := os.WriteFile(path, []byte(b.String()), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write(filepath.Join(dir, stem+".jsonl"), fmt.Sprintf(`{"type":"session","version":3,"id":%q,"timestamp":%q,"cwd":"/work"}`+"\n", mainID, at(-time.Hour)), 3, -time.Hour)
	write(filepath.Join(dir, stem, "Kid.jsonl"), fmt.Sprintf(`{"type":"session","version":3,"id":%q,"timestamp":%q,"cwd":"/work","parentSession":%q}`+"\n", kidID, at(-time.Hour+10*time.Second), filepath.Join(dir, stem+".jsonl")), 5, -time.Hour+10*time.Second)
	sessions.Reset()
	t.Cleanup(sessions.Reset)
	// the gateway's records of the same calls, each under the session id
	// its request sent
	for i, id := range []string{mainID, mainID, mainID, kidID, kidID, kidID, kidID, kidID} {
		usage.Append(usage.Record{Time: now.Add(-time.Hour + time.Duration(i)*time.Second), Agent: "omp", Session: id, Requested: "fake-model", Provider: "fake", Model: "fake-model", Input: 1000, Output: 50, CacheRead: 800, Status: 200})
	}

	mux := http.NewServeMux()
	sessionRoutes(mux, folderOnly{})
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest("GET", "/api/sessions", nil))
	if w.Code != 200 {
		t.Fatalf("sessions: %d %s", w.Code, w.Body)
	}
	var list struct {
		Sessions []map[string]any `json:"sessions"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &list); err != nil {
		t.Fatal(err)
	}
	var ids []any
	for _, s := range list.Sessions {
		ids = append(ids, s["id"])
	}
	if len(ids) != 1 || ids[0] != mainID {
		t.Fatalf("omp's sessions as listed: %v", ids)
	}
}

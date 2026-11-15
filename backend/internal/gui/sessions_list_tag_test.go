package gui

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/sessions"
	"github.com/dieqiyun/uni-switch/backend/internal/settings"
	"github.com/dieqiyun/uni-switch/backend/internal/usage"
)

// TestSessionsSameWithoutRows: an unchanged Sessions poll made every row,
// encoded and hashed the whole list again only to answer X-Uni-Switch-Same,
// ~135 ms of CPU and ~22 MB a poll over ~17k real sessions. While the
// listing, the usage log and the aliases stand, an ask with the last
// tag is answered without the rows; a gateway call, an alias and a
// session file each make them again.
func TestSessionsSameWithoutRows(t *testing.T) {
	h := sandboxHome(t)
	proj := filepath.Join(h, ".claude", "projects", "-work")
	if err := os.MkdirAll(proj, 0o755); err != nil {
		t.Fatal(err)
	}
	write := func(id, at string) {
		b := `{"parentUuid":null,"isSidechain":false,"type":"user","message":{"role":"user","content":"hi"},"uuid":"u","timestamp":"` + at + `","cwd":"/work","sessionId":"` + id + `"}` + "\n" +
			`{"parentUuid":"u","isSidechain":false,"message":{"model":"claude-sonnet-5","id":"msg_` + id + `","type":"message","role":"assistant","content":[{"type":"text","text":"done"}],"usage":{"input_tokens":10,"output_tokens":2}},"requestId":"req_1","type":"assistant","uuid":"a","timestamp":"` + at + `","cwd":"/work","sessionId":"` + id + `"}` + "\n"
		if err := os.WriteFile(filepath.Join(proj, id+".jsonl"), []byte(b), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("00000001-1111-2222-3333-444444444444", "2026-09-20T10:00:00.000Z")
	t.Setenv("CLAUDE_CONFIG_DIR", filepath.Join(h, ".claude"))
	sessions.Reset()
	t.Cleanup(sessions.Reset)

	mux := http.NewServeMux()
	sessionRoutes(mux, folderOnly{})
	tag := ""
	poll := func(what string, wantSame bool) {
		t.Helper()
		r := httptest.NewRequest("GET", "/api/sessions", nil)
		r.Header.Set("X-Uni-Switch-Had", tag)
		w := httptest.NewRecorder()
		rowed := listsRowed.Load()
		mux.ServeHTTP(w, r)
		if w.Code != 200 {
			t.Fatalf("%s: %d %s", what, w.Code, w.Body)
		}
		same, made := w.Header().Get("X-Uni-Switch-Same") == "1", listsRowed.Load()-rowed
		if same != wantSame || wantSame && (made != 0 || w.Body.Len() != 0) || !wantSame && made != 1 {
			t.Fatalf("%s: same %v, rows made %d times, %d bytes; want same %v", what, same, made, w.Body.Len(), wantSame)
		}
		if got := w.Header().Get("X-Uni-Switch-Tag"); got == "" || wantSame && got != tag {
			t.Fatalf("%s: tag %q, had %q", what, got, tag)
		}
		tag = w.Header().Get("X-Uni-Switch-Tag")
	}
	poll("the first ask", false)
	for range 3 {
		poll("an unchanged poll", true)
	}

	usage.Append(usage.Record{Time: time.Now(), Agent: "opencode", Session: "gw-only", Provider: "fake", Model: "m", Status: 200})
	poll("after a gateway call", false)
	poll("the poll after it", true)

	if err := settings.EditUsageAliases(func(map[string]string) (map[string]string, error) {
		return map[string]string{"claude-sonnet-5": "sonnet"}, nil
	}); err != nil {
		t.Fatal(err)
	}
	poll("after an alias", false)
	poll("the poll after it", true)

	write("00000002-1111-2222-3333-444444444444", "2026-09-21T10:00:00.000Z")
	future := time.Now().Add(time.Minute)
	if err := os.Chtimes(filepath.Join(proj, "00000002-1111-2222-3333-444444444444.jsonl"), future, future); err != nil {
		t.Fatal(err)
	}
	poll("after a new session", false)
	poll("the poll after it", true)

	// another tag than the last sent is answered whole
	tag = "0123"
	poll("an ask with another tag", false)
}

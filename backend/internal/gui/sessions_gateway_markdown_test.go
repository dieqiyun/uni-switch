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

// akic404 on Discord: a uni-switch on a server that other uni-switchs reach as a
// remote uni-switch records their agents' conversations, and the Sessions page
// shows them, but its download of one as Markdown was a 400: the agent ran
// on the other machine, so there is no file of the session here, and the
// export looked only for one. It is written from what the gateway kept, as
// the page shows it, and says so.
func TestGatewaySessionExportsMarkdown(t *testing.T) {
	h := sandboxHome(t)
	sessions.Reset()
	t.Cleanup(sessions.Reset)
	if err := os.MkdirAll(filepath.Join(h, "Downloads"), 0o755); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	sessionRoutes(mux, folderOnly{})
	sessionManageRoutes(mux, folderOnly{})
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest("POST", "/api/sessions/recording", strings.NewReader(`{"on":true}`)))
	if w.Code != 200 || !settings.Load().GatewayConversations {
		t.Fatalf("consent: %d %s", w.Code, w.Body)
	}
	id := "019a2b3c-pi-remote"
	usage.Append(usage.Record{Time: time.Now(), Agent: "pi", Session: id, Provider: "fake", Model: "m", Status: 200})
	if err := sessions.SaveGatewayTurn(sessions.GatewayTurn{Agent: "pi", Session: id, Time: time.Now(),
		Input:  []sessions.Part{{Role: "user", Kind: "text", Text: "rename foo to bar"}},
		Output: []sessions.Part{{Role: "assistant", Kind: "text", Text: "Renamed it in a.go."}}}); err != nil {
		t.Fatal(err)
	}
	check := func(md string) {
		t.Helper()
		for _, want := range []string{"- **Agent:** Pi\n", "- **Recorded by:** uni-switch's gateway\n", "## User\n\nrename foo to bar\n", "## Assistant\n\nRenamed it in a.go.\n"} {
			if !strings.Contains(md, want) {
				t.Fatalf("no %q in\n%s", want, md)
			}
		}
	}
	q := "?agent=pi&id=" + id
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest("GET", "/api/sessions/markdown"+q, nil))
	if w.Code != 200 || !strings.Contains(w.Header().Get("Content-Disposition"), `filename="uni-switch-session-pi-019a2b3c-pi-.md"`) {
		t.Fatalf("download: %d %v %s", w.Code, w.Header(), w.Body)
	}
	check(w.Body.String())

	w = httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest("POST", "/api/sessions/export"+q, strings.NewReader("{}")))
	var out struct{ Path string }
	json.Unmarshal(w.Body.Bytes(), &out)
	if w.Code != 200 {
		t.Fatalf("export: %d %s", w.Code, w.Body)
	}
	b, err := os.ReadFile(filepath.Join(h, "Downloads", filepath.Base(out.Path)))
	if err != nil {
		t.Fatal(err)
	}
	check(string(b))

	// another agent's session of the same id isn't this one
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest("GET", "/api/sessions/markdown?agent=codex&id="+id, nil))
	if w.Code == 200 || strings.Contains(w.Body.String(), "rename foo") {
		t.Fatalf("cross-agent: %d %s", w.Code, w.Body)
	}
}

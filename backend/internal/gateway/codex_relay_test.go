package gateway

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// relayUp is a fake ChatGPT backend for the relay: what it was sent.
type relayUp struct {
	mu   sync.Mutex
	got  []relayGot
	srv  *httptest.Server
	code int
}

type relayGot struct {
	path, auth, body, encoding string
}

func newRelayUp(t *testing.T) *relayUp {
	t.Helper()
	up := &relayUp{code: 200}
	up.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		up.mu.Lock()
		up.got = append(up.got, relayGot{r.URL.Path, r.Header.Get("Authorization"), string(b), r.Header.Get("Content-Encoding")})
		up.mu.Unlock()
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(up.code)
		io.WriteString(w, "event: response.completed\ndata: {\"type\":\"response.completed\"}\n\n")
	}))
	t.Cleanup(up.srv.Close)
	was := provider.CodexBase
	provider.CodexBase = up.srv.URL + "/backend-api/codex"
	t.Cleanup(func() { provider.CodexBase = was })
	return up
}

func (up *relayUp) sent() []relayGot {
	up.mu.Lock()
	defer up.mu.Unlock()
	return append([]relayGot(nil), up.got...)
}

func relayDo(h http.Handler, method, host, path, body string, hdr map[string]string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Host = host
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// A Codex thread open when uni-switch quit keeps sending its turns to the
// gateway's address (idkatbro on X: 503 "Unknown error" until uni-switch was
// opened again). The relay passes one on Codex's own model to the ChatGPT
// backend with the caller's own sign-in, and its reply comes back whole.
func TestCodexRelayPassesOwnTurnOn(t *testing.T) {
	fresh(t)
	up := newRelayUp(t)
	h := CodexRelay()
	rec := relayDo(h, "POST", "127.0.0.1:3425", CodexPath+"/responses", `{"model":"gpt-5.5","input":"hi","stream":true}`,
		map[string]string{"Authorization": "Bearer chatgpt-token", "Content-Type": "application/json"})
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "response.completed") {
		t.Fatalf("%d %s", rec.Code, rec.Body.String())
	}
	got := up.sent()
	if len(got) != 1 || got[0].path != "/backend-api/codex/responses" || got[0].auth != "Bearer chatgpt-token" {
		t.Fatalf("upstream was sent %+v", got)
	}
	// the model list Codex asks for, and localhost by name, go on too
	rec = relayDo(h, "GET", "localhost:3425", CodexPath+"/models?client_version=0.162.1", "", map[string]string{"Authorization": "Bearer chatgpt-token"})
	if rec.Code != 200 || len(up.sent()) != 2 || up.sent()[1].path != "/backend-api/codex/models" {
		t.Fatalf("%d %+v", rec.Code, up.sent())
	}
}

// What only the running uni-switch serves is refused with a reason Codex
// shows at once (a 4xx; a 5xx it retries five times over), and nothing
// goes to OpenAI: a uni-switch model, a turn on one of
// uni-switch's accounts.
func TestCodexRelayRefusesWhatNeedsUniSwitch(t *testing.T) {
	fresh(t)
	up := newRelayUp(t)
	h := CodexRelay()
	auth := map[string]string{"Authorization": "Bearer chatgpt-token"}
	for _, c := range []struct {
		name, body string
		hdr        map[string]string
		want       string
	}{
		{"uni-switch model", `{"model":"openrouter/kimi-k2","input":"hi"}`, auth, "one of uni-switch's models"},
		{"uni-switch account", `{"model":"gpt-5.5","input":"hi"}`, map[string]string{"Authorization": "Bearer t", AccountHeader: "a@b.c"}, "uni-switch's Codex accounts"},
	} {
		rec := relayDo(h, "POST", "127.0.0.1:3425", CodexPath+"/responses", c.body, c.hdr)
		var e struct {
			Error struct{ Message string } `json:"error"`
		}
		json.Unmarshal(rec.Body.Bytes(), &e)
		if rec.Code != http.StatusBadRequest || !strings.Contains(e.Error.Message, c.want) || !strings.Contains(e.Error.Message, "uni-switch isn't running") {
			t.Errorf("%s: %d %s", c.name, rec.Code, rec.Body.String())
		}
	}
	if got := up.sent(); len(got) != 0 {
		t.Fatalf("OpenAI was sent %+v", got)
	}
}

// Only Codex on this computer is served: a page (any Origin, null too)
// and a name rebound to 127.0.0.1 are turned away before anything goes
// on, as corsGuard turns them from the gateway.
func TestCodexRelayOnlyThisComputer(t *testing.T) {
	fresh(t)
	up := newRelayUp(t)
	h := CodexRelay()
	body := `{"model":"gpt-5.5","input":"hi"}`
	for _, c := range []struct{ host, origin string }{
		{"127.0.0.1:3425", "null"},
		{"127.0.0.1:3425", "https://evil.example"},
		{"evil.example:3425", ""},
		{"uni-switch.localhost:3425", ""},
	} {
		hdr := map[string]string{"Authorization": "Bearer chatgpt-token"}
		if c.origin != "" {
			hdr["Origin"] = c.origin
		}
		if rec := relayDo(h, "POST", c.host, CodexPath+"/responses", body, hdr); rec.Code != 403 {
			t.Errorf("%+v: %d %s", c, rec.Code, rec.Body.String())
		}
	}
	if got := up.sent(); len(got) != 0 {
		t.Fatalf("OpenAI was sent %+v", got)
	}
	if rec := relayDo(h, "POST", "[::1]:3425", CodexPath+"/responses", body, map[string]string{"Authorization": "Bearer t"}); rec.Code != 200 {
		t.Fatalf("::1: %d %s", rec.Code, rec.Body.String())
	}
}

// Anything but Codex's turns says uni-switch isn't running, readably; and
// the relay's answer at "/" is never taken for uni-switch's (ServedBy), so
// the next uni-switch starting still serves the gateway. An OpenAI that
// can't be reached is said too, never an empty body Codex shows as
// "Unknown error".
func TestCodexRelayIsNotUniSwitch(t *testing.T) {
	fresh(t)
	newRelayUp(t)
	h := CodexRelay()
	for _, p := range []string{"/", "/v1/models", "/v1/chat/completions"} {
		rec := relayDo(h, "GET", "127.0.0.1:3425", p, "", nil)
		b := rec.Body.Bytes()
		var info struct {
			Name string `json:"name"`
		}
		json.Unmarshal(b, &info)
		if rec.Code != 503 || info.Name == "uni-switch" || bytes.Contains(b, []byte(`"uni-switch"`)) || !strings.Contains(string(b), "uni-switch isn't running") {
			t.Errorf("%s: %d %s", p, rec.Code, b)
		}
	}
	provider.CodexBase = "http://127.0.0.1:1/backend-api/codex"
	rec := relayDo(h, "POST", "127.0.0.1:3425", CodexPath+"/responses", `{"model":"gpt-5.5","input":"hi"}`, map[string]string{"Authorization": "Bearer t"})
	if rec.Code != 502 || !strings.Contains(rec.Body.String(), "OpenAI can't be reached") {
		t.Fatalf("%d %s", rec.Code, rec.Body.String())
	}
	// a WebSocket is sent to plain HTTP, as the gateway does
	if rec := relayDo(h, "GET", "127.0.0.1:3425", CodexPath+"/responses", "", map[string]string{"Upgrade": "websocket"}); rec.Code != http.StatusUpgradeRequired {
		t.Fatalf("websocket: %d", rec.Code)
	}
}

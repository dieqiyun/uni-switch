package gateway

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
	"github.com/tidwall/gjson"
)

// streamModels is every "model" the data lines of an SSE body hold, at
// any depth.
func streamModels(t *testing.T, body string) []string {
	t.Helper()
	var models []string
	var walk func(v any)
	walk = func(v any) {
		switch v := v.(type) {
		case map[string]any:
			if m, ok := v["model"].(string); ok {
				models = append(models, m)
			}
			for _, x := range v {
				walk(x)
			}
		case []any:
			for _, x := range v {
				walk(x)
			}
		}
	}
	for _, line := range strings.Split(body, "\n") {
		d, ok := strings.CutPrefix(line, "data: ")
		if !ok || d == "[DONE]" {
			continue
		}
		var v any
		if err := json.Unmarshal([]byte(d), &v); err != nil {
			t.Fatalf("a data line that isn't JSON: %q", d)
		}
		walk(v)
	}
	return models
}

// lateUpstream is a vendor that says nothing until the agent has heard a
// keepalive (or 5s), then answers x; with no code, it hangs up with no
// reply, the Go client's "EOF", as chatgpt.com's in #3728.
func lateUpstream(t *testing.T, heard <-chan struct{}, x reply) *httptest.Server {
	t.Helper()
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.ReadAll(r.Body)
		select {
		case <-heard:
		case <-time.After(5 * time.Second):
		}
		if x.code != 0 {
			w.Header().Set("Content-Type", x.ctype)
			w.WriteHeader(x.code)
			io.WriteString(w, x.body)
			return
		}
		if c, _, err := w.(http.Hijacker).Hijack(); err == nil {
			c.Close()
		}
	}))
	t.Cleanup(up.Close)
	return up
}

// An error sent in a stream already begun names the model the agent
// asked for wherever its protocol says one ahead of it (#3728). Chat's
// first chunk went with "model":"", which fx takes for a broken chunk:
// it stopped there and never read the error after it. Anthropic's
// message_start and Responses' response.created, response.in_progress and
// response.failed had it empty too. Gemini's error says no model.
func TestStreamErrorNamesTheModel(t *testing.T) {
	const asked = "codex/gpt-6.1-sol"
	for _, c := range []struct {
		proto provider.Protocol
		named bool // the protocol says a model ahead of the error
	}{
		{provider.Chat, true},
		{provider.Anthropic, true},
		{provider.Responses, true},
		{provider.Gemini, false},
	} {
		rec := httptest.NewRecorder()
		streamError(rec, c.proto, asked, http.StatusBadGateway, `Post "https://chatgpt.com/backend-api/codex/responses": EOF`)
		body := rec.Body.String()
		models := streamModels(t, body)
		if c.named && len(models) == 0 {
			t.Errorf("%s: no model named: %s", c.proto, body)
		}
		if !c.named && len(models) > 0 {
			t.Errorf("%s: names %q, want none: %s", c.proto, models, body)
		}
		for _, m := range models {
			if m != asked {
				t.Errorf("%s: model %q, want %q: %s", c.proto, m, asked, body)
			}
		}
		if !strings.Contains(body, "EOF") {
			t.Errorf("%s: the error isn't in the stream: %s", c.proto, body)
		}
	}
}

// #3728's case: a Chat stream kept alive while its vendor said nothing,
// then the vendor gone with no reply. The error goes in the stream, and
// the chunk ahead of it names the model the agent asked for; the same
// for each way the vendor can fail after the stream's headers went out:
// an error status, told as it is (holdWriter.release), a 413, told with
// the byte cap's words (failTo), a 200 that isn't a stream, and a JSON
// 200 that is no reply, which release tells as the failure it is. A model
// asked for at an effort (#536) is named with it, as the agent asked.
func TestKeptAliveChatErrorNamesTheModel(t *testing.T) {
	for _, c := range []struct {
		name  string
		model string
		x     reply
	}{
		{"gone", "a/m", reply{}},
		{"gone, at an effort", "a/m:high", reply{}},
		{"502", "a/m", reply{http.StatusBadGateway, "application/json", `{"error":{"message":"upstream connect error","type":"upstream_error"}}`}},
		{"413", "a/m", reply{http.StatusRequestEntityTooLarge, "application/json", `{"error":{"message":"request entity too large","type":"invalid_request_error"}}`}},
		{"a 200 that isn't a stream", "a/m", reply{http.StatusOK, "text/html", "<html><body>down for maintenance</body></html>"}},
		{"a 200 JSON that isn't a reply", "a/m", reply{http.StatusOK, "application/json", `{"status":"ok"}`}},
	} {
		t.Run(c.name, func(t *testing.T) {
			quietFast(t)
			fresh(t)
			pause := retryPause
			retryPause = time.Millisecond
			t.Cleanup(func() { retryPause = pause })
			heard := make(chan struct{})
			up := lateUpstream(t, heard, c.x)
			if err := provider.Save(provider.Provider{ID: "a", Name: "A", Key: "k", Models: []string{"m"}, Chat: up.URL + "/v1"}); err != nil {
				t.Fatal(err)
			}
			code, body, _ := askChat(t, New(), c.model, heard)
			keptThenFailed(t, code, body, c.model)
			if c.x.code == http.StatusRequestEntityTooLarge && !strings.Contains(body, "request_too_large") {
				t.Fatalf("the byte cap's words aren't in the error: %q", body)
			}
		})
	}
}

// keptThenFailed checks a Chat stream kept alive and then failed: its
// headers went out ahead of the failure, its first chunk names model, and
// the error follows.
func keptThenFailed(t *testing.T, code int, body, model string) {
	t.Helper()
	if code != http.StatusOK || !strings.Contains(body, ": keepalive") {
		t.Fatalf("the stream's headers didn't go out ahead of the failure: %d %q", code, body)
	}
	var first string
	for _, line := range strings.Split(body, "\n") {
		if d, ok := strings.CutPrefix(line, "data: "); ok {
			first = d
			break
		}
	}
	if got := gjson.Get(first, "model"); got.String() != model {
		t.Fatalf("the stream's first chunk names %s, want %s: %q", got.Raw, model, body)
	}
	if !strings.Contains(body, `{"error":`) {
		t.Fatalf("no error in the stream: %q", body)
	}
}

// #3728's own route: fx on uni-switch's codex provider (Codex's sign-in), its
// Chat turn asked on the ChatGPT backend, which hangs up once fx was kept
// alive, each time it is asked. The error is the report's EOF.
func TestKeptAliveChatOnCodexErrorNamesTheModel(t *testing.T) {
	quietFast(t)
	codexSignedIn(t)
	pause := retryPause
	retryPause = time.Millisecond
	t.Cleanup(func() { retryPause = pause })
	heard := make(chan struct{})
	up := lateUpstream(t, heard, reply{})
	was := provider.CodexBase
	provider.CodexBase = up.URL + "/backend-api/codex"
	t.Cleanup(func() { provider.CodexBase = was })
	code, body, _ := askChat(t, New(), "codex/gpt-5.5", heard)
	keptThenFailed(t, code, body, "codex/gpt-5.5")
	if !strings.Contains(body, "EOF") {
		t.Fatalf("the error isn't the backend's EOF: %q", body)
	}
}

// The same on Codex's own sign-in (codexBackend): the backend overloaded,
// Codex kept alive while the turn waits to be asked again, then the
// backend gone. The response.created and response.failed Codex is sent
// name the model it asked for.
func TestKeptAliveCodexErrorNamesTheModel(t *testing.T) {
	quietFast(t)
	codexSignedIn(t)
	var mu sync.Mutex
	asked := 0
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.ReadAll(r.Body)
		mu.Lock()
		asked++
		n := asked
		mu.Unlock()
		if n == 1 {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(529)
			io.WriteString(w, overloadedStatus)
			return
		}
		if c, _, err := w.(http.Hijacker).Hijack(); err == nil {
			c.Close()
		}
	}))
	t.Cleanup(up.Close)
	was := provider.CodexBase
	provider.CodexBase = up.URL + "/backend-api/codex"
	t.Cleanup(func() { provider.CodexBase = was })
	// asked again after 500ms, Codex kept alive past keepHeldAfter's 40ms
	pause := retryPause
	retryPause = 500 * time.Millisecond
	t.Cleanup(func() { retryPause = pause })
	code, body := codexPost(t, oneAccountTurn)
	if code != http.StatusOK || !strings.Contains(body, ": keepalive") || !strings.Contains(body, "event: response.failed") {
		t.Fatalf("not kept alive, then failed in the stream: %d %q", code, body)
	}
	models := streamModels(t, body)
	if len(models) == 0 {
		t.Fatalf("no model named: %q", body)
	}
	for _, m := range models {
		if m != "gpt-5.5" {
			t.Fatalf("model %q, want gpt-5.5: %q", m, body)
		}
	}
}

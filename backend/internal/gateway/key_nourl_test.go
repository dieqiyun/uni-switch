package gateway

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
	"github.com/dieqiyun/uni-switch/backend/internal/testenv"
)

// A key set to Responses on a provider with only a Chat URL (#1702,
// edussss: one key, Responses picked, the address typed in the
// OpenAI-compatible field only) has no endpoint of its own. The request
// still goes out on the Chat URL the provider has, as before, but the
// routing trace marks the provider with the protocol it lacks, so Routing
// says why it went to /v1/chat/completions. A key set to Any beside it is
// used as ever and nothing is marked.
func TestKeyForAProtocolWithNoURLIsSaid(t *testing.T) {
	testenv.SetHome(t, t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	var mu sync.Mutex
	var seen []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		seen = append(seen, r.URL.Path+" "+strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
		mu.Unlock()
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, sse(
			`data: {"id":"c1","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"role":"assistant","content":"ok"}}]}`,
			`data: {"id":"c1","object":"chat.completion.chunk","choices":[{"index":0,"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1}}`,
			`data: [DONE]`))
	}))
	defer srv.Close()
	body := `{"model":"relay/gpt-5.5","stream":false,"input":"hi"}`
	for _, c := range []struct {
		name  string
		keys  []provider.KeyAccount // after the first, k-resp set to Responses
		noURL provider.Protocol
		want  string
	}{
		{"the reporter's: one key, Responses", nil, provider.Responses, "/v1/chat/completions k-resp"},
		{"a key for Any beside it", []provider.KeyAccount{{Key: "k-any"}}, "", "/v1/chat/completions k-any"},
	} {
		p := provider.Provider{ID: "relay", Name: "Relay", Chat: srv.URL + "/v1", Models: []string{"gpt-5.5"},
			Key: "k-resp", KeyProtocol: provider.Responses, Keys: c.keys}
		if err := provider.Save(p); err != nil {
			t.Fatal(err)
		}
		s := New()
		rec := httptest.NewRecorder()
		s.Handler().ServeHTTP(rec, httptest.NewRequest("POST", "/v1/responses", strings.NewReader(body)))
		mu.Lock()
		got := seen
		seen = nil
		mu.Unlock()
		if rec.Code != 200 || len(got) != 1 || got[0] != c.want {
			t.Fatalf("%s: %d %s, upstream saw %v", c.name, rec.Code, rec.Body.String(), got)
		}
		routes := s.Trace(context.Background(), 0, 0).Routes
		if len(routes) != 1 || len(routes[0].Order) != 1 {
			t.Fatalf("%s: routes %+v", c.name, routes)
		}
		if w := routes[0].Order[0]; w.NoURL != c.noURL {
			t.Fatalf("%s: noURL = %q, want %q (%+v)", c.name, w.NoURL, c.noURL, w)
		}
	}
}

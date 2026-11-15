package gateway

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// Aloha on Discord: a relay answering 401 "鉴权 header， x-api-key 和
// Authorization 不可以同时存在" to the two key headers uni-switch sends a
// third-party Anthropic API. The agent's turn goes through: asked again with
// x-api-key alone, the same body, and the provider keeps x-api-key, so the
// next turn is sent one header from the first (Anthropic's own API is
// TestAuthHeaderChoice's, in internal/provider).
func TestOneAuthHeaderRelay(t *testing.T) {
	fresh(t)
	var mu sync.Mutex
	type ask struct{ x, a, model string }
	var asked []ask
	up := &adaptiveVendor{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var b struct {
			Model string `json:"model"`
		}
		raw, _ := io.ReadAll(r.Body)
		json.Unmarshal(raw, &b)
		x, a := r.Header.Get("x-api-key"), r.Header.Get("Authorization")
		mu.Lock()
		asked = append(asked, ask{x, a, b.Model})
		mu.Unlock()
		if x != "" && a != "" {
			w.WriteHeader(http.StatusUnauthorized)
			io.WriteString(w, `{"error":{"type":"authentication_error","message":"鉴权 header， x-api-key 和 Authorization 不可以同时存在"}}`)
			return
		}
		r.Body = io.NopCloser(bytes.NewReader(raw))
		up.ServeHTTP(w, r)
	}))
	t.Cleanup(srv.Close)
	if err := provider.Save(provider.Provider{ID: "relay", Name: "Relay", Key: "k", Anthropic: srv.URL, Models: []string{"claude-sonnet-4-5"}}); err != nil {
		t.Fatal(err)
	}
	code, body := post(t, "/v1/messages", `{"model":"relay/claude-sonnet-4-5","max_tokens":100,"messages":[{"role":"user","content":"hi"}]}`)
	if code != 200 {
		t.Fatalf("status %d: %s", code, body)
	}
	mu.Lock()
	got := append([]ask(nil), asked...)
	mu.Unlock()
	if len(got) != 2 || got[0].a == "" || got[1] != (ask{"k", "", "claude-sonnet-4-5"}) {
		t.Fatalf("asked %+v", got)
	}
	if p, _ := provider.Find("relay"); p.AuthHeader != provider.AuthXAPIKey {
		t.Fatalf("kept %q", p.AuthHeader)
	}
	// the next turn, from Chat Completions too, goes with one header first
	code, body = post(t, "/v1/chat/completions", `{"model":"relay/claude-sonnet-4-5","messages":[{"role":"user","content":"hi"}]}`)
	if code != 200 {
		t.Fatalf("status %d: %s", code, body)
	}
	mu.Lock()
	got = append([]ask(nil), asked[2:]...)
	mu.Unlock()
	if len(got) != 1 || got[0].x != "k" || got[0].a != "" {
		t.Errorf("next turn asked %+v", got)
	}
}

package gateway

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// TestForwardOnceCarriesBiliHeaders holds #3052: a billion-context proxy
// put in front of the provider knows a cooperative plugin's session by the
// x-bili- headers the agent sent, so forwardOnce copies them on to a URL in
// bili's /bili/<upstream> form. A header no family allowlists, and the
// client's own key, still stay off.
func TestForwardOnceCarriesBiliHeaders(t *testing.T) {
	var mu sync.Mutex
	got := http.Header{}
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		got = r.Header.Clone()
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{}`)
	}))
	defer up.Close()

	in := http.Header{}
	in.Set("User-Agent", "pi/0.0.56")
	in.Set("x-bili-plugin", "pi")
	in.Set("x-bili-plugin-conversation", "test-123")
	in.Set("x-bili-plugin-context-window", "200000")
	in.Set("X-Custom-Whatever", "never")
	in.Set("Authorization", "Bearer client-key")

	s := New()
	p := provider.Provider{Responses: up.URL + "/bili/http://127.0.0.1:8317/v1", Key: "provider-key"}
	res, err := s.forwardOnce(context.Background(), p, provider.Responses, "/responses", []byte(`{"model":"gpt-5.5","input":"hi"}`), in)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()

	mu.Lock()
	defer mu.Unlock()
	for _, h := range []struct{ key, want string }{
		{"X-Bili-Plugin", "pi"},
		{"X-Bili-Plugin-Conversation", "test-123"},
		{"X-Bili-Plugin-Context-Window", "200000"},
	} {
		if v := got.Get(h.key); v != h.want {
			t.Errorf("%s = %q, want %q: the bili proxy reads it to know the cooperative session", h.key, v, h.want)
		}
	}
	if v := got.Get("X-Custom-Whatever"); v != "" {
		t.Errorf("X-Custom-Whatever = %q, want none: only allowlisted families go on", v)
	}
	if v := got.Get("Authorization"); v != "Bearer provider-key" {
		t.Errorf("Authorization = %q, want the provider's key, never the client's", v)
	}
}

// TestForwardOnceKeepsBiliHeadersFromOthers: the same headers never go to
// a provider that isn't reached through bili's /bili/ prefix, a vendor or a
// relay, nor to a signed-in account (#3052's review).
func TestForwardOnceKeepsBiliHeadersFromOthers(t *testing.T) {
	var mu sync.Mutex
	var got []http.Header
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		got = append(got, r.Header.Clone())
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{}`)
	}))
	defer up.Close()

	in := http.Header{}
	in.Set("x-bili-plugin", "pi")
	in.Set("x-bili-plugin-conversation", "test-123")

	s := New()
	for _, p := range []provider.Provider{
		{Responses: up.URL + "/v1", Key: "provider-key"},
		{Responses: up.URL + "/v1/bili/x", Key: "provider-key"},
		{Responses: up.URL + "/bili/http://127.0.0.1:8317/v1", Account: &provider.Account{}},
	} {
		res, err := s.forwardOnce(context.Background(), p, provider.Responses, "/responses", []byte(`{"model":"gpt-5.5","input":"hi"}`), in)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
	}
	mu.Lock()
	defer mu.Unlock()
	if len(got) != 3 {
		t.Fatalf("%d requests reached the upstream, want 3", len(got))
	}
	for i, h := range got {
		for k := range h {
			if biliHeader(k) {
				t.Errorf("request %d carried %s; only a /bili/ URL of the user's own provider is sent it", i, k)
			}
		}
	}
}

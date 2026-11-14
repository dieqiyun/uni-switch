package gateway

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// The Kilo Gateway's free models are served with no key: a Kilo provider
// saved without one is asked as the Kilo CLI asks (its attribution and
// editor, the conversation as its task) with no Authorization, one with
// a key sends that key, and no other provider gets Kilo's headers.
func TestKiloClientHeaders(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	free, err := provider.FromPreset("kilo")
	if err != nil {
		t.Fatal(err)
	}
	free.ID, free.Models = "kilo", []string{"qwen/qwen3.8-27b:free"}
	keyed := free
	keyed.ID, keyed.Key, keyed.Chat = "kilo-keyed", "kilo_jwt", "https://api.kilocode.ai/api/openrouter"
	for _, p := range []provider.Provider{free, keyed,
		{ID: "other", Name: "Other", Key: "k", Chat: "https://other.test/v1", Models: []string{"qwen/qwen3.8-27b:free"}},
	} {
		if err := provider.Save(p); err != nil {
			t.Fatal(err)
		}
	}
	got := map[string]*http.Request{}
	s := New()
	s.client = &http.Client{Transport: countTransport(func(r *http.Request) (*http.Response, error) {
		got[r.URL.Host] = r.Clone(r.Context())
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}},
			Body: io.NopCloser(strings.NewReader(`{"id":"c","object":"chat.completion","model":"m","choices":[{"index":0,"message":{"role":"assistant","content":"pong"},"finish_reason":"stop"}]}`))}, nil
	})}
	for _, id := range []string{"kilo", "kilo-keyed", "other"} {
		req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(`{"model":"`+id+`/qwen/qwen3.8-27b:free","messages":[{"role":"user","content":"Say pong"}]}`))
		req.Header.Set("User-Agent", "some-agent/1.0")
		rec := httptest.NewRecorder()
		s.Handler().ServeHTTP(rec, req)
		if rec.Code != 200 {
			t.Fatalf("%s: %d %s", id, rec.Code, rec.Body)
		}
	}
	k, kk, o := got["api.kilo.ai"], got["api.kilocode.ai"], got["other.test"]
	if k == nil || kk == nil || o == nil {
		t.Fatalf("requests: %v", got)
	}
	if k.URL.Path != "/api/openrouter/chat/completions" {
		t.Errorf("path %s", k.URL.Path)
	}
	want := map[string]string{
		"HTTP-Referer": "https://kilocode.ai", "X-Title": "Kilo Code",
		"User-Agent": "Kilo-Code/" + provider.KiloVersion, "X-KILOCODE-EDITORNAME": "Kilo CLI " + provider.KiloVersion,
		"x-kilocode-mode": "code",
	}
	if a := k.Header.Values("Authorization"); len(a) != 0 {
		t.Errorf("keyless kilo sent Authorization %q", a)
	}
	for k2, v := range want {
		if k.Header.Get(k2) != v {
			t.Errorf("kilo: %s %q, want %q", k2, k.Header.Get(k2), v)
		}
	}
	if id := k.Header.Get("X-KILOCODE-TASKID"); !strings.HasPrefix(id, "ses_") || k.Header.Get("X-Session-Id") != id {
		t.Errorf("kilo task %q", id)
	}
	if got := kk.Header.Get("Authorization"); got != "Bearer kilo_jwt" {
		t.Errorf("keyed kilo: Authorization %q", got)
	}
	for _, h := range []string{"X-KILOCODE-EDITORNAME", "X-KILOCODE-TASKID", "X-Title"} {
		if o.Header.Get(h) != "" {
			t.Errorf("other provider got %s", h)
		}
	}
	if o.Header.Get("Authorization") != "Bearer k" {
		t.Errorf("other: %q", o.Header.Get("Authorization"))
	}
}

package gateway

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// Cline's free models (cline-free/…) answer 403 "only available via Cline
// product surfaces" unless the request says it is Cline's: requests to the
// Cline API carry its desktop app's headers, with the user's key, and those
// to anyone else don't (lml and ARNO on Discord).
func TestClineClientHeaders(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	cline, err := provider.FromPreset("clinepass")
	if err != nil {
		t.Fatal(err)
	}
	cline.ID, cline.Key, cline.Models = "cline", "clp_key", []string{"cline-free/deepseek-v4.1-flash"}
	for _, p := range []provider.Provider{cline,
		{ID: "other", Name: "Other", Key: "k", Chat: "https://other.test/v1", Models: []string{"cline-free/deepseek-v4.1-flash"}},
	} {
		if err := provider.Save(p); err != nil {
			t.Fatal(err)
		}
	}
	got := map[string]http.Header{}
	s := New()
	s.client = &http.Client{Transport: countTransport(func(r *http.Request) (*http.Response, error) {
		got[r.URL.Host] = r.Header.Clone()
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}},
			Body: io.NopCloser(strings.NewReader(`{"id":"c","object":"chat.completion","model":"m","choices":[{"index":0,"message":{"role":"assistant","content":"pong"},"finish_reason":"stop"}]}`))}, nil
	})}
	for _, id := range []string{"cline", "other"} {
		req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(`{"model":"`+id+`/cline-free/deepseek-v4.1-flash","messages":[{"role":"user","content":"Say pong"}]}`))
		req.Header.Set("User-Agent", "some-agent/1.0")
		rec := httptest.NewRecorder()
		s.Handler().ServeHTTP(rec, req)
		if rec.Code != 200 {
			t.Fatalf("%s: %d %s", id, rec.Code, rec.Body)
		}
	}
	h, o := got["api.cline.bot"], got["other.test"]
	if h == nil || o == nil {
		t.Fatalf("requests: %v", got)
	}
	want := map[string]string{
		"HTTP-Referer": "https://cline.bot", "X-Title": "Cline", "X-IS-MULTIROOT": "false",
		"X-CLIENT-TYPE": "cline-desktop", "X-CLIENT-VERSION": provider.ClineVersion,
		"User-Agent": "Cline/" + provider.ClineVersion, "Authorization": "Bearer clp_key",
	}
	for k, v := range want {
		if h.Get(k) != v {
			t.Errorf("cline: %s %q, want %q", k, h.Get(k), v)
		}
	}
	for _, k := range []string{"X-CLIENT-TYPE", "X-Title", "X-IS-MULTIROOT"} {
		if o.Get(k) != "" {
			t.Errorf("other provider got %s", k)
		}
	}
}

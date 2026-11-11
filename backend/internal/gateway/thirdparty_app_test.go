package gateway

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
	"github.com/dieqiyun/uni-switch/backend/internal/usage"
)

// What docs/integrating.md promises an app uni-switch has no adapter for (#918):
// /api/hello finds the gateway, and its calls are counted as its own in
// Usage, by the key it sends (uni-switch-<app>) or else by its User-Agent.
func TestThirdPartyAppIsCountedAsItself(t *testing.T) {
	setHome(t, t.TempDir())

	rec := httptest.NewRecorder()
	New().Handler().ServeHTTP(rec, httptest.NewRequest("GET", "/api/hello", nil))
	var hello struct{ Name string }
	if rec.Code != 200 || json.Unmarshal(rec.Body.Bytes(), &hello) != nil || hello.Name != "uni-switch" {
		t.Fatalf("GET /api/hello: %d %s", rec.Code, rec.Body.String())
	}

	f := &fake{t: t, reply: sse(
		`data: {"id":"c1","choices":[{"delta":{"content":"OK"},"finish_reason":"stop"}],"usage":{"prompt_tokens":5,"completion_tokens":2}}`,
		`data: [DONE]`)}
	setup(t, provider.Chat, f)
	body := `{"model":"fake/m1","stream":true,"messages":[{"role":"user","content":"hi"}]}`
	for i, c := range []struct{ key, ua, want string }{
		{TokenFor("my-app"), "node-fetch/1.0", "my-app"},
		{Token, "MyApp/1.2 (darwin)", "MyApp"},
	} {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+c.key)
		req.Header.Set("User-Agent", c.ua)
		New().Handler().ServeHTTP(rec, req)
		if rec.Code != 200 || !strings.Contains(rec.Body.String(), "OK") {
			t.Fatalf("%+v: %d %s", c, rec.Code, rec.Body.String())
		}
		var got []usage.Record
		for range 100 {
			if got = usage.Load(time.Time{}); len(got) > i {
				break
			}
			time.Sleep(20 * time.Millisecond)
		}
		if len(got) <= i || got[i].Agent != c.want {
			t.Fatalf("%+v: records %+v, want the last %s's", c, got, c.want)
		}
	}
}

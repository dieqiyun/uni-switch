package gui

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/usage"
)

// /api/model-speeds says how fast each provider answered a model this
// week, keyed by the ref an agent's model picker gives the row (axuhere on
// X: one model from several providers, at very different speeds). A model
// with no timed reply, and one older than the week, say nothing.
func TestModelSpeedsByRef(t *testing.T) {
	sandboxHome(t)
	now := time.Now()
	for _, r := range []usage.Record{
		// 200 tokens written in 2s after a 400ms first token: 100 tok/s
		{Time: now.Add(-time.Hour), Provider: "quick", Model: "glm-5", Output: 200, Millis: 2400, TTFT: 400, Status: 200},
		{Time: now.Add(-time.Hour), Provider: "quick", Model: "glm-5", Output: 200, Millis: 2600, TTFT: 600, Status: 200},
		// 200 tokens in 10s after 3s: 20 tok/s
		{Time: now.Add(-time.Hour), Provider: "slow", Model: "glm-5", Output: 200, Millis: 13000, TTFT: 3000, Status: 200},
		// not streamed: no first token, no speed
		{Time: now.Add(-time.Hour), Provider: "plain", Model: "glm-5", Output: 200, Millis: 5000, Status: 200},
		// before the week
		{Time: now.AddDate(0, 0, -9), Provider: "old", Model: "glm-5", Output: 200, Millis: 2400, TTFT: 400, Status: 200},
	} {
		usage.Append(r)
	}
	mux := http.NewServeMux()
	speedsAPI(mux)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest("GET", "/api/model-speeds", nil))
	if w.Code != 200 {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	var got map[string]speedJSON
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if q := got["quick/glm-5"]; q.TTFT != 500 || q.TPS < 99 || q.TPS > 101 || q.N != 2 {
		t.Errorf("quick/glm-5 = %+v; want 500ms, 100 tok/s of 2", q)
	}
	if s := got["slow/glm-5"]; s.TTFT != 3000 || s.TPS < 19 || s.TPS > 21 {
		t.Errorf("slow/glm-5 = %+v; want 3000ms, 20 tok/s", s)
	}
	for _, k := range []string{"plain/glm-5", "old/glm-5"} {
		if s, ok := got[k]; ok {
			t.Errorf("%s says %+v; want nothing", k, s)
		}
	}
}

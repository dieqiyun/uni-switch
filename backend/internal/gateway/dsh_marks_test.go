package gateway

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
	"github.com/dieqiyun/uni-switch/backend/internal/usage"
)

// #2505 (h4rk8s): DSH names its session in x-deepseek-harness-session-id
// and marks a request compacting its context with
// x-deepseek-harness-compact. A call is recorded under that session and as
// a compaction ("compact", the Requests list's Compaction), on this uni-switch
// and on a remote one it is passed on to; a request without the mark is
// never taken for one by what its prompt says. The vendor is sent neither
// header.

// a compaction-like prompt, unmarked

// #3298 (tom047784): dsh's own DeepSeek row (llm-deepseek) pointed at uni-switch
// by hand calls /v1/messages, and dsh's pi-ai routes reach /v1/responses
// too. Each route records dsh's session and compaction as Chat Completions
// does above, and the vendor is sent no x-deepseek-harness-* header, the
// user id llm-deepseek always sends included.
func TestDshMarksOnEveryRoute(t *testing.T) {
	var mu sync.Mutex
	var vendor []http.Header
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.Copy(io.Discard, r.Body)
		mu.Lock()
		vendor = append(vendor, r.Header.Clone())
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "/messages"):
			io.WriteString(w, `{"id":"msg_1","type":"message","role":"assistant","model":"m1","content":[{"type":"text","text":"hello"}],"stop_reason":"end_turn","usage":{"input_tokens":3,"output_tokens":1}}`)
		case strings.HasSuffix(r.URL.Path, "/responses"):
			io.WriteString(w, `{"id":"resp_1","object":"response","status":"completed","model":"m1","output":[{"type":"message","id":"m_1","role":"assistant","status":"completed","content":[{"type":"output_text","text":"hello","annotations":[]}]}],"usage":{"input_tokens":3,"output_tokens":1,"total_tokens":4}}`)
		default:
			http.Error(w, "unexpected "+r.URL.Path, 404)
		}
	}))
	t.Cleanup(up.Close)
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	for _, p := range []provider.Provider{
		{ID: "ant", Name: "Ant", Key: "k", Models: []string{"m1"}, Anthropic: up.URL + "/v1"},
		{ID: "resp", Name: "Resp", Key: "k", Models: []string{"m1"}, Responses: up.URL + "/v1"},
	} {
		if err := provider.Save(p); err != nil {
			t.Fatal(err)
		}
	}
	record := func(id string, n int) usage.Record {
		t.Helper()
		for range 200 {
			var rs []usage.Record
			for _, r := range usage.Load(time.Time{}) {
				if r.Provider == id {
					rs = append(rs, r)
				}
			}
			if len(rs) >= n {
				return rs[len(rs)-1]
			}
			time.Sleep(20 * time.Millisecond)
		}
		t.Fatalf("%s: fewer than %d records", id, n)
		return usage.Record{}
	}
	h := New().Handler()
	seen := map[string]int{}
	for _, tc := range []struct {
		path, prov, body, compact, kind string
	}{
		{"/v1/messages", "ant", `{"model":"ant/m1","max_tokens":16,"messages":[{"role":"user","content":"hi"}]}`, "1", "compact"},
		{"/v1/messages", "ant", `{"model":"ant/m1","max_tokens":16,"messages":[{"role":"user","content":"hi"}]}`, "", ""},
		{"/v1/responses", "resp", `{"model":"resp/m1","input":"hi"}`, "1", "compact"},
		{"/v1/responses", "resp", `{"model":"resp/m1","input":"hi"}`, "", ""},
	} {
		req := httptest.NewRequest("POST", tc.path, strings.NewReader(tc.body))
		req.Header.Set("User-Agent", "dsh/0.2.1")
		req.Header.Set("x-api-key", TokenFor("dsh"))
		req.Header.Set("anthropic-version", "2023-06-01")
		req.Header.Set("x-deepseek-harness-user-id", "dsh-user")
		req.Header.Set("x-deepseek-harness-session-id", "dsh-session-B")
		if tc.compact != "" {
			req.Header.Set("x-deepseek-harness-compact", tc.compact)
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != 200 {
			t.Fatalf("%s: %d %s", tc.path, rec.Code, rec.Body.String())
		}
		seen[tc.prov]++
		r := record(tc.prov, seen[tc.prov])
		if r.NativeSession != "dsh-session-B" || r.Kind != tc.kind {
			t.Errorf("%s compact %q: recorded session %q kind %q; want dsh-session-B, %q", tc.path, tc.compact, r.NativeSession, r.Kind, tc.kind)
		}
	}
	mu.Lock()
	defer mu.Unlock()
	if len(vendor) != 4 {
		t.Fatalf("the vendor got %d requests, want 4", len(vendor))
	}
	for _, v := range vendor {
		for k := range v {
			if strings.HasPrefix(strings.ToLower(k), "x-deepseek-harness-") {
				t.Errorf("the vendor was sent %s", k)
			}
		}
	}
}

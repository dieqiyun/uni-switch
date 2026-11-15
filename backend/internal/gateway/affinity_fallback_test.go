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
)

// downThenUp is an Anthropic Messages upstream that refuses with a 429
// while down is set, and otherwise answers as cacheWriter does, writing
// the whole prompt to its cache.
type downThenUp struct {
	cacheWriter
	mu    sync.Mutex
	down  bool
	asked int
}

func (d *downThenUp) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	d.mu.Lock()
	down := d.down
	d.asked++
	d.mu.Unlock()
	if down {
		io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusTooManyRequests)
		io.WriteString(w, `{"type":"error","error":{"type":"rate_limit_error","message":"rate limited"}}`)
		return
	}
	d.cacheWriter.ServeHTTP(w, r)
}

// A provider's fallback is its emergency path: a conversation that went to
// it while the provider was down goes back to the provider at its next
// turn once the provider can serve again, however much the fallback
// cached (01huadalang on Discord: "主模型恢复后备用模型还在走缓存逻辑").
// Within a turn it stays on the fallback, whose sealed reasoning the turn
// runs on. A provider still resting keeps the conversation on the
// fallback, cached there.
func TestFallbackGivesTheConversationBackOnceTheMainIsReady(t *testing.T) {
	const (
		first = `{"model":"main/m","messages":[{"role":"user","content":"hi"}]}`
		again = `{"model":"main/m","messages":[{"role":"user","content":"hi"},{"role":"assistant","content":"yo"},{"role":"user","content":"more"}]}`
		tool  = `{"model":"main/m","messages":[{"role":"user","content":"hi"},{"role":"assistant","content":null,"tool_calls":[{"id":"c1","type":"function","function":{"name":"ls","arguments":"{}"}}]},{"role":"tool","tool_call_id":"c1","content":"a.go"}]}`
	)
	for _, c := range []struct {
		name     string
		next     string
		mainBack bool
		mode     string
		want     string // who answers the next request
		why      string
	}{
		{"next turn, main ready", again, true, "", "main", "main"},
		{"next turn, main ready, session", again, true, provider.AffinitySession, "main", "main"},
		{"within the turn, main ready", tool, true, "", "spare", "turn"},
		{"next turn, main still resting", again, false, "", "spare", "cache"},
	} {
		t.Run(c.name, func(t *testing.T) {
			fresh(t)
			main := &downThenUp{cacheWriter: cacheWriter{wrote: 3000}, down: true}
			spare := &downThenUp{cacheWriter: cacheWriter{wrote: 3000}}
			mu, su := httptest.NewServer(main), httptest.NewServer(spare)
			t.Cleanup(mu.Close)
			t.Cleanup(su.Close)
			for _, p := range []provider.Provider{
				{ID: "main", Name: "Main", Key: "main-key", Models: []string{"m"}, Anthropic: mu.URL + "/v1", Fallback: []string{"spare/s"}, Affinity: c.mode},
				{ID: "spare", Name: "Spare", Key: "spare-key", Models: []string{"s"}, Anthropic: su.URL + "/v1"},
			} {
				if err := provider.Save(p); err != nil {
					t.Fatal(err)
				}
			}
			s := New()
			if code, body := postAs(t, s, "s1", first); code != 200 || !strings.Contains(body, "from spare-key") {
				t.Fatalf("first turn %d %s, want the fallback's answer", code, body)
			}
			if c.mainBack {
				// its rest over, as when the minute or the reset passes
				main.mu.Lock()
				main.down = false
				main.mu.Unlock()
				restingUntil.Lock()
				for k := range restingUntil.m {
					restingUntil.m[k] = time.Now().Add(-time.Second)
				}
				restingUntil.Unlock()
			}
			code, body := postAs(t, s, "s1", c.next)
			if code != 200 || !strings.Contains(body, "from "+c.want+"-key") {
				t.Fatalf("next request %d %s, want %s's answer", code, body, c.want)
			}
			a := s.trace.routes[len(s.trace.routes)-1].Affinity
			if a == nil || a.Why != c.why {
				t.Fatalf("affinity %+v, want why %q", a, c.why)
			}
		})
	}
}

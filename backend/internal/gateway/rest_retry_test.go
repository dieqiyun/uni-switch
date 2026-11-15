package gateway

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// onceRefused is an upstream that refuses each key once, with status,
// header and body, and serves it after: a local aggregator switching
// upstreams (zoecats on Discord), or a relay's moment of "quota
// exhausted".
type onceRefused struct {
	status int
	header http.Header
	body   string

	mu    sync.Mutex
	asked []string
	seen  map[string]bool
}

func (o *onceRefused) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	io.Copy(io.Discard, r.Body)
	key := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	o.mu.Lock()
	o.asked = append(o.asked, key)
	first := !o.seen[key]
	o.seen[key] = true
	o.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	if first {
		for k, v := range o.header {
			w.Header()[k] = v
		}
		w.WriteHeader(o.status)
		io.WriteString(w, o.body)
		return
	}
	io.WriteString(w, `{"id":"c1","object":"chat.completion","model":"m1","choices":[{"index":0,"message":{"role":"assistant","content":"hi"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`)
}

func (o *onceRefused) took() []string {
	o.mu.Lock()
	defer o.mu.Unlock()
	out := o.asked
	o.asked = nil
	return out
}

func onceChain(t *testing.T, o *onceRefused, keys ...string) *Server {
	t.Helper()
	noRests()
	t.Cleanup(noRests)
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	o.seen = map[string]bool{}
	up := httptest.NewServer(o)
	t.Cleanup(up.Close)
	p := provider.Provider{ID: "agg", Name: "Agg", Key: keys[0], Models: []string{"m1"}, Chat: up.URL + "/v1", Routing: provider.Ordered}
	for _, k := range keys[1:] {
		p.Keys = append(p.Keys, provider.KeyAccount{Key: k})
	}
	if err := provider.Save(p); err != nil {
		t.Fatal(err)
	}
	return New()
}

func postAgg(t *testing.T, srv *Server) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(`{"model":"agg/m1","messages":[{"role":"user","content":"hi"}]}`))
	req.Header.Set("Content-Type", "application/json")
	srv.Handler().ServeHTTP(rec, req)
	return rec
}

// zoecats on Discord (#feedback, 10-25), by default, with no switch
// turned: a provider's key refused once out of its quota or credit, and
// served the moment after. The agent's retry, and every request after
// it, reach the upstream and are answered; uni-switch doesn't answer them
// from the rest, "out of its allowance: not asked again before …", for
// the half hour it guessed. Each refusal a vendor gives for a limit: its
// "insufficient_quota" (read as out of credit), a usage limit that names
// no reset, the same with a Retry-After of 0, and every key of the
// provider refused at once.
func TestRetryAfterALimitIsAsked(t *testing.T) {
	cases := []struct {
		name   string
		status int
		header http.Header
		body   string
		keys   []string
	}{
		{"insufficient_quota", 429, nil, `{"error":{"message":"quota exhausted","type":"insufficient_quota"}}`, []string{"k1"}},
		{"usage limit", 429, nil, `{"error":{"message":"you have reached your usage limit","type":"rate_limit_error"}}`, []string{"k1"}},
		{"retry-after 0", 429, http.Header{"Retry-After": {"0"}}, `{"error":{"message":"you have reached your usage limit","type":"rate_limit_error"}}`, []string{"k1"}},
		{"402", 402, nil, `{"error":{"message":"insufficient balance"}}`, []string{"k1"}},
		{"every key", 429, nil, `{"error":{"message":"quota exhausted","type":"insufficient_quota"}}`, []string{"k1", "k2"}},
		// a rate limit was asked again before too (restLast, the retries
		// within the request): held to that
		{"rate limited, retry-after 0", 429, http.Header{"Retry-After": {"0"}}, `{"error":{"message":"Too many requests","type":"rate_limit_error"}}`, []string{"k1"}},
		{"rate limited, every key", 429, http.Header{"Retry-After": {"1"}}, `{"error":{"message":"Too many requests","type":"rate_limit_error"}}`, []string{"k1", "k2"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			o := &onceRefused{status: tc.status, header: tc.header, body: tc.body}
			srv := onceChain(t, o, tc.keys...)
			// a rate limit is asked again within the request, and answered
			if rec := postAgg(t, srv); rec.Code != tc.status && rec.Code != http.StatusOK {
				t.Fatalf("first: %d %s", rec.Code, rec.Body)
			}
			if got := o.took(); len(got) < len(tc.keys) {
				t.Fatalf("first request asked %v", got)
			}
			for i := 0; i < 2; i++ {
				rec := postAgg(t, srv)
				got := o.took()
				if rec.Code != http.StatusOK || len(got) == 0 {
					t.Fatalf("request %d after the refusal: %d %s, upstream asked %v", i+2, rec.Code, rec.Body, got)
				}
			}
		})
	}
}

// Refused twice in a row, every key is held as #1772 wants, not asked
// on every request; but a rest whose length uni-switch guessed (the half
// hour out of credit, the quarter out of quota with no word of when) is
// asked again once a minute, and its Retry-After says so, so an agent
// that waits as told is answered within the minute the upstream is back.
// A time the vendor named still holds it to then.
func TestHeldLimitAskedAgainInAMinute(t *testing.T) {
	d, srv := deadChain(t, "dead-a")
	for i := 0; i < 2; i++ {
		if rec := postDead(t, srv); rec.Code != 429 {
			t.Fatalf("refusal %d: %d %s", i+1, rec.Code, rec.Body)
		}
	}
	if got := d.took(); len(got) != 2 {
		t.Fatalf("two refusals asked %v", got)
	}
	rec := postDead(t, srv)
	if got := d.took(); rec.Code != 429 || len(got) != 0 {
		t.Fatalf("held: %d, asked %v", rec.Code, got)
	}
	ra, err := strconv.Atoi(rec.Header().Get("Retry-After"))
	if err != nil || ra < 50 || ra > 61 {
		t.Errorf("held: Retry-After %q, want about a minute", rec.Header().Get("Retry-After"))
	}
	// a minute on: one more ask
	restingUntil.Lock()
	for k, r := range restingUntil.note {
		r.askAt = time.Now().Add(-time.Second)
		restingUntil.note[k] = r
	}
	restingUntil.Unlock()
	if rec := postDead(t, srv); rec.Code != 429 {
		t.Fatalf("a minute on: %d %s", rec.Code, rec.Body)
	}
	if got := d.took(); len(got) != 1 {
		t.Fatalf("a minute on asked %v, want once", got)
	}
	if rec := postDead(t, srv); rec.Code != 429 || len(d.took()) != 0 {
		t.Fatalf("refused again a minute on: held no more")
	}
}

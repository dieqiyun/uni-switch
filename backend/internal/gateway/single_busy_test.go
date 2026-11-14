package gateway

import (
	"bufio"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// busyUpstream answers each request with the next of its replies, the
// last one again once they run out, and counts what it was asked.
type busyUpstream struct {
	mu      sync.Mutex
	replies []busyReply
	n       int
}

type busyReply struct {
	code       int
	retryAfter string
	body       string
}

func (u *busyUpstream) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	io.ReadAll(r.Body)
	u.mu.Lock()
	x := u.replies[min(u.n, len(u.replies)-1)]
	u.n++
	u.mu.Unlock()
	if x.retryAfter != "" {
		w.Header().Set("Retry-After", x.retryAfter)
	}
	if !strings.HasPrefix(x.body, "{") {
		w.Header().Set("Content-Type", "text/event-stream")
	} else {
		w.Header().Set("Content-Type", "application/json")
	}
	w.WriteHeader(x.code)
	io.WriteString(w, x.body)
}

func (u *busyUpstream) asked() int {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.n
}

// Anthropic's own rate limit reply.
const rateLimited = `{"type":"error","error":{"type":"rate_limit_error","message":"Rate limited, please retry shortly"}}`

// busyProvider saves provider a, served by u, alone: no group.
func busyProvider(t *testing.T, u *busyUpstream) *Server {
	t.Helper()
	return busyProviderWaiting(t, u, 0)
}

// busyProviderWaiting is busyProvider with the Patience the user set.
func busyProviderWaiting(t *testing.T, u *busyUpstream, patience int) *Server {
	t.Helper()
	fresh(t)
	up := httptest.NewServer(u)
	t.Cleanup(up.Close)
	if err := provider.Save(provider.Provider{ID: "a", Name: "A", Key: "k", Models: []string{"m"}, Anthropic: up.URL, Patience: patience}); err != nil {
		t.Fatal(err)
	}
	return New()
}

func singleAsk(stream bool) string {
	s := "false"
	if stream {
		s = "true"
	}
	return `{"model":"a/m","max_tokens":10,"stream":` + s + `,"messages":[{"role":"user","content":"hi"}]}`
}

// scaled shrinks the waits the test goes through, keeping their order:
// a Retry-After past the same-one retries' longestPause (8s), the pause
// past keepHeldAfter (3s), all within singlePatience (60s).
func scaled(t *testing.T, patience time.Duration) {
	t.Helper()
	lp, sp, kh, we, rp := longestPause, singlePatience, keepHeldAfter, watchEvery, retryPause
	longestPause, singlePatience, keepHeldAfter, watchEvery, retryPause = 50*time.Millisecond, patience, 20*time.Millisecond, 2*time.Millisecond, 10*time.Millisecond
	t.Cleanup(func() {
		longestPause, singlePatience, keepHeldAfter, watchEvery, retryPause = lp, sp, kh, we, rp
	})
}

// A request to one provider, not a group, whose vendor answers a short
// rate limit — a Retry-After past what the same-one retries wait for —
// is held and asked again once it has passed, as a group's members are
// (zoecats on Discord: the 429 went straight to the agent). A streaming
// agent is kept alive through the pause once it passes keepHeldAfter.
func TestSingleProviderWaitsOutAShortRateLimit(t *testing.T) {
	for _, stream := range []bool{true, false} {
		name := "whole"
		if stream {
			name = "stream"
		}
		t.Run(name, func(t *testing.T) {
			answer := anthropicAnswer
			if !stream {
				answer = `{"id":"m2","type":"message","role":"assistant","model":"m","content":[{"type":"text","text":"from b"}],"stop_reason":"end_turn","usage":{"input_tokens":3,"output_tokens":4}}`
			}
			u := &busyUpstream{replies: []busyReply{
				{code: 429, retryAfter: "0.4", body: rateLimited},
				{code: 200, body: answer},
			}}
			s := busyProvider(t, u)
			scaled(t, 3*time.Second)
			gw := httptest.NewServer(s.Handler())
			t.Cleanup(gw.Close)
			res, err := http.Post(gw.URL+"/v1/messages", "application/json", strings.NewReader(singleAsk(stream)))
			if err != nil {
				t.Fatal(err)
			}
			defer res.Body.Close()
			var body strings.Builder
			rd := bufio.NewReader(res.Body)
			for {
				line, err := rd.ReadString('\n')
				body.WriteString(line)
				if err != nil {
					break
				}
			}
			out := body.String()
			if res.StatusCode != 200 || !strings.Contains(out, "from b") {
				t.Fatalf("agent got %d %q, want the answer after the pause", res.StatusCode, out)
			}
			if n := u.asked(); n != 2 {
				t.Errorf("upstream asked %d times, want 2", n)
			}
			alive := strings.Index(out, ": keepalive")
			if stream && (alive < 0 || alive > strings.Index(out, "from b")) {
				t.Errorf("no keepalive before the answer: %q", out)
			}
			r := lastRoute(s)
			if len(r.Tries) != 2 || r.Tries[0].Again < 300 || r.Tries[0].Replan != 0 || r.Tries[0].Patience != 0 || r.Status != 200 {
				t.Errorf("tries %+v: want the first asked again after its Retry-After, told as the last one left", r.Tries)
			}
		})
	}
}

// A rate limit that lasts past the patience, or that is the plan's quota,
// goes to the agent as the vendor said it, with nobody asked again: the
// route is watched, not a duration, so a broken budget fails at once.
func TestSingleProviderPassesALongLimitThrough(t *testing.T) {
	for _, c := range []struct {
		name string
		r    busyReply
	}{
		{"an hour", busyReply{code: 429, retryAfter: "3600", body: rateLimited}},
		{"quota", busyReply{code: 429, body: `{"type":"error","error":{"type":"rate_limit_error","message":"You exceeded your current quota, please check your plan and billing details"}}`}},
		{"usage limit back in hours", busyReply{code: 429, body: `{"type":"error","error":{"type":"rate_limit_error","message":"Usage has exceeded the limit. Your usage limit for the original model will reset in 5 hours."}}`}},
		{"key refused", busyReply{code: 401, body: `{"type":"error","error":{"type":"authentication_error","message":"invalid x-api-key"}}`}},
	} {
		t.Run(c.name, func(t *testing.T) {
			u := &busyUpstream{replies: []busyReply{c.r, {code: 200, body: anthropicAnswer}}}
			s := busyProvider(t, u) // the real defaults
			gw := httptest.NewServer(s.Handler())
			t.Cleanup(gw.Close)
			type got struct {
				code  int
				after string
				body  string
			}
			// a failure below ends the request with the test, not when a
			// pause it shouldn't have taken would
			ctx, cancel := context.WithCancel(context.Background())
			t.Cleanup(cancel)
			done := make(chan got, 1)
			go func() {
				req, _ := http.NewRequestWithContext(ctx, "POST", gw.URL+"/v1/messages", strings.NewReader(singleAsk(true)))
				req.Header.Set("Content-Type", "application/json")
				res, err := http.DefaultClient.Do(req)
				if err != nil {
					done <- got{}
					return
				}
				b, _ := io.ReadAll(res.Body)
				res.Body.Close()
				done <- got{res.StatusCode, res.Header.Get("Retry-After"), string(b)}
			}()
			var g got
		wait:
			for {
				select {
				case g = <-done:
					break wait
				case <-time.After(5 * time.Millisecond):
				}
				s.trace.mu.Lock()
				waiting := false
				if k := len(s.trace.routes); k > 0 {
					for _, tr := range s.trace.routes[k-1].Tries {
						waiting = waiting || tr.Again > 0
					}
				}
				s.trace.mu.Unlock()
				if waiting {
					t.Fatalf("uni-switch waits to ask again: %+v", lastRoute(s).Tries)
				}
			}
			if g.code != c.r.code || u.asked() != 1 {
				t.Fatalf("agent got %d %q, upstream asked %d times; want the vendor's %d, asked once", g.code, g.body, u.asked(), c.r.code)
			}
			if c.r.retryAfter != "" && g.after == "" {
				t.Errorf("the vendor's Retry-After was dropped")
			}
		})
	}
}

// An agent gone while uni-switch waits out its one provider's rate limit ends
// the request there: the route is over as canceled, its try says the agent
// left in the pause, and the vendor is asked nothing more.
func TestSingleProviderAgentGoneInTheWait(t *testing.T) {
	u := &busyUpstream{replies: []busyReply{
		{code: 429, retryAfter: "120", body: rateLimited},
		{code: 200, body: anthropicAnswer},
	}}
	s := busyProvider(t, u)
	scaled(t, 5*time.Minute) // a pause of 120s, far longer than the test
	gw := httptest.NewServer(s.Handler())
	t.Cleanup(gw.Close)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "POST", gw.URL+"/v1/messages", strings.NewReader(singleAsk(true)))
	req.Header.Set("Content-Type", "application/json")
	ended := make(chan struct{})
	go func() {
		defer close(ended)
		if res, err := http.DefaultClient.Do(req); err == nil {
			io.Copy(io.Discard, res.Body)
			res.Body.Close()
		}
	}()
	if !inPause(s, 1, 30*time.Second) {
		t.Fatalf("uni-switch never paused to ask again: %+v", lastRoute(s).Tries)
	}
	cancel()
	<-ended
	var r Route
	for deadline := time.Now().Add(60 * time.Second); time.Now().Before(deadline); time.Sleep(5 * time.Millisecond) {
		s.trace.mu.Lock()
		r = *s.trace.routes[len(s.trace.routes)-1]
		r.Tries = append([]Try{}, r.Tries...)
		s.trace.mu.Unlock()
		if r.Done {
			break
		}
	}
	if !r.Done || r.Status != 499 {
		t.Fatalf("route done %v status %d, want done 499 long before the pause ends", r.Done, r.Status)
	}
	if last := r.Tries[len(r.Tries)-1]; !last.Gone || last.Again == 0 {
		t.Errorf("last try %+v: want its pause and that the agent left in it", last)
	}
	if n := u.asked(); n != 1 {
		t.Errorf("upstream asked %d times after the agent left, want 1", n)
	}
}

// The Patience the user set on the provider (uni-switch provider set a
// patience=…) stands over singlePatience either way: off hands the agent a
// rate limit the default would wait out, a shorter one hands it one the
// default would wait for, and a longer one waits out one the default
// wouldn't.
func TestSingleProviderOwnPatienceWins(t *testing.T) {
	for _, c := range []struct {
		name       string
		patience   int           // the user's, in seconds
		byDefault  time.Duration // singlePatience, scaled
		retryAfter string
		want       int
	}{
		{"off", provider.PatienceOff, 3 * time.Second, "0.4", 429},
		{"shorter", 1, 5 * time.Minute, "3", 429},
		{"longer", 120, 100 * time.Millisecond, "0.4", 200},
	} {
		t.Run(c.name, func(t *testing.T) {
			u := &busyUpstream{replies: []busyReply{
				{code: 429, retryAfter: c.retryAfter, body: rateLimited},
				{code: 200, body: anthropicAnswer},
			}}
			s := busyProviderWaiting(t, u, c.patience)
			scaled(t, c.byDefault)
			if c.want != 200 {
				// the 429 is the agent's status, however long a busy
				// machine takes to have it: past a scaled keepHeldAfter
				// the stream's 200 went first, and the 429 in it
				keepHeldAfter = time.Minute
			}
			gw := httptest.NewServer(s.Handler())
			t.Cleanup(gw.Close)
			// a failure ends the request with the test, not when a pause
			// it shouldn't have taken would
			ctx, cancel := context.WithCancel(context.Background())
			t.Cleanup(cancel)
			req, _ := http.NewRequestWithContext(ctx, "POST", gw.URL+"/v1/messages", strings.NewReader(singleAsk(true)))
			req.Header.Set("Content-Type", "application/json")
			done := make(chan int, 1)
			go func() {
				res, err := http.DefaultClient.Do(req)
				if err != nil {
					done <- 0
					return
				}
				io.Copy(io.Discard, res.Body)
				res.Body.Close()
				done <- res.StatusCode
			}()
			var code int
		wait:
			for {
				select {
				case code = <-done:
					break wait
				case <-time.After(5 * time.Millisecond):
				}
				if c.want != 200 {
					s.trace.mu.Lock()
					waiting := false
					if k := len(s.trace.routes); k > 0 {
						for _, tr := range s.trace.routes[k-1].Tries {
							waiting = waiting || tr.Again > 0
						}
					}
					s.trace.mu.Unlock()
					if waiting {
						t.Fatalf("uni-switch waits to ask again past the user's patience: %+v", lastRoute(s).Tries)
					}
				}
			}
			wantAsked := 1
			if c.want == 200 {
				wantAsked = 2
			}
			if code != c.want || u.asked() != wantAsked {
				t.Fatalf("agent got %d, upstream asked %d times; want %d, asked %d", code, u.asked(), c.want, wantAsked)
			}
		})
	}
}

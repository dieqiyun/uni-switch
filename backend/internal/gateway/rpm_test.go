package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// minuteVendor answers chat completions, Messages and count_tokens, noting
// when each request came; fail is how many chat completions it answers
// 503 first.
type minuteVendor struct {
	mu   sync.Mutex
	at   []time.Time
	path []string
	fail int
	// late is how much later than the rest the first request arrives, as
	// when its connection has to be dialed (and, to a real vendor, its TLS
	// handshake made) while the later ones reuse it
	late time.Duration
	// clock, when set, is MaxRPM's held clock (holdMinute): the vendor
	// notes each request's time on it, and a late first moves it on by
	// late rather than sleeping
	clock *minuteClock
	n     int
}

func (v *minuteVendor) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	io.Copy(io.Discard, r.Body)
	v.mu.Lock()
	v.n++
	if v.n == 1 && v.late > 0 {
		if v.clock != nil {
			v.clock.advance(v.late)
		} else {
			v.mu.Unlock()
			time.Sleep(v.late)
			v.mu.Lock()
		}
	}
	if v.clock != nil {
		v.at = append(v.at, v.clock.Now())
	} else {
		v.at = append(v.at, time.Now())
	}
	v.path = append(v.path, r.URL.Path)
	failing := v.fail > 0 && strings.HasSuffix(r.URL.Path, "/chat/completions")
	if failing {
		v.fail--
	}
	v.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	switch {
	case failing:
		w.WriteHeader(503)
		io.WriteString(w, `{"error":{"message":"busy a moment"}}`)
	case strings.HasSuffix(r.URL.Path, "/count_tokens"):
		io.WriteString(w, `{"input_tokens":7}`)
	case strings.HasSuffix(r.URL.Path, "/messages"):
		io.WriteString(w, `{"id":"msg_1","type":"message","role":"assistant","model":"m","content":[{"type":"text","text":"ok"}],"stop_reason":"end_turn","usage":{"input_tokens":3,"output_tokens":1}}`)
	default:
		io.WriteString(w, `{"id":"x","object":"chat.completion","model":"m","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":3,"completion_tokens":1,"total_tokens":4}}`)
	}
}

func (v *minuteVendor) times() []time.Time {
	v.mu.Lock()
	defer v.mu.Unlock()
	return append([]time.Time(nil), v.at...)
}

// shortMinute makes MaxRPM's window and longest wait w and longest for the
// test, with no slack unless the test gives it one (slackMinute).
func shortMinute(t *testing.T, w, longest time.Duration) {
	t.Helper()
	ow, ol, os := rpmWindow, rpmLongest, rpmSlack
	rpmWindow, rpmLongest, rpmSlack = w, longest, 0
	t.Cleanup(func() { rpmWindow, rpmLongest, rpmSlack = ow, ol, os })
}

// slackMinute is shortMinute with a slack of a sixth of the window, as the
// real one is to its minute, for a test that times what the vendor sees.
func slackMinute(t *testing.T, w, longest time.Duration) {
	t.Helper()
	shortMinute(t, w, longest)
	rpmSlack = w / 6
}

// minuteClock is MaxRPM's clock and timer held still: it moves only when the
// test moves it. A test of the minute on the wall clock went red on a busy
// machine whenever the machine was slower than the checks allowed: a second
// request that came late in a 1.5s window was told Retry-After 1, or found
// the window passed (#1791). On the held clock, what the limiter decides
// rests on the minute's arithmetic alone, and a wait for room is a state
// the test sees (waiting) rather than a time it measures.
type minuteClock struct {
	mu    sync.Mutex
	now   time.Time
	waits []*minuteWait
}

type minuteWait struct {
	at time.Time
	c  chan time.Time
}

// holdMinute holds MaxRPM's clock for the test, before its gateway starts.
// A minuteVendor given the clock lets every wait go before the gateway
// closes (rpmGateway), so a request a failed test left waiting ends.
func holdMinute(t *testing.T) *minuteClock {
	t.Helper()
	c := &minuteClock{now: time.Now()}
	on, ot := rpmNow, rpmTimer
	rpmNow, rpmTimer = c.Now, c.timer
	t.Cleanup(func() { rpmNow, rpmTimer = on, ot })
	return c
}

func (c *minuteClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *minuteClock) timer(d time.Duration) (<-chan time.Time, func() bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	w := &minuteWait{at: c.now.Add(d), c: make(chan time.Time, 1)}
	if !w.at.After(c.now) {
		w.c <- c.now
		return w.c, func() bool { return false }
	}
	c.waits = append(c.waits, w)
	return w.c, func() bool {
		c.mu.Lock()
		defer c.mu.Unlock()
		i := slices.Index(c.waits, w)
		if i < 0 {
			return false
		}
		c.waits = slices.Delete(c.waits, i, i+1)
		return true
	}
}

// advance moves the clock on by d, letting go the waits it reaches.
func (c *minuteClock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
	c.waits = slices.DeleteFunc(c.waits, func(w *minuteWait) bool {
		if w.at.After(c.now) {
			return false
		}
		w.c <- c.now
		return true
	})
}

// waiting is when each request waiting for room in the minute goes.
func (c *minuteClock) waiting() []time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	var at []time.Time
	for _, w := range c.waits {
		at = append(at, w.at)
	}
	return at
}

// release lets every wait go.
func (c *minuteClock) release() {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, w := range c.waits {
		w.c <- w.at
	}
	c.waits = nil
}

// rpmReply is a request's answer, or the error that ended it.
type rpmReply struct {
	code   int
	header http.Header
	body   string
	err    error
}

// askRPM sends body to the gateway's path and answers on the channel once
// the request ends.
func askRPM(ctx context.Context, gw, path, body string) <-chan rpmReply {
	out := make(chan rpmReply, 1)
	go func() {
		req, _ := http.NewRequestWithContext(ctx, "POST", gw+path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			out <- rpmReply{err: err}
			return
		}
		b, _ := io.ReadAll(res.Body)
		res.Body.Close()
		out <- rpmReply{code: res.StatusCode, header: res.Header, body: string(b)}
	}()
	return out
}

const rlPath, rlBody = "/v1/chat/completions", `{"model":"rl/m","messages":[{"role":"user","content":"hi"}]}`

// rpmBound bounds how long a test waits for a request on a busy machine to
// get where it is going: never a check of how fast it was, only so a test
// gone wrong ends.
const rpmBound = 2 * time.Minute

// atOnce is r's answer, failing the test if its request starts a wait for
// room in the minute instead: on the held clock such a wait never ends on
// its own, so the request either answers without one or is seen waiting.
func (c *minuteClock) atOnce(t *testing.T, what string, r <-chan rpmReply) rpmReply {
	t.Helper()
	for end := time.Now().Add(rpmBound); time.Now().Before(end); {
		select {
		case got := <-r:
			return got
		case <-time.After(5 * time.Millisecond):
		}
		if w := c.waiting(); len(w) > 0 {
			c.release()
			t.Fatalf("%s waited for room in the minute, to go %s after now", what, w[0].Sub(c.Now()))
		}
	}
	t.Fatalf("%s didn't end in %s", what, rpmBound)
	return rpmReply{}
}

// waitsUntil is when the one request waiting for room in the minute goes,
// once it is waiting.
func (c *minuteClock) waitsUntil(t *testing.T, what string) time.Time {
	t.Helper()
	var w []time.Time
	rpmEventually(t, what+" waiting for room in the minute", func() bool { w = c.waiting(); return len(w) > 0 })
	if len(w) != 1 {
		t.Fatalf("%d waiting for room in the minute, want %s alone", len(w), what)
	}
	return w[0]
}

// rpmAnswered is r's answer, once its request ends.
func rpmAnswered(t *testing.T, what string, r <-chan rpmReply) rpmReply {
	t.Helper()
	select {
	case got := <-r:
		return got
	case <-time.After(rpmBound):
		t.Fatalf("%s didn't end in %s", what, rpmBound)
	}
	return rpmReply{}
}

// rpmEventually waits for cond, as within does, with room for a busy machine.
func rpmEventually(t *testing.T, what string, cond func() bool) {
	t.Helper()
	for end := time.Now().Add(rpmBound); time.Now().Before(end); time.Sleep(5 * time.Millisecond) {
		if cond() {
			return
		}
	}
	t.Fatalf("timed out waiting for %s", what)
}

// rpmGateway is a gateway as the real server serves it (lanGuard in
// front), with a provider "rl" of one key whose MaxRPM is limit.
func rpmGateway(t *testing.T, v *minuteVendor, limit int) (*Server, string) {
	t.Helper()
	fresh(t)
	up := httptest.NewServer(v)
	t.Cleanup(up.Close)
	if err := provider.Save(provider.Provider{ID: "rl", Name: "Limited", Key: "k", Models: []string{"m"}, Chat: up.URL + "/v1", Anthropic: up.URL, MaxRPM: limit}); err != nil {
		t.Fatal(err)
	}
	s := New()
	gw := httptest.NewServer(localGuard(s.Handler()))
	t.Cleanup(gw.Close)
	if v.clock != nil {
		// before the gateway closes, which waits for its requests
		t.Cleanup(v.clock.release)
	}
	return s, gw.URL
}

// coeo91 on Discord: OpenRouter's free models take 20 requests a minute,
// which a limit at once of 1 didn't keep to. With a MaxRPM of 2, through
// the real server, two requests go at once and the third waits until the
// first is a minute (and the slack) old, then is answered; the vendor,
// whose first request came late by its dial, still sees the third a whole
// window after the first.
func TestMaxRPMWaitsForRoomInTheMinute(t *testing.T) {
	window := 1500 * time.Millisecond
	slackMinute(t, window, time.Minute)
	// the first dials the connection the others reuse
	v := &minuteVendor{late: 150 * time.Millisecond, clock: holdMinute(t)}
	_, gw := rpmGateway(t, v, 2)
	clk := v.clock
	t0 := clk.Now()
	for i := range 2 {
		what := fmt.Sprintf("request %d, with room in the minute,", i+1)
		if r := clk.atOnce(t, what, askRPM(context.Background(), gw, rlPath, rlBody)); r.err != nil || r.code != 200 || !strings.Contains(r.body, `"ok"`) {
			t.Fatalf("request %d: %v %d %s", i+1, r.err, r.code, r.body)
		}
	}
	third := askRPM(context.Background(), gw, rlPath, rlBody)
	until := clk.waitsUntil(t, "the third")
	if want := t0.Add(window + rpmSlack); !until.Equal(want) {
		t.Fatalf("the third waits to go %s after the first, want the window and slack, %s", until.Sub(t0), want.Sub(t0))
	}
	if n := len(v.times()); n != 2 {
		t.Fatalf("the vendor saw %d while the third waits, want 2", n)
	}
	clk.advance(until.Sub(clk.Now()))
	if r := rpmAnswered(t, "the third", third); r.err != nil || r.code != 200 || !strings.Contains(r.body, `"ok"`) {
		t.Fatalf("request 3: %v %d %s", r.err, r.code, r.body)
	}
	at := v.times()
	if len(at) != 3 {
		t.Fatalf("the vendor saw %d requests, want 3", len(at))
	}
	if d := at[2].Sub(at[0]); d < window {
		t.Fatalf("the vendor saw the third %s after the first, inside the %s window", d, window)
	}
}

// One that would wait longer than a request waits is turned away at once
// with a 429 whose Retry-After says when the minute has room, and never
// reaches the vendor; nobody rests for it, and once the minute has room
// the next is answered.
func TestMaxRPMTooLongIs429WithRetryAfter(t *testing.T) {
	window := 1500 * time.Millisecond
	shortMinute(t, window, 200*time.Millisecond)
	v := &minuteVendor{clock: holdMinute(t)}
	_, gw := rpmGateway(t, v, 1)
	clk := v.clock
	if r := clk.atOnce(t, "the first", askRPM(context.Background(), gw, rlPath, rlBody)); r.err != nil || r.code != 200 {
		t.Fatalf("first: %v %d %s", r.err, r.code, r.body)
	}
	// the clock held where the first went, the minute has room again in
	// the whole window, 1.5s: Retry-After 2
	r := clk.atOnce(t, "the second, to be turned away,", askRPM(context.Background(), gw, rlPath, rlBody))
	if r.err != nil {
		t.Fatal(r.err)
	}
	if r.code != 429 || r.header.Get("Retry-After") != "2" || !strings.Contains(r.body, "1 requests a minute is its limit") {
		t.Fatalf("second = %d Retry-After %q %s, want a 429 saying the limit, Retry-After 2", r.code, r.header.Get("Retry-After"), r.body)
	}
	if n := len(v.times()); n != 1 {
		t.Fatalf("the vendor saw %d, want 1", n)
	}
	if r := restingNow(t); len(r) != 0 {
		t.Fatalf("%v rest for a limit of the user's", r)
	}
	clk.advance(window)
	if r := clk.atOnce(t, "the next, after the minute,", askRPM(context.Background(), gw, rlPath, rlBody)); r.err != nil || r.code != 200 {
		t.Fatalf("after the minute: %v %d %s", r.err, r.code, r.body)
	}
}

// An agent that goes away while its request waits for room ends the wait:
// nothing is sent, and its place in the minute is given back.
func TestMaxRPMAgentGoneWhileWaiting(t *testing.T) {
	shortMinute(t, 3*time.Second, time.Minute)
	v := &minuteVendor{clock: holdMinute(t)}
	s, gw := rpmGateway(t, v, 1)
	clk := v.clock
	if r := clk.atOnce(t, "the first", askRPM(context.Background(), gw, rlPath, rlBody)); r.err != nil || r.code != 200 {
		t.Fatalf("first: %v %d", r.err, r.code)
	}
	who := meterWho(provider.Provider{ID: "rl", Key: "k"})
	ctx, cancel := context.WithCancel(context.Background())
	second := askRPM(ctx, gw, rlPath, rlBody)
	clk.waitsUntil(t, "the second")
	if n := s.rpms.used(who); n != 2 {
		t.Fatalf("%d counted with the second waiting, want 2", n)
	}
	cancel()
	if r := rpmAnswered(t, "the canceled request", second); r.err == nil {
		t.Fatal("the canceled request answered")
	}
	rpmEventually(t, "its place given back", func() bool { return s.rpms.used(who) == 1 })
	// its wait ended with its place given back, so nothing is left to send
	// it once the minute has room
	if w := clk.waiting(); len(w) != 0 {
		t.Fatalf("%d still waiting for room after the agent went", len(w))
	}
	if n := len(v.times()); n != 1 {
		t.Fatalf("the vendor saw %d, want 1", n)
	}
}

// What uni-switch sends on its own counts too: a retry after the vendor's 503
// is a request of the minute, and so is a count_tokens that reaches the
// vendor. With 3 a minute, one request tried twice and one count leave no
// room, so the next chat waits for the minute.
func TestMaxRPMCountsRetriesAndCounts(t *testing.T) {
	window := 1500 * time.Millisecond
	slackMinute(t, window, time.Minute)
	v := &minuteVendor{fail: 1, late: 150 * time.Millisecond, clock: holdMinute(t)}
	_, gw := rpmGateway(t, v, 3)
	clk := v.clock
	t0 := clk.Now()
	if r := clk.atOnce(t, "the first", askRPM(context.Background(), gw, rlPath, rlBody)); r.err != nil || r.code != 200 {
		t.Fatalf("first: %v %d %s", r.err, r.code, r.body)
	}
	if n := len(v.times()); n != 2 {
		t.Fatalf("the vendor saw %d, want the 503 and its retry", n)
	}
	r := clk.atOnce(t, "the count, with room in the minute,", askRPM(context.Background(), gw, "/v1/messages/count_tokens", rlBody))
	var count struct {
		InputTokens int `json:"input_tokens"`
	}
	json.Unmarshal([]byte(r.body), &count)
	if r.err != nil || count.InputTokens != 7 {
		t.Fatalf("count = %d (%v %s), want the vendor's 7", count.InputTokens, r.err, r.body)
	}
	// with no room in the minute, a count doesn't wait: the estimate
	// answers, and the vendor isn't asked
	r = clk.atOnce(t, "a count with no room", askRPM(context.Background(), gw, "/v1/messages/count_tokens", rlBody))
	if r.err != nil || r.code != 200 {
		t.Fatalf("a count with no room: %v %d %s", r.err, r.code, r.body)
	}
	if n := len(v.times()); n != 3 {
		t.Fatalf("the vendor saw %d, want the count estimated", n)
	}
	last := askRPM(context.Background(), gw, rlPath, rlBody)
	until := clk.waitsUntil(t, "the last")
	if want := t0.Add(window + rpmSlack); !until.Equal(want) {
		t.Fatalf("the last waits to go %s after the first, want the window and slack, %s: the retry or the count wasn't counted", until.Sub(t0), want.Sub(t0))
	}
	clk.advance(until.Sub(clk.Now()))
	if r := rpmAnswered(t, "the last", last); r.err != nil || r.code != 200 {
		t.Fatalf("last: %v %d %s", r.err, r.code, r.body)
	}
	at := v.times()
	if len(at) != 4 {
		t.Fatalf("the vendor saw %d (%v), want 4", len(at), v.path)
	}
	if d := at[3].Sub(at[0]); d < window {
		t.Fatalf("the last went %s after the first, inside the %s window: the retry or the count wasn't counted", d, window)
	}
}

// The wait for room never holds the lock a request giving its place back
// needs: many waiting and many giving up at once all end.
func TestRPMWaitsAndGiveBacksTogether(t *testing.T) {
	shortMinute(t, 200*time.Millisecond, time.Minute)
	var l rpms
	var wg sync.WaitGroup
	for i := range 40 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), time.Duration(i%4)*100*time.Millisecond+50*time.Millisecond)
			defer cancel()
			l.wait(ctx, "x", 3, 0)
		}()
	}
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("waits didn't end")
	}
}

// The minute's arithmetic: room while fewer than the limit fall in the
// last minute, then a minute after the limit-th from the end, in order;
// one given back leaves room, and one past the longest wait is refused
// with when it could go.
func TestRPMReserve(t *testing.T) {
	shortMinute(t, time.Minute, 2*time.Minute)
	var l rpms
	t0 := time.Now()
	at := func(d time.Duration, longest time.Duration) (time.Duration, error) {
		got, err := l.reserve("x", 2, longest, t0.Add(d))
		return got.Sub(t0), err
	}
	for _, c := range []struct{ now, want time.Duration }{
		{0, 0}, {10 * time.Second, 10 * time.Second}, // room
		{20 * time.Second, 60 * time.Second},  // a minute after the first
		{20 * time.Second, 70 * time.Second},  // after the second
		{30 * time.Second, 120 * time.Second}, // after the third, to come
	} {
		if got, err := at(c.now, 2*time.Minute); err != nil || got != c.want {
			t.Fatalf("at %s: %s %v, want %s", c.now, got, err, c.want)
		}
	}
	_, err := at(30*time.Second, 2*time.Minute) // would go at 130s: 100s away
	if err != nil {
		t.Fatalf("100s away refused: %v", err)
	}
	_, err = at(30*time.Second, 90*time.Second) // at 180s: 150s away
	e, ok := err.(*errRPM)
	if !ok || e.retryAfter() != "150" {
		t.Fatalf("past the longest: %v, want an errRPM in 150s", err)
	}
	l.giveBack("x", t0.Add(130*time.Second))
	l.giveBack("x", t0.Add(120*time.Second))
	if got, err := at(31*time.Second, 2*time.Minute); err != nil || got != 120*time.Second {
		t.Fatalf("after two given back: %s %v, want 120s", got, err)
	}
	if l.free("x", 0) != true {
		t.Fatal("no limit isn't free")
	}
}

// The slack is part of the minute both ways: a request waits for a minute
// and the slack after the limit-th from the end, and one that old no longer
// counts only once the slack has passed too.
func TestRPMReserveKeepsTheSlack(t *testing.T) {
	shortMinute(t, time.Minute, 2*time.Minute)
	rpmSlack = time.Second
	var l rpms
	t0 := time.Now()
	for _, c := range []struct{ now, want time.Duration }{
		{0, 0}, {10 * time.Second, 61 * time.Second},
		{70 * time.Second, 122 * time.Second}, // a minute and the slack after 61
	} {
		got, err := l.reserve("x", 1, 0, t0.Add(c.now))
		if err != nil || got.Sub(t0) != c.want {
			t.Fatalf("at %s: %s %v, want %s", c.now, got.Sub(t0), err, c.want)
		}
	}
	l = rpms{}
	l.reserve("x", 1, 0, t0)
	if got, _ := l.reserve("x", 1, 0, t0.Add(60500*time.Millisecond)); got.Sub(t0) != 61*time.Second {
		t.Fatalf("half a second into the slack: %s, want 61s", got.Sub(t0))
	}
}

// A try's own requests are counted once, against the key or account the
// try is of, however the provider names its account; a request to another
// provider made on the way is counted against that one's.
func TestRPMTicketOfATry(t *testing.T) {
	s := &Server{}
	p := provider.Provider{ID: "codex", MaxRPM: 3, Account: &provider.Account{}}
	ctx := s.paidFor(context.Background(), p, "codex@someone")
	if got := s.metered(ctx, p, ""); got != ctx {
		t.Fatal("a try's own request got a ticket of its own: it would be counted twice")
	}
	other := provider.Provider{ID: "kimi", Key: "k", MaxRPM: 3}
	got := s.metered(ctx, other, "")
	tk, _ := got.Value(rpmKey{}).(*rpmTicket)
	if tk == nil || tk.who != meterWho(other) || tk.paid.Load() {
		t.Fatalf("another provider's request on the way: %+v", tk)
	}
}

package gateway

import (
	"bufio"
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/http/httptrace"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// streamVendor streams a short reply on chat completions, Responses and
// Messages; fail is how many requests it answers 500 "overloaded" first,
// after the first it lets through.
type streamVendor struct {
	mu    sync.Mutex
	seen  int
	at    []time.Time // when each request arrived
	nth   []int64     // each request's place in order (step)
	fail  int
	block chan struct{} // the first request waits on it, if set
	later time.Duration // the requests after the first wait this, quiet
	// when it began to answer each request, after block and later
	answered    []time.Time
	answeredNth []int64 // and its place in order (step)
}

// arrived is when the vendor's n-th request (from 0) arrived.
func (v *streamVendor) arrived(n int) (time.Time, bool) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if n < len(v.at) {
		return v.at[n], true
	}
	return time.Time{}, false
}

// step orders the gateway's writes to its agents and its vendor's
// arrivals: a clock can't, since Windows' advances in ticks of ~0.5ms
// and a keepalive heard, the slot freed and the vendor asked fit in one
// (#2249).
var step atomic.Int64

// arrivedNth is the place in order (step) of the vendor's n-th request.
func (v *streamVendor) arrivedNth(n int) (int64, bool) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if n < len(v.nth) {
		return v.nth[n], true
	}
	return 0, false
}

func (v *streamVendor) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	io.Copy(io.Discard, r.Body)
	v.mu.Lock()
	v.seen++
	v.at = append(v.at, time.Now())
	v.nth = append(v.nth, step.Add(1))
	n := v.seen
	failing := n > 1 && v.fail > 0
	if failing {
		v.fail--
	}
	v.mu.Unlock()
	if n == 1 && v.block != nil {
		select {
		case <-v.block:
		case <-r.Context().Done():
			return
		}
	}
	if n > 1 && v.later > 0 {
		select {
		case <-time.After(v.later):
		case <-r.Context().Done():
			return
		}
	}
	v.mu.Lock()
	v.answered = append(v.answered, time.Now())
	v.answeredNth = append(v.answeredNth, step.Add(1))
	v.mu.Unlock()
	if failing {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(500)
		io.WriteString(w, `{"error":{"type":"api_error","message":"the vendor is overloaded"}}`)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	switch {
	case strings.HasSuffix(r.URL.Path, "/messages"):
		io.WriteString(w, "event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_1\",\"type\":\"message\",\"role\":\"assistant\",\"model\":\"m\",\"content\":[],\"usage\":{\"input_tokens\":3,\"output_tokens\":0}}}\n\n"+
			"event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\",\"text\":\"\"}}\n\n"+
			"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"queued ok\"}}\n\n"+
			"event: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":0}\n\n"+
			"event: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":{\"output_tokens\":2}}\n\n"+
			"event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n")
	case strings.HasSuffix(r.URL.Path, "/responses"):
		io.WriteString(w, quietCreated+quietDelta("queued ok")+quietDone)
	default:
		io.WriteString(w, "data: {\"id\":\"c1\",\"object\":\"chat.completion.chunk\",\"model\":\"m\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":\"queued ok\"}}]}\n\n"+
			"data: {\"id\":\"c1\",\"object\":\"chat.completion.chunk\",\"model\":\"m\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":3,\"completion_tokens\":2,\"total_tokens\":5}}\n\n"+
			"data: [DONE]\n\n")
	}
}

// queueGateway is the real server (lanGuard in front) with a provider "q"
// of one key, set up by p, and every keepalive within milliseconds.
func queueGateway(t *testing.T, v *streamVendor, set func(*provider.Provider)) string {
	t.Helper()
	fresh(t)
	keepFast(t, keepaliveGap, 40*time.Millisecond, keepaliveLongest)
	k, w := keepHeldAfter, watchEvery
	keepHeldAfter, watchEvery = 80*time.Millisecond, 10*time.Millisecond
	t.Cleanup(func() { keepHeldAfter, watchEvery = k, w })
	up := httptest.NewServer(v)
	t.Cleanup(up.Close)
	p := provider.Provider{ID: "q", Name: "Queued", Key: "k", Models: []string{"m"}, Chat: up.URL + "/v1", Responses: up.URL + "/v1", Anthropic: up.URL}
	set(&p)
	if err := provider.Save(p); err != nil {
		t.Fatal(err)
	}
	gw := httptest.NewUnstartedServer(localGuard(New().Handler()))
	gw.Listener = wroteListener{gw.Listener}
	gw.Start()
	t.Cleanup(gw.Close)
	return gw.URL
}

// wrote is when the gateway first wrote to each agent's connection, by
// the agent's address: the time the gateway sent it, which a client's own
// read can't tell under load, its goroutine waiting to run.
var wrote sync.Map // string -> *firstWrite

// firstWrite is when, and in what place in order (step), the gateway
// first wrote to a connection. The place is taken before the write, so
// nothing the agent does on hearing it can come before it.
type firstWrite struct{ at, nth atomic.Int64 }

type wroteListener struct{ net.Listener }

func (l wroteListener) Accept() (net.Conn, error) {
	c, err := l.Listener.Accept()
	if err != nil {
		return c, err
	}
	at, all := new(firstWrite), new(writeLog)
	wrote.Store(c.RemoteAddr().String(), at)
	writes.Store(c.RemoteAddr().String(), all)
	return wroteConn{c, at, all}, nil
}

// writes is every time the gateway wrote to each agent's connection, by
// the agent's address.
var writes sync.Map // string -> *writeLog

type writeLog struct {
	mu  sync.Mutex
	at  []time.Time
	nth []int64 // each write's place in order (step), taken before it
}

// within says how many writes were after the place from and before to,
// in order (step), not by a clock.
func (l *writeLog) within(from, to int64) int {
	l.mu.Lock()
	defer l.mu.Unlock()
	n := 0
	for _, i := range l.nth {
		if i > from && i < to {
			n++
		}
	}
	return n
}

type wroteConn struct {
	net.Conn
	at  *firstWrite
	all *writeLog
}

func (c wroteConn) Write(b []byte) (int, error) {
	nth := step.Add(1)
	n, err := c.Conn.Write(b)
	if n > 0 {
		now := time.Now()
		if c.at.nth.CompareAndSwap(0, nth) {
			c.at.at.Store(now.UnixNano())
		}
		c.all.mu.Lock()
		c.all.at = append(c.all.at, now)
		c.all.nth = append(c.all.nth, nth)
		c.all.mu.Unlock()
	}
	return n, err
}

var queueAsks = []struct{ name, path, body string }{
	{"chat", "/v1/chat/completions", `{"model":"q/m","stream":true,"messages":[{"role":"user","content":"hi"}]}`},
	{"responses", "/v1/responses", `{"model":"q/m","stream":true,"input":"hi"}`},
	{"messages", "/v1/messages", `{"model":"q/m","stream":true,"max_tokens":50,"messages":[{"role":"user","content":"hi"}]}`},
}

// queueMinute is the minute of the tests that wait for room in it. A queued
// request waits for what is left of it when it arrives, which under load
// is late: it is long enough that what is left stays far over
// keepHeldAfter (a request ~1.4s late was seen with 8 busy goroutines per
// core).
const queueMinute = 3 * time.Second

// askHeard sends body to path as an agent would and returns the status, the
// stream read, when the gateway first wrote to the agent (wrote), and the
// read's error. heard, if set, is called once the agent has a first byte.
func askHeard(gw, path, body string, heard func()) (int, string, heardAt, error) {
	req, _ := http.NewRequest("POST", gw+path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	var from string
	req = req.WithContext(httptrace.WithClientTrace(context.Background(), &httptrace.ClientTrace{
		GotConn: func(i httptrace.GotConnInfo) { from = i.Conn.LocalAddr().String() },
		GotFirstResponseByte: func() {
			if heard != nil {
				heard()
			}
		},
	}))
	res, err := (&http.Client{Transport: &http.Transport{DisableKeepAlives: true}, Timeout: 30 * time.Second}).Do(req)
	if err != nil {
		return 0, "", heardAt{}, err
	}
	defer res.Body.Close()
	var b strings.Builder
	_, err = io.Copy(&b, bufio.NewReader(res.Body))
	var first heardAt
	if at, ok := wrote.Load(from); ok {
		if n := at.(*firstWrite).nth.Load(); n != 0 {
			first = heardAt{time.Unix(0, at.(*firstWrite).at.Load()), n}
		}
	}
	return res.StatusCode, b.String(), first, err
}

// heardAt is when the gateway first wrote to an agent, and its place in
// order (step); nth is 0 when it never wrote.
type heardAt struct {
	at  time.Time
	nth int64
}

// before says the gateway wrote to the agent before the vendor's n-th
// request arrived, in order, not by a clock.
func (h heardAt) before(v *streamVendor, n int) bool {
	asked, ok := v.arrivedNth(n)
	return ok && h.nth != 0 && h.nth < asked
}

// waitSeen waits until v has had n requests.
func waitSeen(t *testing.T, v *streamVendor, n int) {
	t.Helper()
	for end := time.Now().Add(10 * time.Second); ; time.Sleep(5 * time.Millisecond) {
		if _, ok := v.arrived(n - 1); ok {
			return
		}
		if time.Now().After(end) {
			t.Fatalf("the vendor never had %d requests", n)
		}
	}
}

// coeo91 on Discord: with a Requests per minute limit, a request held for
// room in the minute had its agent hear nothing at all — WorkBuddy, Trae
// and Qoder said it timed out. Through the real server, a streaming
// request that waits for the minute (queueMinute) hears the stream's 200 and
// keepalives before the vendor is asked, then the vendor's reply, in each
// protocol's stream. The test asks for that order, as the gateway wrote
// it, rather than for a client's idle timeout: under load (machine load
// ~130, 20582866a) every gap a client measures stretched, the first
// request's too, the client's goroutine waiting to run.
func TestQueuedStreamKeptAliveForTheMinute(t *testing.T) {
	for _, a := range queueAsks {
		t.Run(a.name, func(t *testing.T) {
			shortMinute(t, queueMinute, time.Minute)
			v := &streamVendor{}
			gw := queueGateway(t, v, func(p *provider.Provider) { p.MaxRPM = 1 })
			if code, body, _, err := askHeard(gw, a.path, a.body, nil); err != nil || code != 200 {
				t.Fatalf("first: %d %v %s", code, err, body)
			}
			code, body, heard, err := askHeard(gw, a.path, a.body, nil)
			if err != nil || code != 200 || !strings.Contains(body, ": keepalive") || !strings.Contains(body, "queued ok") {
				t.Fatalf("queued = %d %v %q, want 200, keepalives, then the reply", code, err, body)
			}
			first, _ := v.arrived(0)
			asked, _ := v.arrived(1)
			if !heard.before(v, 1) {
				t.Fatalf("the gateway first wrote to the agent at %s, the vendor was asked at %s: nothing reached it while it waited", heard.at.Format("15:04:05.000"), asked.Format("15:04:05.000"))
			}
			// it waited for the minute: the vendor sees the first late
			// under load (rpmSlack, zero here), so half of it
			if d := asked.Sub(first); d < queueMinute/2 {
				t.Fatalf("the vendor was asked %s after the first, inside the minute", d)
			}
		})
	}
}

// Once a queued request's agent has been kept alive, it hears a keepalive
// every keepaliveEvery after the request goes to its vendor too, not
// keepHeldAfter after that: the vendor's watch waited keepHeldAfter
// before its first keepalive, as if the agent had heard nothing, so an
// agent sent one just before the handoff went up to keepHeldAfter plus
// keepaliveEvery (30s with the real 15s each) with none. Here
// keepHeldAfter is 1.5s, keepaliveEvery 100ms, and the vendor stays
// quiet 1s on the queued request: the gateway must write to the agent
// between the vendor being asked and its answer, where the old watch,
// begun before the vendor was asked, wrote nothing until keepHeldAfter
// after it began. The check is on the gateway's own writes, in order,
// not on gaps a client measures, which stretch under load (3f4287e6).
// The request waits for a slot (MaxConcurrency), freed once its agent has
// heard a keepalive, so it is kept alive before the handoff however late
// it runs; a wait for room in the minute is kept alive and handed over
// the same way (keepQueued, then watch), but needed a minute sized for a
// loaded machine (#1654).
func TestQueuedStreamKeptAliveOnceSentToItsVendor(t *testing.T) {
	for _, a := range queueAsks {
		t.Run(a.name, func(t *testing.T) {
			one := 1
			v := &streamVendor{block: make(chan struct{}), later: time.Second}
			gw := queueGateway(t, v, func(p *provider.Provider) { p.MaxConcurrency = &one })
			keepFast(t, keepaliveGap, 100*time.Millisecond, keepaliveLongest)
			keepHeldAfter = 1500 * time.Millisecond // put back by queueGateway's cleanup
			first := holdSlot(t, gw, v, a.path, a.body)
			req, _ := http.NewRequest("POST", gw+a.path, strings.NewReader(a.body))
			req.Header.Set("Content-Type", "application/json")
			var from string
			req = req.WithContext(httptrace.WithClientTrace(context.Background(), &httptrace.ClientTrace{
				GotConn:              func(i httptrace.GotConnInfo) { from = i.Conn.LocalAddr().String() },
				GotFirstResponseByte: first.free,
			}))
			res, err := (&http.Client{Transport: &http.Transport{DisableKeepAlives: true}, Timeout: 30 * time.Second}).Do(req)
			if err != nil {
				t.Fatal(err)
			}
			b, err := io.ReadAll(res.Body)
			res.Body.Close()
			if err != nil || res.StatusCode != 200 || !strings.Contains(string(b), ": keepalive") || !strings.Contains(string(b), "queued ok") {
				t.Fatalf("queued = %d %v %q, want 200, keepalives, then the reply", res.StatusCode, err, b)
			}
			first.done(t)
			l, ok := writes.Load(from)
			if !ok {
				t.Fatal("no writes recorded for the agent's connection")
			}
			log := l.(*writeLog)
			v.mu.Lock()
			asked, answered := v.at[1], v.answered[1]
			askedNth, answeredNth := v.nth[1], v.answeredNth[1]
			v.mu.Unlock()
			if log.within(0, askedNth) == 0 {
				t.Fatalf("the agent wasn't kept alive before the vendor was asked")
			}
			if n := log.within(askedNth, answeredNth); n == 0 {
				t.Fatalf("the gateway wrote nothing to the kept-alive agent in the %s between the vendor being asked and its answer, want a keepalive every %s", answered.Sub(asked).Round(time.Millisecond), keepaliveEvery)
			}
		})
	}
}

// heldSlot is a request holding a provider's one slot (MaxConcurrency 1)
// at a vendor whose first request waits on its block.
type heldSlot struct {
	once sync.Once
	v    *streamVendor
	err  chan error
}

// free lets the holder's vendor answer, which frees the slot; once.
func (h *heldSlot) free() { h.once.Do(func() { close(h.v.block) }) }

// done waits for the holder's own reply.
func (h *heldSlot) done(t *testing.T) {
	t.Helper()
	if err := <-h.err; err != nil {
		t.Fatal(err)
	}
}

// holdSlot sends a request that takes the slot and keeps it until free; it
// is freed after 5s if nothing frees it, so a queued agent that hears
// nothing fails its test rather than hanging it.
func holdSlot(t *testing.T, gw string, v *streamVendor, path, body string) *heldSlot {
	t.Helper()
	h := &heldSlot{v: v, err: make(chan error, 1)}
	go func() {
		_, _, _, err := askHeard(gw, path, body, nil)
		h.err <- err
	}()
	waitSeen(t, v, 1) // the first has the slot
	stop := time.AfterFunc(5*time.Second, h.free)
	t.Cleanup(func() { stop.Stop(); h.free() })
	return h
}

// A slot of MaxConcurrency waited for is kept alive the same way.
func TestQueuedStreamKeptAliveForASlot(t *testing.T) {
	one := 1
	v := &streamVendor{block: make(chan struct{})}
	gw := queueGateway(t, v, func(p *provider.Provider) { p.MaxConcurrency = &one })
	first := make(chan error, 1)
	go func() {
		_, _, _, err := askHeard(gw, queueAsks[0].path, queueAsks[0].body, nil)
		first <- err
	}()
	waitSeen(t, v, 1) // the first has the slot
	// the slot is freed once the queued agent has heard something, or
	// after 5s when nothing reaches it
	var once sync.Once
	free := func() { once.Do(func() { close(v.block) }) }
	time.AfterFunc(5*time.Second, free)
	code, body, heard, err := askHeard(gw, queueAsks[0].path, queueAsks[0].body, free)
	if err != nil || code != 200 || !strings.Contains(body, ": keepalive") || !strings.Contains(body, "queued ok") {
		t.Fatalf("queued for a slot = %d %v %q, want 200, keepalives, then the reply", code, err, body)
	}
	if asked, _ := v.arrived(1); !heard.before(v, 1) {
		t.Fatalf("the gateway first wrote to the agent at %s, the vendor was asked at %s: nothing reached it while it waited", heard.at.Format("15:04:05.000"), asked.Format("15:04:05.000"))
	}
	if err := <-first; err != nil {
		t.Fatal(err)
	}
}

// Once a queued request's agent has the stream's 200, the vendor failing
// after it reaches the agent as its stream's error event, in its
// protocol's own shape, not as a status the agent can no longer be told,
// nor a JSON error body inside the stream. The request waits for a slot
// (MaxConcurrency), freed once its agent has heard a keepalive: a wait for
// room in the minute goes through the same keepQueued and handoff, but
// there each of the vendor's retries waited a minute more, nine seconds a
// protocol (#1654).
func TestQueuedStreamFailureIsTheStreamsError(t *testing.T) {
	wants := map[string][]string{
		"chat":      {`"error"`, "the vendor is overloaded"},
		"responses": {"event: response.failed", `"status":"failed"`, "the vendor is overloaded"},
		"messages":  {"event: error", `"type":"error"`, "the vendor is overloaded"},
	}
	for _, a := range queueAsks {
		t.Run(a.name, func(t *testing.T) {
			one := 1
			// failing every time it is asked: past the same-one retries
			// and the plain provider's patience (singlePatience) too
			v := &streamVendor{fail: 1000, block: make(chan struct{})}
			gw := queueGateway(t, v, func(p *provider.Provider) { p.MaxConcurrency = &one })
			first := holdSlot(t, gw, v, a.path, a.body)
			code, body, _, err := askHeard(gw, a.path, a.body, first.free)
			if err != nil || code != 200 || !strings.Contains(body, ": keepalive") {
				t.Fatalf("queued = %d %v %q, want the stream's 200 and keepalives", code, err, body)
			}
			first.done(t)
			if _, ok := v.arrived(1); !ok {
				t.Fatal("the queued request never reached the vendor")
			}
			for _, w := range wants[a.name] {
				if !strings.Contains(body, w) {
					t.Fatalf("stream %q doesn't say %s", body, w)
				}
			}
			if strings.Contains(body, `{"error":{"type":"api_error","message":"the vendor is overloaded"}}`+"\n") || strings.HasPrefix(strings.TrimLeft(strings.ReplaceAll(body, ": keepalive\n", ""), "\n"), "{") {
				t.Fatalf("the vendor's JSON error went into the stream raw: %q", body)
			}
		})
	}
}

// A request that waits out its queue (QueueWait) after keepalives were
// sent is told so as its stream's error, saying the limit.
func TestQueuedStreamWaitedOutIsTheStreamsError(t *testing.T) {
	one := 1
	v := &streamVendor{block: make(chan struct{})}
	defer close(v.block)
	gw := queueGateway(t, v, func(p *provider.Provider) { p.MaxConcurrency, p.QueueWait = &one, 1 })
	go askHeard(gw, queueAsks[2].path, queueAsks[2].body, nil)
	waitSeen(t, v, 1) // the first has the slot
	code, body, _, err := askHeard(gw, queueAsks[2].path, queueAsks[2].body, nil)
	if err != nil || code != 200 || !strings.Contains(body, ": keepalive") || !strings.Contains(body, "event: error") || !strings.Contains(body, "requests at once is its limit") {
		t.Fatalf("waited out = %d %v %q, want keepalives, then the limit as the stream's error", code, err, body)
	}
}

// A wait shorter than keepHeldAfter sends nothing ahead: turned away, it
// is still a 429 with its Retry-After.
func TestQueuedBrieflyStill429(t *testing.T) {
	shortMinute(t, 10*time.Second, 50*time.Millisecond)
	gw := queueGateway(t, &streamVendor{}, func(p *provider.Provider) { p.MaxRPM = 1 })
	if code, _, _, err := askHeard(gw, queueAsks[0].path, queueAsks[0].body, nil); err != nil || code != 200 {
		t.Fatalf("first: %d %v", code, err)
	}
	code, body, _, err := askHeard(gw, queueAsks[0].path, queueAsks[0].body, nil)
	if err != nil || code != 429 || strings.Contains(body, "keepalive") {
		t.Fatalf("second = %d %v %q, want a plain 429", code, err, body)
	}
}

// A try that wasn't held — the last one, its retries spent — and whose
// agent was kept alive while it waited is held from then on: the vendor's
// error is the stream's to tell (failTo), not a JSON body written into
// the stream the agent already has.
func TestQueuedUnheldTryHeldOnceKeptAlive(t *testing.T) {
	k, w := keepHeldAfter, watchEvery
	keepHeldAfter, watchEvery = 20*time.Millisecond, 5*time.Millisecond
	t.Cleanup(func() { keepHeldAfter, watchEvery = k, w })
	rec := httptest.NewRecorder()
	hw := newHoldWriter(rec, false)
	hw.alive, hw.streams = &keptAlive{proto: provider.Chat}, true
	stop := hw.keepQueued()
	time.Sleep(80 * time.Millisecond)
	stop()
	if !hw.alive.sent {
		t.Fatal("the agent wasn't kept alive while its try waited")
	}
	hw.Header().Set("Content-Type", "application/json")
	hw.WriteHeader(400)
	hw.Write([]byte(`{"error":{"message":"bad request"}}`))
	if hw.passing || strings.Contains(rec.Body.String(), "bad request") {
		t.Fatalf("the vendor's error went into the kept-alive stream: passing %v, %q", hw.passing, rec.Body.String())
	}
}

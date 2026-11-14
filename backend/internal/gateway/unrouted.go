package gateway

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"runtime/pprof"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// An agent's request that uni-switch holds before routing it went nowhere a
// user could see (heyusaku6 on X): Codex thought for over ten minutes on a
// routing group's model, then on two others, the Routing page showed none
// of them, and only a restart of Codex and uni-switch ended it. Codex waits for
// a reply's headers with no limit of its own, and a request shows on the
// Routing page once a route is begun for it, so whatever held it before
// that (a lock, a wait with no end) held it in silence.
//
// What held it isn't known. So a request with no route begun for it is
// shown on the Routing page as routing once it has waited unroutedShow,
// and is answered with an error once it has waited unroutedMost, with
// uni-switch's log saying where every goroutine stood. Its context ends then
// too, so what waits on it lets go.
//
// Two minutes of a spinner before that error was a torment of its own
// (sharwowwow on X). Most of what comes before a route is a wait uni-switch
// knows of and that may rightly be long: the body still arriving (an
// image over a slow link), the routing
// group's decision model, an image described for a model that can't see,
// a Codex reset spent, the plugins' gateway hooks and JS middleware, a
// slot or a minute's room (noteWait, notePhase). The rest is uni-switch's own
// work: parsing, the catalog, the plan. A request that has spent
// unroutedUnknown in none of those waits, added up however they came, is
// answered then with the same error; one in those waits still has
// unroutedMost. 30s is three times the slowest such work a report has
// measured (a cold catalog sync, ~8.5s) and leaves a loaded machine room.
var unroutedShow, unroutedUnknown, unroutedMost = 15 * time.Second, 30 * time.Second, 2 * time.Minute

// Uploads earn time at 32 KiB/s, only for time actually spent reading
// wire bytes. A large conversation over a slow link can finish (#4775),
// while a trickle still runs out of the ordinary two-minute hold. The
// socket's idle deadline and the decoded body size limit also still hold.
// Even compressed input that produces no decoded bytes can earn no more
// than eight minutes, bounding a hold to ten minutes overall.
// The grace is a var only for tests, which scale it with unroutedMost; it
// is counted in whole seconds.
const unroutedUploadRate = 32 << 10

var unroutedUploadGrace = 8 * time.Minute

// unroutedWatched is whether a request to path is an agent's turn, which
// is given a route once uni-switch knows where it goes: Chat, Responses,
// Messages, Gemini and Codex's own backend. A compaction or a model list
// has no route of its own, and an MCP or a search call is not a turn.
func unroutedWatched(path string) bool {
	switch path {
	case "/v1/chat/completions", "/chat/completions", "/v1/responses", "/responses",
		"/v1/messages", "/messages", CodexPath + "/responses":
		return true
	}
	return strings.HasPrefix(path, "/v1beta/models/") && (strings.HasSuffix(path, ":generateContent") || strings.HasSuffix(path, ":streamGenerateContent"))
}

// unroutedProto is the API path's client speaks, for its error.
func unroutedProto(path string) provider.Protocol {
	switch {
	case strings.HasSuffix(path, "/messages"):
		return provider.Anthropic
	case strings.HasPrefix(path, "/v1beta/"):
		return provider.Gemini
	case strings.HasSuffix(path, "/responses"):
		return provider.Responses
	}
	return provider.Chat
}

// heldAnswered, when set, is called as giveUp has answered a request, its
// client free to read the answer and close: a test holds it there.
var heldAnswered func(u *unroutedHold)

// errUnrouted ends a request's context when uni-switch stopped holding it.
var errUnrouted = errors.New("uni-switch held the request too long before routing it")

// heldWriter is the client's writer while uni-switch may still answer for its
// handler: what the handler writes goes through it, and once uni-switch has
// answered (gaveUp) the handler's writes go nowhere. The handler's headers
// are its own until it writes, so uni-switch's answer never shares a map
// another goroutine is filling.
type heldWriter struct {
	w      http.ResponseWriter
	mu     sync.Mutex
	hdr    http.Header
	wrote  bool // the handler began its reply
	gaveUp bool // uni-switch answered for it
}

func newHeldWriter(w http.ResponseWriter) *heldWriter {
	return &heldWriter{w: w, hdr: http.Header{}}
}

func (h *heldWriter) Header() http.Header {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.wrote {
		return h.w.Header()
	}
	return h.hdr
}

// begin is the handler's reply starting: its headers go to the client's.
// Called with mu held; false once uni-switch has answered.
func (h *heldWriter) begin() bool {
	if h.gaveUp {
		return false
	}
	if !h.wrote {
		h.wrote = true
		out := h.w.Header()
		for k, vs := range h.hdr {
			out[k] = vs
		}
	}
	return true
}

func (h *heldWriter) WriteHeader(code int) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.begin() {
		h.w.WriteHeader(code)
	}
}

func (h *heldWriter) Write(b []byte) (int, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if !h.begin() {
		return 0, http.ErrHandlerTimeout
	}
	return h.w.Write(b)
}

func (h *heldWriter) Flush() { _ = h.FlushError() }

// FlushError is http.ResponseController's Flush, which looks for it
// before it unwraps.
func (h *heldWriter) FlushError() error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if !h.begin() {
		return http.ErrHandlerTimeout
	}
	return http.NewResponseController(h.w).Flush()
}

// Unwrap is for http.ResponseController's deadlines.
func (h *heldWriter) Unwrap() http.ResponseWriter { return h.w }

// answer writes uni-switch's own reply, whole, unless the handler began one.
// Its length is said, so the client has all of it while the handler may
// still be held: a reply of no length ends only when the handler does.
func (h *heldWriter) answer(proto provider.Protocol, status int, msg string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.wrote || h.gaveUp {
		return false
	}
	h.gaveUp = true
	rec := httptest.NewRecorder()
	writeError(rec, proto, status, msg)
	out := h.w.Header()
	for k, vs := range rec.Header() {
		out[k] = vs
	}
	out.Set("Content-Length", strconv.Itoa(rec.Body.Len()))
	out.Set("Connection", "close")
	h.w.WriteHeader(status)
	h.w.Write(rec.Body.Bytes())
	_ = http.NewResponseController(h.w).Flush()
	return true
}

// unroutedHold is one watched request's watch: the route that shows it
// while it waits, and what ends it.
type unroutedHold struct {
	s *Server
	r *http.Request
	f *inflight
	// agent is who it is shown as (callerFrom), read as it comes: the
	// timers never read its headers, which withCaller and decodedBody
	// change meanwhile
	agent  string
	w      *heldWriter
	cancel context.CancelCauseFunc
	asked  atomic.Value // string: the model it asked for, once read
	shown  *Route       // under f.mu
	ended  bool         // under f.mu: answered by uni-switch
	over   bool         // under f.mu: its handler returned
	// saved is closed once giveUp, having answered, has written its
	// report: a test waits for it before its temp HOME goes
	saved chan struct{}
	// the body as it arrives (waitedBody): the bytes read, when the last
	// came (unix nanoseconds), whether a read met its end (or an error),
	// and stopped once uni-switch answered for it while it was still
	// arriving, so its handler reads no more of it
	bodyRead    atomic.Int64
	bodyLast    atomic.Int64
	bodyEnded   atomic.Bool
	bodyStopped atomic.Bool

	// the waits uni-switch knows it is in (noteWait), under wmu: how many are
	// under way, since when one has been, the time spent in one before
	// that, and whether unknown fired in one and looks again at its end
	wmu     sync.Mutex
	waits   int
	since   time.Time
	excused time.Duration
	due     bool
	unknown *time.Timer
	// each kind of known wait it was in, in the order first met, for the
	// report of where it was held (heldReport)
	kinds []*waitKind

	most *time.Timer
}

type unroutedKey struct{}

// noteAsked tells ctx's watch the model its request asked for, for the
// route that shows it.
func noteAsked(ctx context.Context, model string) {
	if u, _ := ctx.Value(unroutedKey{}).(*unroutedHold); u != nil {
		u.asked.Store(model)
	}
}

// elapsedLocked is how long the request has been held other than
// the upload time earned by received bytes; wmu is held.
func (u *unroutedHold) elapsedLocked(now time.Time) time.Duration {
	d := now.Sub(u.f.start)
	for _, k := range u.kinds {
		if k.kind == "body" {
			spent := k.spent
			if k.n > 0 {
				spent += now.Sub(k.since)
			}
			// Clamp before multiplying: compressed wire bytes need not
			// be bounded by the decoded size cap, and may overflow time.
			read := min(u.bodyRead.Load(), int64(unroutedUploadGrace/time.Second)*unroutedUploadRate)
			earned := time.Duration(read) * time.Second / unroutedUploadRate
			d -= min(spent, earned)
			break
		}
	}
	return d
}

// checkMost is most's timer: a request held unroutedMost other than
// earned upload time is answered for; one with time left is checked when that is up,
// but no sooner than unroutedRecheck. While an upload earns at least as
// fast as it is read, its held time stands still, so what is left of it
// doesn't shrink: a sliver left would be looked at again every sliver for
// as long as the body kept coming.
func (u *unroutedHold) checkMost() {
	u.wmu.Lock()
	if left := unroutedMost - u.elapsedLocked(time.Now()); left > 0 {
		u.most.Reset(max(left, unroutedRecheck()))
		u.wmu.Unlock()
		return
	}
	u.wmu.Unlock()
	u.giveUp(false)
}

// unroutedRecheck is the least checkMost waits before it looks again: a
// second, or an eighth of unroutedMost when a test makes that shorter. A
// hold is answered at most that late.
func unroutedRecheck() time.Duration { return min(time.Second, unroutedMost/8) }

// noteWait tells ctx's watch its request is in a wait uni-switch knows of
// until the func it answers is called: the time it spends there counts
// toward unroutedMost alone, not unroutedUnknown.
// kind names the wait in the report of a request held too long
// (heldReport): "body", "body room", "hooks" and the rest.
func noteWait(ctx context.Context, kind string) (done func()) {
	u, _ := ctx.Value(unroutedKey{}).(*unroutedHold)
	if u == nil {
		return func() {}
	}
	return u.wait(kind)
}

// waitKind is the time a request spent in one kind of known wait: how
// many of it are under way, since when, and the time before that.
type waitKind struct {
	kind  string
	n     int
	since time.Time
	spent time.Duration
}

// wait begins a known wait of kind, and answers the func that ends it,
// once. A kind of "" is counted but not named: a phase whose time its own
// wait names already.
func (u *unroutedHold) wait(kind string) func() {
	u.wmu.Lock()
	now := time.Now()
	if u.waits == 0 {
		u.since = now
	}
	u.waits++
	var k *waitKind
	if kind != "" {
		for _, x := range u.kinds {
			if x.kind == kind {
				k = x
			}
		}
		if k == nil {
			k = &waitKind{kind: kind}
			u.kinds = append(u.kinds, k)
		}
		if k.n++; k.n == 1 {
			k.since = now
		}
	}
	u.wmu.Unlock()
	var once sync.Once
	return func() {
		once.Do(func() {
			if k != nil {
				u.wmu.Lock()
				if k.n--; k.n == 0 {
					k.spent += time.Since(k.since)
				}
				u.wmu.Unlock()
			}
			u.waited()
		})
	}
}

// waited ends a known wait: the last to end adds its time to excused,
// and if unknown fired meanwhile it is set for what is left of it.
func (u *unroutedHold) waited() {
	u.wmu.Lock()
	defer u.wmu.Unlock()
	if u.waits--; u.waits > 0 {
		return
	}
	now := time.Now()
	u.excused += now.Sub(u.since)
	if u.due {
		u.due = false
		u.unknown.Reset(max(unroutedUnknown-u.unknownLocked(now), 0))
	}
}

// unknownLocked is how long the request has been held in no known wait;
// wmu is held.
func (u *unroutedHold) unknownLocked(now time.Time) time.Duration {
	d := now.Sub(u.f.start) - u.excused
	if u.waits > 0 {
		d -= now.Sub(u.since)
	}
	return d
}

// excusedFor is how long the request has been held in a known wait.
func (u *unroutedHold) excusedFor() time.Duration {
	u.wmu.Lock()
	defer u.wmu.Unlock()
	d := u.excused
	if u.waits > 0 {
		d += time.Since(u.since)
	}
	return d
}

// checkUnknown is unknown's timer: a request held unroutedUnknown in no
// known wait is answered for; one in a wait now is looked at again when
// it ends, and one with time left, when that is up.
func (u *unroutedHold) checkUnknown() {
	u.wmu.Lock()
	if u.waits > 0 {
		u.due = true
		u.wmu.Unlock()
		return
	}
	if left := unroutedUnknown - u.unknownLocked(time.Now()); left > 0 {
		u.unknown.Reset(left)
		u.wmu.Unlock()
		return
	}
	u.wmu.Unlock()
	u.giveUp(true)
}

// waitedBody is a watched request's body: the time spent waiting for it
// to arrive is a known wait, however much of it is read where.
type waitedBody struct {
	io.ReadCloser
	u *unroutedHold
}

func (b waitedBody) Read(p []byte) (int, error) {
	if b.u.bodyStopped.Load() {
		return 0, errBodyStopped
	}
	done := b.u.wait("body")
	defer done()
	n, err := b.ReadCloser.Read(p)
	if n > 0 {
		b.u.bodyRead.Add(int64(n))
		b.u.bodyLast.Store(time.Now().UnixNano())
	}
	if err != nil {
		b.u.bodyEnded.Store(true)
	}
	return n, err
}

// errBodyStopped is a body read after uni-switch answered for its request
// while the body was still arriving.
var errBodyStopped = errors.New("uni-switch stopped reading this request's body: it was still arriving when uni-switch answered for it")

// inBody is whether u's request is reading its body now.
func (u *unroutedHold) inBody() bool {
	u.wmu.Lock()
	defer u.wmu.Unlock()
	for _, k := range u.kinds {
		if k.kind == "body" {
			return k.n > 0
		}
	}
	return false
}

// bodyArriving is whether the handler has begun reading the body, is
// between two of its reads, and its end hasn't come: less has come than
// the request's own Content-Length says, or, for a body of no said
// length (chunked), no read has met its end yet. The hold can run out in
// the gap between two reads of a trickled body, and a read's absence
// alone answered such a request 504, as if uni-switch held it of its own (a
// full run raced one out there, #4511); a body short of its length is
// still arriving, and the time is the client's. Not before its first read: a request waiting for body
// room (bodyRoom.take) is held by uni-switch, whose 503 or 504 says so, and
// its client may have sent the whole body already. A chunked body's
// decoder spends its time between reads too (empty gzip members, #4775
// review), and judged by the read alone it was answered 504 there, as
// uni-switch's own hold. A body read whole and an empty one are not arriving.
func (u *unroutedHold) bodyArriving() bool {
	d := u.r.ContentLength
	if d == 0 || u.bodyEnded.Load() || d > 0 && u.bodyRead.Load() >= d {
		return false
	}
	u.wmu.Lock()
	defer u.wmu.Unlock()
	read := false
	for _, k := range u.kinds {
		switch k.kind {
		case "body":
			read = true
		case "body room":
			if k.n > 0 {
				return false
			}
		}
	}
	return read
}

// bodySaid is how much of u's request's body had come, and when the last
// of it did: "12 KB of 3 MB, the last 2s ago".
func (u *unroutedHold) bodySaid() string {
	n, of := u.bodyRead.Load(), ""
	if d := u.r.ContentLength; d > 0 {
		of = " of " + byteWord(d)
	}
	if n == 0 {
		return "none" + of
	}
	return fmt.Sprintf("%s%s, the last %s ago", byteWord(n), of, time.Since(time.Unix(0, u.bodyLast.Load())).Round(100*time.Millisecond))
}

// byteWord is n bytes in B, KB or MB.
func byteWord(n int64) string {
	switch {
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.1f KB", float64(n)/(1<<10))
	}
	return fmt.Sprintf("%d bytes", n)
}

// unroutedBodyMsg is the error of a request whose body was still arriving
// when it had been held for held: the time went to the client sending it,
// not to uni-switch, and it says so.
func unroutedBodyMsg(held time.Duration, body string) string {
	return fmt.Sprintf("uni-switch stopped this request after %s because its body was still arriving from the client (read: %s), before it could be routed to any provider. The upload exhausted its time allowance (32 KiB/s with a two-minute grace period, at most eight additional minutes). Check the connection or proxy to uni-switch, or compact the conversation and reduce inline images before retrying. "+heldSay, held, body, heldWhere())
}

// watchUnrouted watches r, counted as f, until the func it answers is
// called as its handler returns.
func (s *Server) watchUnrouted(w http.ResponseWriter, r *http.Request, f *inflight) (http.ResponseWriter, *http.Request, func()) {
	if f == nil || !unroutedWatched(r.URL.Path) {
		return w, r, func() {}
	}
	ctx, cancel := context.WithCancelCause(r.Context())
	u := &unroutedHold{s: s, f: f, w: newHeldWriter(w), cancel: cancel, agent: callerFrom(r).agent, saved: make(chan struct{})}
	r = r.WithContext(context.WithValue(ctx, unroutedKey{}, u))
	if r.Body != nil && r.Body != http.NoBody {
		r.Body = waitedBody{r.Body, u}
	}
	u.r = r
	f.mu.Lock()
	f.unrouted = u
	f.mu.Unlock()
	// The timers reach u through held alone, let go as the handler
	// returns. A stopped timer is not gone: the runtime keeps it, and its
	// func, in the timer heap of the P that set it until its time comes
	// or that P next tidies its heap, and an idle P never does. Through u
	// a stopped timer kept the request, its body and its context's values
	// up to unroutedMost after the handler returned (#5250: a socket's
	// last 256 KB turn body was kept that way).
	held := new(atomic.Pointer[unroutedHold])
	held.Store(u)
	via := func(m func(*unroutedHold)) func() {
		return func() {
			if u := held.Load(); u != nil {
				m(u)
			}
		}
	}
	u.wmu.Lock()
	u.unknown = time.AfterFunc(unroutedUnknown, via((*unroutedHold).checkUnknown))
	u.most = time.AfterFunc(unroutedMost, via((*unroutedHold).checkMost))
	u.wmu.Unlock()
	show := time.AfterFunc(unroutedShow, via((*unroutedHold).show))
	return u.w, r, func() {
		show.Stop()
		u.wmu.Lock()
		u.unknown.Stop()
		u.most.Stop()
		u.wmu.Unlock()
		held.Store(nil)
		f.mu.Lock()
		// a timer that fired as the handler returned does nothing now
		u.over = true
		shown := u.shown
		if !u.ended {
			u.shown = nil
		}
		f.mu.Unlock()
		if shown != nil && !u.ended {
			// it was answered with no route of its own: it is the
			// usage log's, as before
			s.trace.drop(shown)
		}
		cancel(context.Canceled)
	}
}

// show begins the route that shows the request on the Routing page, as
// routing, while nothing else does.
func (u *unroutedHold) show() {
	f := u.f
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.begun) > 0 || u.shown != nil || u.ended || u.over {
		return
	}
	u.showLocked()
}

// showLocked begins the route that shows it; f.mu is held.
func (u *unroutedHold) showLocked() {
	asked, _ := u.asked.Load().(string)
	rt := Route{Time: u.f.start, Agent: u.agent, Session: u.f.session, Model: asked, Provider: "uni-switch"}
	if rt.Agent == "" {
		rt.Agent = u.f.agent
	}
	rt.CallerKey, rt.CallerKeyName = callerKeyOf(u.r.Context())
	u.shown = u.s.trace.begin(rt)
}

// routed is the request's own route begun: the one that showed it goes.
func (u *unroutedHold) routed() (drop *Route) {
	if u.ended {
		return nil
	}
	drop, u.shown = u.shown, nil
	return drop
}

// giveUp answers the request with an error, when no route was begun for it
// and its handler wrote nothing, ends its context, and says on its route
// and in the log what held it. unknown: it was held unroutedUnknown in no
// wait uni-switch knows of, rather than unroutedMost other than
// earned upload time.
func (u *unroutedHold) giveUp(unknown bool) {
	f := u.f
	f.mu.Lock()
	if len(f.begun) > 0 || u.ended || u.over {
		f.mu.Unlock()
		return
	}
	held := time.Since(f.start).Round(time.Second)
	msg, where, status := unroutedMsg(held), "", http.StatusGatewayTimeout
	switch {
	case unknown:
		where = fmt.Sprintf(" %s of it in no wait uni-switch knows of", unroutedUnknown)
	case u.inBody() || u.bodyArriving():
		// the client was still sending it (heatonz on Discord: 2m0s
		// "without routing" with the body half in): a 408 says whose
		// the time was, and an SDK sends it again at once
		where = " its body still arriving (" + u.bodySaid() + ")"
		msg, status = unroutedBodyMsg(held, u.bodySaid()), http.StatusRequestTimeout
	}
	f.mu.Unlock()
	// The report is taken while it is still held, so before it is
	// answered: the answer says Connection: close, and once its client
	// has it and closes, the request's context ends and what held it lets
	// go (#4300). Taken after, it could name no wait under way, and the
	// stacks would show it gone. It is taken out of f.mu: the goroutine
	// dump is no wait for what begins a route.
	asked, _ := u.asked.Load().(string)
	who := cmpOr(u.agent, cmpOr(f.agent, "?"))
	said := cmpOr(where, " waiting for routing")
	waits := u.waitsSaid()
	report := u.s.heldReport(u, held, status, who, asked, said)
	f.mu.Lock()
	if len(f.begun) > 0 || u.ended || u.over {
		// routed, answered or let go meanwhile
		f.mu.Unlock()
		return
	}
	if !u.w.answer(unroutedProto(u.r.URL.Path), status, msg) {
		f.mu.Unlock()
		return // the handler is answering
	}
	if status == http.StatusRequestTimeout {
		// and the handler reads no more of it: its read ends now, not at
		// the next idle limit, and the connection goes with the answer
		u.bodyStopped.Store(true)
		_ = http.NewResponseController(u.w.w).SetReadDeadline(time.Now())
	}
	u.ended = true
	defer close(u.saved)
	if u.shown == nil {
		u.showLocked()
	}
	shown := u.shown
	f.mu.Unlock()
	if heldAnswered != nil {
		heldAnswered(u)
	}
	u.s.trace.update(shown, func(r *Route) {
		r.Done, r.Status, r.Error, r.Millis = true, status, msg, time.Since(r.Time).Milliseconds()
	})
	u.cancel(errUnrouted)
	name, err := writeHeld(report)
	saved := ""
	if err != nil {
		saved = "; its report could not be saved: " + err.Error()
	} else {
		saved = "; its report is " + filepath.Join(HeldDir(), name)
		u.s.trace.update(shown, func(r *Route) { r.Held = name })
	}
	log.Printf("gateway: %s %s for %s from %s held %s before routing,%s%s; stopped with %d%s%s", u.r.Method, u.r.URL.Path, cmpOr(asked, "?"), who, held, said, waits, status, saved, unroutedStacks())
}

// unroutedMsg describes a request held too long before routing.
func unroutedMsg(held time.Duration) string {
	return fmt.Sprintf("uni-switch held this request for %s without routing it to any provider, so it was stopped rather than left waiting. Send it again. "+heldSay, held, heldWhere())
}

// heldSay is where the error of a request held too long sends its user:
// the report uni-switch saved of it (heldReport), which a desktop app's user
// can reach, where its log goes only to a stderr nobody sees.
const heldSay = "If it keeps happening, report it with the report uni-switch saved of where it was held: Copy Report on this request on uni-switch's Routing page, or the newest file in %s."

func cmpOr(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// unroutedDumped is when the goroutines were last written to the log for
// a held request: one dump says where, and a burst of held requests
// writes it once.
var unroutedDumped atomic.Int64

// unroutedStacks is every goroutine's stack, alike ones counted once, for
// the log at most once in ten minutes; "" otherwise.
func unroutedStacks() string {
	now := time.Now().UnixNano()
	last := unroutedDumped.Load()
	if last != 0 && now-last < int64(10*time.Minute) || !unroutedDumped.CompareAndSwap(last, now) {
		return ""
	}
	var b bytes.Buffer
	_ = pprof.Lookup("goroutine").WriteTo(&b, 1)
	const most = 256 << 10
	if b.Len() > most {
		b.Truncate(most)
	}
	return "; where every goroutine stood:\n" + b.String()
}

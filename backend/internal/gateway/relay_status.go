package gateway

import (
	"context"
	"math"
	"net/http"
	"strconv"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// relayStatus relays a backend's events and keeps its upstream failures
// before a response is committed, including failures after partial
// non-streaming text; once streaming output has begun a failure is a
// terminal error event. It is for the backends that report their own
// statuses; name is the backend's, for an answer that never came.
func relayStatus(w http.ResponseWriter, from provider.Protocol, name string, req *Request, events <-chan Event, usage *Usage, abort context.CancelFunc) (int, string) {
	fail := func(ev Event) (int, string) {
		abort()
		code := ev.Status
		if code < 400 || code > 599 {
			code = 502
		}
		retryAfterOn(w, ev.Wait)
		return writeError(w, from, code, ev.Text), ev.Text
	}
	if req.Stream {
		var head []Event
		for ev := range events {
			if ev.Kind == KError {
				return fail(ev)
			}
			head = append(head, ev)
			if ev.Kind != KStart && ev.Kind != KUsage {
				break
			}
		}
		if len(head) == 0 || head[len(head)-1].Kind == KStart || head[len(head)-1].Kind == KUsage {
			return fail(Event{Text: name + " ended without an answer"})
		}
		sw := newSSEWriter(w)
		enc := encoder(from, sw, req, usage)
		var failed string
		see := func(ev Event) bool {
			if ev.Kind == KStart || ev.Kind == KUsage {
				usage.add(ev.Usage)
			}
			enc.event(ev)
			if ev.Kind == KError {
				failed = ev.Text
				return false
			}
			return true
		}
		for _, ev := range head {
			see(ev)
		}
		// kept from the client's idle timeout while none comes (#436)
		relayEvents(events, sw, enc, see)
		if failed != "" {
			abort()
			return 200, failed
		}
		enc.finish()
		return 200, ""
	}
	var col collector
	for ev := range events {
		if ev.Kind == KError {
			return fail(ev)
		}
		col.add(ev)
	}
	res := col.finish()
	usage.add(res.Usage)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(200)
	w.Write(renderUsage(from, res, req, usage))
	return 200, ""
}

// retryAfterOn sends, with an error about to be written to w, how long
// the vendor said to wait before asking again, when it said: the agent
// reads it, and so does the gateway's busy handling (noteBusy).
func retryAfterOn(w http.ResponseWriter, wait time.Duration) {
	if wait > 0 {
		w.Header().Set("Retry-After", strconv.Itoa(int(math.Ceil(wait.Seconds()))))
	}
}

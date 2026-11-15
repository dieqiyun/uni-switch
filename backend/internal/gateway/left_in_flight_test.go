package gateway

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// routeEnded reports s's newest route and whether it and every try of it
// is over.
func routeEnded(s *Server) (Route, bool) {
	s.trace.mu.Lock()
	defer s.trace.mu.Unlock()
	if len(s.trace.routes) == 0 {
		return Route{}, false
	}
	r := *s.trace.routes[len(s.trace.routes)-1]
	r.Tries = append([]Try{}, r.Tries...)
	ok := r.Done
	for _, t := range r.Tries {
		ok = ok && t.Done
	}
	return r, ok
}

// nothingInFlight waits until the gateway counts no request in flight.
func nothingInFlight(t *testing.T, s *Server) {
	t.Helper()
	for deadline := time.Now().Add(10 * time.Second); s.Busy().Requests != 0; time.Sleep(5 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatalf("%d requests still in flight", s.Busy().Requests)
		}
	}
}

// An agent that goes away while its request is with the vendor leaves
// nothing in flight (#2386, Moody-Sin: the page kept showing a request
// being called long after the agent had stopped): whether the vendor had
// begun its reply or said nothing yet, once the request is no longer
// counted its route and every try of it are over, and nothing is listed
// as active.
func TestAgentGoneMidRequestLeavesNothingInFlight(t *testing.T) {
	for _, began := range []bool{true, false} {
		name := "vendor silent"
		if began {
			name = "reply streaming"
		}
		t.Run(name, func(t *testing.T) {
			fresh(t)
			// the vendor: a reply begun (or nothing at all), then quiet until
			// its caller goes
			asked, gone := make(chan struct{}, 1), make(chan struct{})
			up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				io.ReadAll(r.Body)
				if began {
					w.Header().Set("Content-Type", "text/event-stream")
					w.WriteHeader(200)
					io.WriteString(w, "event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"m\",\"type\":\"message\",\"role\":\"assistant\",\"model\":\"m\",\"content\":[],\"usage\":{\"input_tokens\":3,\"output_tokens\":0}}}\n\n")
					io.WriteString(w, "event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\",\"text\":\"\"}}\n\n")
					io.WriteString(w, "event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"hel\"}}\n\n")
					w.(http.Flusher).Flush()
				}
				asked <- struct{}{}
				select {
				case <-r.Context().Done():
				case <-gone:
				}
			}))
			t.Cleanup(up.Close)
			t.Cleanup(func() { close(gone) })
			if err := provider.Save(provider.Provider{ID: "a", Name: "A", Key: "k", Models: []string{"m"}, Anthropic: up.URL}); err != nil {
				t.Fatal(err)
			}
			s := New()
			gw := httptest.NewServer(s.Handler())
			t.Cleanup(gw.Close)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			req, _ := http.NewRequestWithContext(ctx, "POST", gw.URL+"/v1/messages",
				strings.NewReader(`{"model":"a/m","max_tokens":10,"stream":true,"messages":[{"role":"user","content":"hi"}]}`))
			req.Header.Set("Content-Type", "application/json")
			ended := make(chan struct{})
			go func() {
				defer close(ended)
				if res, err := http.DefaultClient.Do(req); err == nil {
					io.Copy(io.Discard, res.Body)
					res.Body.Close()
				}
			}()
			select {
			case <-asked:
			case <-time.After(10 * time.Second):
				t.Fatal("the vendor was never asked")
			}
			if _, ok := routeEnded(s); ok {
				t.Fatal("the route was over before the agent left")
			}
			cancel() // the agent is stopped
			<-ended
			nothingInFlight(t, s)
			r, ok := routeEnded(s)
			if !ok {
				t.Fatalf("the agent gone and nothing counted, its route is still in flight: %+v", r)
			}
			if a := s.Active(); len(a) != 0 {
				t.Errorf("still listed as active: %+v", a)
			}
		})
	}
}

// A request whose handling panics (net/http recovers it and drops the
// connection) still ends every route begun for it, each try too: a route
// left not done shows as still answering on the Routing page and counts as in
// progress for good, and the trace never trims it (#2386). The route is
// over as failed, and the panic goes on to net/http as before.
func TestPanicEndsTheRequestsRoutes(t *testing.T) {
	fresh(t)
	s := New()
	var ids []int64
	h := s.counted(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// the request's own route, with a try out at the vendor, and one
		// begun beside it (a decision call's)
		rt := s.trace.beginFor(r, Route{Time: time.Now(), Agent: "zcode", Model: "a/m", Provider: "a"})
		s.trace.update(rt, func(t *Route) { t.Tries = append(t.Tries, Try{ID: "a#1", Model: "m", Start: time.Now()}) })
		side := s.trace.beginFor(r, Route{Time: time.Now(), Agent: "zcode", Model: "a/m", Provider: "a"})
		ids = append(ids, rt.ID, side.ID)
		panic("a translator broke")
	}))
	repanicked := func() (p any) {
		defer func() { p = recover() }()
		req := httptest.NewRequest("POST", "/v1/messages", strings.NewReader(`{}`))
		h.ServeHTTP(httptest.NewRecorder(), req)
		return nil
	}()
	if repanicked != "a translator broke" {
		t.Fatalf("the panic went on as %v, want it as it was", repanicked)
	}
	if n := s.Busy().Requests; n != 0 {
		t.Fatalf("%d requests in flight after the panic", n)
	}
	s.trace.mu.Lock()
	defer s.trace.mu.Unlock()
	if len(s.trace.routes) != 2 {
		t.Fatalf("%d routes, want 2", len(s.trace.routes))
	}
	for i, r := range s.trace.routes {
		if r.ID != ids[i] {
			t.Fatalf("route %d is %d, want %d", i, r.ID, ids[i])
		}
		if !r.Done || r.Status < 500 || r.Error == "" {
			t.Errorf("route %d after the panic: done %v status %d error %q, want done, failed", r.ID, r.Done, r.Status, r.Error)
		}
		for _, tr := range r.Tries {
			if !tr.Done || tr.Status < 500 {
				t.Errorf("route %d's try still in flight: %+v", r.ID, tr)
			}
		}
	}
	if s.trace.totals.Requests != 2 || s.trace.totals.Errors != 2 {
		t.Errorf("totals %+v, want 2 requests, 2 errors", s.trace.totals)
	}
}

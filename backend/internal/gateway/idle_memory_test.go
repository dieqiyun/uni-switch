package gateway

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// Once the gateway rests, what its requests left in the heap goes back to
// the system, once per rest, and never while a request is in flight
// (#3472).
func TestRestingGatewayHandsMemoryBack(t *testing.T) {
	fresh(t)
	s := New()
	var released atomic.Int32
	s.idle.rest = 20 * time.Millisecond
	s.idle.release = func() { released.Add(1) }
	t.Cleanup(s.idle.stop)
	hold, held := make(chan struct{}), make(chan struct{})
	h := s.counted(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/hold" {
			close(held)
			<-hold
		}
	}))
	post := func(path string) {
		h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("POST", path, strings.NewReader(`{}`)))
	}
	waitReleased := func(n int32) {
		t.Helper()
		for deadline := time.Now().Add(10 * time.Second); released.Load() < n; {
			if time.Now().After(deadline) {
				t.Fatalf("released %d times, want %d", released.Load(), n)
			}
			time.Sleep(5 * time.Millisecond)
		}
	}

	post("/v1/messages")
	waitReleased(1)

	// a request in flight: the rest it would end is not one
	go post("/hold")
	<-held
	s.releaseIfResting()
	if n := released.Load(); n != 1 {
		t.Fatalf("released %d times with a request in flight, want 1", n)
	}
	// a request just ended: the rest is not long enough yet
	s.idle.rest = time.Hour
	post("/v1/messages")
	s.idle.stop()
	s.releaseIfResting()
	if n := released.Load(); n != 1 {
		t.Fatalf("released %d times right after a request, want 1", n)
	}
	s.idle.stop()
	s.idle.rest = 20 * time.Millisecond
	close(hold)
	for s.Busy().Requests != 0 {
		time.Sleep(time.Millisecond)
	}
	waitReleased(2)
	// a GET (the model list another uni-switch asks for) is not work to rest from
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/v1/models", nil))
	s.idle.mu.Lock()
	armed := s.idle.due != nil
	s.idle.mu.Unlock()
	if armed {
		t.Fatal("a GET armed a release")
	}
}

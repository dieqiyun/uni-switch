package gateway

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"testing"
)

// The routing is this process's own, so a uni-switch on this computer that does
// not serve the gateway reads it here to show the same Routing page (#2606).
// It answers this computer alone: the trace names accounts and what they
// served, and the uni-switch that reads it is on the same machine.
func TestGatewayTraceIsLoopbackOnly(t *testing.T) {
	h := New().Handler()

	t.Run("this computer", func(t *testing.T) {
		w := httptest.NewRecorder()
		r := httptest.NewRequest("GET", "/v1/uni-switch/trace", nil)
		r.RemoteAddr = "127.0.0.1:50123"
		h.ServeHTTP(w, r)
		if w.Code != 200 {
			t.Fatalf("%d %s", w.Code, w.Body)
		}
		var got struct {
			Object string            `json:"object"`
			Seq    int64             `json:"seq"`
			Routes []json.RawMessage `json:"routes"`
			Pid    int               `json:"pid"`
			Serves string            `json:"serves"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
			t.Fatal(err)
		}
		if got.Object != "trace" || got.Routes == nil {
			t.Fatalf("object %q routes %v", got.Object, got.Routes)
		}
		// the console names the uni-switch routing from the trace it plays
		if got.Pid != os.Getpid() || got.Serves != Serves {
			t.Errorf("the trace doesn't name its uni-switch: pid %d serves %q", got.Pid, got.Serves)
		}
	})

	// a proxy or tunnel on this computer (cloudflared, Tailscale serve)
	// hands on someone else's request from loopback
	t.Run("through a proxy here", func(t *testing.T) {
		for _, hdr := range []string{"X-Forwarded-For", "Cf-Connecting-IP", "Tailscale-User-Login"} {
			w := httptest.NewRecorder()
			r := httptest.NewRequest("GET", "/v1/uni-switch/trace", nil)
			r.RemoteAddr = "127.0.0.1:50123"
			r.Header.Set(hdr, "203.0.113.7")
			h.ServeHTTP(w, r)
			if w.Code != 403 {
				t.Fatalf("a request a proxy handed on (%s) reached the trace: %d %s", hdr, w.Code, w.Body)
			}
		}
	})

	t.Run("another machine", func(t *testing.T) {
		w := httptest.NewRecorder()
		r := httptest.NewRequest("GET", "/v1/uni-switch/trace", nil)
		r.RemoteAddr = "192.168.0.9:50123"
		h.ServeHTTP(w, r)
		if w.Code != 403 {
			t.Fatalf("another machine reached the trace: %d %s", w.Code, w.Body)
		}
	})
}

// Another uni-switch's trace is read from this computer alone (#2606): an
// address on another interface or host is never asked, whatever
// UNI_SWITCH_ADDR says, and one listening everywhere is read on loopback.
func TestOwnerReadOnLoopbackAlone(t *testing.T) {
	for addr, want := range map[string]string{
		"127.0.0.1:3561":       "http://127.0.0.1:3561",
		"0.0.0.0:3561":         "http://127.0.0.1:3561",
		":3561":                "http://127.0.0.1:3561",
		"[::]:3561":            "http://127.0.0.1:3561",
		"localhost:3561":       "http://127.0.0.1:3561",
		"[::1]:3561":           "http://[::1]:3561",
		"127.0.0.2:3561":       "http://127.0.0.2:3561",
		"192.168.1.9:3561":     "",
		"10.0.0.5:3561":        "",
		"example.com:3561":     "",
		"uni-switch.tail:3561": "",
	} {
		t.Setenv("UNI_SWITCH_ADDR", addr)
		if got := ownerURL(); got != want {
			t.Errorf("UNI_SWITCH_ADDR %s: read at %q, want %q", addr, got, want)
		}
	}
	t.Setenv("UNI_SWITCH_ADDR", "192.0.2.1:3561")
	if _, ok := OwnerTrace(t.Context(), 0, 0); ok {
		t.Error("a trace read from another host")
	}
	if _, ok := OwnerLanes(); ok {
		t.Error("lanes read from another host")
	}
}

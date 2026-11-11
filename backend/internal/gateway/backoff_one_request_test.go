package gateway

import (
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// One request whose model fails alike on every Codex account — the
// ChatGPT backend's 500, or a stream that fails with server_error — is
// asked of each account again and again by the request's patience. Each
// account rests on the backoff ladder's first step, a minute: its own
// failures within the one rest aren't counted again (ly on Discord: seven
// Plus accounts on gpt-6-astra all resting up to the 10-minute cap after
// one bad spell, each counted 13 times by one request). Once its rest has
// ended, failing again climbs the ladder as before.
func TestOneRequestDoesNotClimbTheBackoff(t *testing.T) {
	for _, x := range []struct {
		name   string
		refuse func(w http.ResponseWriter)
	}{
		{"500", func(w http.ResponseWriter) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusInternalServerError)
			io.WriteString(w, `{"error":{"message":"An error occurred while processing your request.","type":"server_error"}}`)
		}},
		{"response.failed", func(w http.ResponseWriter) {
			w.Header().Set("Content-Type", "text/event-stream")
			io.WriteString(w, sse(`data: {"type":"response.created","response":{"id":"r0"}}`,
				`data: {"type":"response.failed","response":{"id":"r0","status":"failed","error":{"code":"server_error","message":"An error occurred while processing your request."}}}`))
		}},
	} {
		t.Run(x.name, func(t *testing.T) {
			codexSignedIn(t, "b@example.com", "c@example.com")
			if err := provider.SetRouting("codex", provider.Ordered); err != nil {
				t.Fatal(err)
			}
			var tried []string
			chatgptRefusing(t, &tried, x.refuse, "acct-2", "acct-3")
			s := New()
			codexAskOn(s, `{"model":"gpt-6-astra","stream":true,"input":"ping"}`)
			if len(tried) <= 3 {
				t.Fatalf("each account asked once only (%v): the request's own retries aren't what this tests", tried)
			}
			rested := 0
			for _, who := range []string{"codex@me@example.com", "codex@b@example.com", "codex@c@example.com"} {
				r, ok := restOf(who)
				if !ok {
					continue
				}
				rested++
				if r.Why != failOther || r.Failures != 1 || time.Until(r.Until) > fallbackCooldown {
					t.Fatalf("%s rests %v by %s, %d failures in a row, after one request asked it %d times", who, time.Until(r.Until).Round(time.Second), r.By, r.Failures, len(tried)/3)
				}
			}
			if rested == 0 {
				t.Fatal("no account rests: the test proves nothing")
			}

			// its rest over, the next failure climbs to the next step
			restingUntil.Lock()
			for k := range restingUntil.m {
				restingUntil.m[k] = time.Now().Add(-time.Second)
			}
			restingUntil.Unlock()
			c := candidate{p: provider.Provider{ID: "codex", Account: &provider.Account{Agent: "codex", User: "me@example.com"}}, model: "gpt-6-astra", rest: "codex@me@example.com"}
			r := s.restAfter(c, http.StatusInternalServerError, http.Header{}, []byte(`{"error":{"message":"An error occurred while processing your request.","type":"server_error"}}`))
			if r.Failures != 2 || r.By != "backoff" || time.Until(r.Until) <= fallbackCooldown {
				t.Fatalf("failing again after its rest: %d failures, rests %v by %s", r.Failures, time.Until(r.Until).Round(time.Second), r.By)
			}
		})
	}
}

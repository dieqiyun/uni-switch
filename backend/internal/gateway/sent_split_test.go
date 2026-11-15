package gateway

import (
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/usage"
)

// busyThenAnswers holds its first request for hold and answers it 503, as
// OpenCode Go did under a long context (#1532), and streams the next.
type busyThenAnswers struct {
	hold time.Duration
	n    atomic.Int32
}

func (b *busyThenAnswers) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	io.ReadAll(r.Body)
	if b.n.Add(1) == 1 {
		time.Sleep(b.hold)
		http.Error(w, `{"error":{"message":"Service Unavailable"}}`, http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	io.WriteString(w, chatStreamOK)
}

// #1532: a request answered on its second try, after the vendor held the
// first for a long while and said 503, recorded that wait as uni-switch's own
// time ("In uni-switch" 104 s) — what the detail showed the reporter as uni-switch
// being slow. uni-switch's own time is only its own; the try that failed first
// is told apart.
func TestSentTellsUniSwitchFromEarlierTries(t *testing.T) {
	fresh(t)
	const hold = 1500 * time.Millisecond
	up := &busyThenAnswers{hold: hold}
	slowOn(t, "a", up)
	s := New()
	code, body := postTo(t, s, "/v1/responses", `{"model":"a/m","stream":true,"input":[{"type":"message","role":"user","content":[{"type":"input_text","text":"hi"}]}]}`)
	if code != 200 || !strings.Contains(body, "from b") || up.n.Load() != 2 {
		t.Fatalf("%d %s (tries %d)", code, body, up.n.Load())
	}
	recs := usage.Load(time.Time{})
	if len(recs) == 0 {
		t.Fatal("no record")
	}
	r := recs[len(recs)-1]
	if r.Tries != 2 {
		t.Fatalf("tries %d", r.Tries)
	}
	// the answering try went out after the failed one and its pause
	if r.Sent < hold.Milliseconds() || r.Sent > r.TTFT {
		t.Fatalf("sent %d, ttft %d", r.Sent, r.TTFT)
	}
	if r.Retried < hold.Milliseconds() || r.Retried > r.Sent {
		t.Fatalf("earlier tries %d of sent %d", r.Retried, r.Sent)
	}
	if r.Own < 0 || r.Own >= 500 || r.Queued != 0 || r.Own+r.Retried+r.Queued > r.Sent+1 {
		t.Fatalf("uni-switch's own %d, earlier tries %d, queued %d, sent %d", r.Own, r.Retried, r.Queued, r.Sent)
	}
}

// What of Sent was whose: the try that failed first is not uni-switch's, nor
// a wait for a slot, nor connecting and sending the body.
func TestSplitSent(t *testing.T) {
	start := time.Unix(1000, 0)
	at := func(ms int64) time.Time { return start.Add(time.Duration(ms) * time.Millisecond) }
	for _, c := range []struct {
		name                 string
		sent                 int64
		first, began         int64
		asked                int64 // ms from start; -1 unknown
		queued               int64
		own, retried, waited int64
	}{
		// #1532: 104 s before the answering try's body went, ~100 s of it
		// the try OpenCode Go held before its 503
		{"failed try first", 104000, 30, 103000, 103900, 0, 930, 102970, 0},
		{"one try", 200, 30, 30, 150, 0, 150, 0, 0},
		{"slot waited for", 5200, 30, 30, 5100, 5000, 100, 0, 5000},
		{"upload unknown", 900, 30, 30, -1, 0, 900, 0, 0},
	} {
		asked := int64(0)
		if c.asked >= 0 {
			asked = at(c.asked).UnixNano()
		}
		own, retried, waited := splitSent(c.sent, start, at(c.first), at(c.began), asked, c.queued)
		if own != c.own || retried != c.retried || waited != c.waited {
			t.Errorf("%s: own %d retried %d waited %d, want %d %d %d", c.name, own, retried, waited, c.own, c.retried, c.waited)
		}
	}
}

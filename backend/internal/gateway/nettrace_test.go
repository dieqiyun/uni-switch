package gateway

import (
	"crypto/tls"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// heldHeaders holds each request for hold before its headers, as a vendor
// thinking before it answers, then streams a reply.
type heldHeaders struct{ hold time.Duration }

func (h heldHeaders) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	io.ReadAll(r.Body)
	time.Sleep(h.hold)
	w.Header().Set("Content-Type", "text/event-stream")
	io.WriteString(w, chatStreamOK)
}

// A try says where its time went up to the reply's first byte (TryNet):
// a new connection's DNS, connect and TLS, writing the request, and the
// vendor's wait before its headers, which is where a vendor's slow start
// shows and not as uni-switch's (Discord, Magic_zero). The next request on
// the open connection spends none of the three. The route has the
// usage record's split of the time before the request went out.
func TestTryTellsWhereItsTimeWent(t *testing.T) {
	fresh(t)
	const hold = 300 * time.Millisecond
	up := httptest.NewTLSServer(heldHeaders{hold: hold})
	t.Cleanup(up.Close)
	// by name, so the resolver is asked
	base := strings.Replace(up.URL, "127.0.0.1", "localhost", 1) + "/v1"
	if err := provider.Save(provider.Provider{ID: "a", Name: "A", Key: "k", Models: []string{"m"}, Chat: base}); err != nil {
		t.Fatal(err)
	}
	s := New()
	tr := &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}
	t.Cleanup(tr.CloseIdleConnections)
	s.client = &http.Client{Transport: tr}
	ask := func() (Route, TryNet) {
		t.Helper()
		code, body := postAs(t, s, "", `{"model":"a/m","stream":true,"messages":[{"role":"user","content":"hi"}]}`)
		if code != 200 || !strings.Contains(body, "from b") {
			t.Fatalf("%d %s", code, body)
		}
		r := lastRoute(s)
		if len(r.Tries) != 1 || r.Tries[0].Net == nil {
			t.Fatalf("no timing: %+v", r.Tries)
		}
		return r, *r.Tries[0].Net
	}
	r, n := ask()
	t.Logf("a new connection: %+v", n)
	// the vendor's wait is the hold, give or take the machine
	holdMs := float64(hold.Milliseconds())
	if n.Wait < holdMs-1 || n.Wait > holdMs+5000 {
		t.Fatalf("wait %v for a %v hold: %+v", n.Wait, hold, n)
	}
	if n.Reused || n.TLS <= 0 || n.Asks != 1 || n.Before != 0 {
		t.Fatalf("a new connection: %+v", n)
	}
	// each step is in the time to the first byte, in order
	if sum := n.Own + n.Pool + n.DNS + n.Connect + n.TLS + n.Send + n.Wait; sum > n.Head+1 {
		t.Fatalf("steps %v over the %v to the first byte: %+v", sum, n.Head, n)
	}
	if try := r.Tries[0]; float64(try.TTFT) < n.Head-1 || float64(try.Millis) < n.Head-1 {
		t.Fatalf("first byte %v after the first token %d or the end %d", n.Head, try.TTFT, try.Millis)
	}
	if r.Sent <= 0 || r.Own <= 0 || r.Sent > r.TTFT || r.Own > r.Sent {
		t.Fatalf("route: sent %d own %d ttft %d", r.Sent, r.Own, r.TTFT)
	}
	// the open connection carries the next one
	_, n = ask()
	if !n.Reused || n.DNS != 0 || n.Connect != 0 || n.TLS != 0 || n.Wait < holdMs-1 {
		t.Fatalf("the open connection: %+v", n)
	}
}

// A try that sent nothing through Go's client (a plugin's own fetch) has
// no timing, rather than one made of nothing.
func TestTryNetWithoutARequest(t *testing.T) {
	var n netTrace
	if got := n.timing(time.Now(), 0); got != nil {
		t.Fatalf("%+v", got)
	}
}

// Who sent a try: a plugin, Claude Code for a Claude subscription, and
// uni-switch itself, whose try has its timing, for the rest.
func TestSentBy(t *testing.T) {
	for _, c := range []struct {
		p    provider.Provider
		want string
	}{
		{provider.Provider{ID: "claude", Account: &provider.Account{Agent: "claude", User: "u"}}, "claude"},
		{provider.Provider{ID: "codex", Account: &provider.Account{Agent: "codex", User: "u"}}, ""},
		{provider.Provider{ID: "a"}, ""},
	} {
		if got := sentBy(c.p); got != c.want {
			t.Errorf("sentBy(%s) = %q, want %q", c.p.ID, got, c.want)
		}
	}
}

package gateway

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// staleUpstream answers each connection's first request and closes the
// connection on its second without a word, as a proxy or vendor front does
// with a keep-alive connection it has dropped meanwhile: the request was
// written into it, and the read finds EOF (#2136: Pi → Codex, "POST …: EOF"
// and "write: broken pipe" before any reply, each asked again after a
// pause).
type staleUpstream struct {
	ln    net.Listener
	conns atomic.Int32
	asked atomic.Int32
	keyed atomic.Bool // a request came with an idempotency key on the wire
	wg    sync.WaitGroup
	mu    sync.Mutex
	open  []net.Conn
}

func newStaleUpstream(t *testing.T) *staleUpstream {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	u := &staleUpstream{ln: ln}
	u.wg.Add(1)
	go func() {
		defer u.wg.Done()
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			u.conns.Add(1)
			u.mu.Lock()
			u.open = append(u.open, c)
			u.mu.Unlock()
			u.wg.Add(1)
			go func() {
				defer u.wg.Done()
				u.serve(c)
			}()
		}
	}()
	t.Cleanup(func() {
		// the gateway keeps its connections alive: they end with the test
		ln.Close()
		u.mu.Lock()
		for _, c := range u.open {
			c.Close()
		}
		u.mu.Unlock()
		u.wg.Wait()
	})
	return u
}

func (u *staleUpstream) serve(c net.Conn) {
	defer c.Close()
	br := bufio.NewReader(c)
	for n := 0; ; n++ {
		req, err := http.ReadRequest(br)
		if err != nil {
			return
		}
		io.Copy(io.Discard, req.Body)
		req.Body.Close()
		u.asked.Add(1)
		if _, ok := req.Header["X-Idempotency-Key"]; ok {
			u.keyed.Store(true)
		}
		if n > 0 {
			return // dropped: the request is read, and never answered
		}
		body := `{"id":"r1","object":"response","status":"completed","model":"m","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"hello"}]}],"usage":{"input_tokens":3,"output_tokens":1,"total_tokens":4}}`
		fmt.Fprintf(c, "HTTP/1.1 200 OK\r\nContent-Type: application/json\r\nContent-Length: %d\r\n\r\n%s", len(body), body)
	}
}

// A request that finds the vendor's keep-alive connection dropped is sent
// again at once on a new one, by the transport, as one try: not failed as
// a 502 and asked again after a second's pause (#2136).
func TestDroppedKeepAliveIsAskedAgainAtOnce(t *testing.T) {
	fresh(t)
	up := newStaleUpstream(t)
	if err := provider.Save(provider.Provider{ID: "a", Name: "A", Key: "k", Models: []string{"m"}, Responses: "http://" + up.ln.Addr().String() + "/v1"}); err != nil {
		t.Fatal(err)
	}
	s := New()
	gw := httptest.NewServer(s.Handler())
	t.Cleanup(gw.Close)
	ask := func() {
		t.Helper()
		res, err := http.Post(gw.URL+"/v1/responses", "application/json", strings.NewReader(`{"model":"a/m","stream":false,"input":"hi"}`))
		if err != nil {
			t.Fatal(err)
		}
		b, _ := io.ReadAll(res.Body)
		res.Body.Close()
		if res.StatusCode != 200 || !strings.Contains(string(b), "hello") {
			t.Fatalf("got %d %s", res.StatusCode, b)
		}
	}
	ask() // the connection's first request: answered, and kept alive
	ask() // goes on the kept connection, which drops it
	if got := up.asked.Load(); got != 3 {
		t.Fatalf("upstream asked %d times, want 3 (the dropped one again on a new connection)", got)
	}
	if up.keyed.Load() {
		t.Fatal("the vendor was sent an X-Idempotency-Key: the mark is for Go's transport only")
	}
	r := lastRoute(s)
	if len(r.Tries) != 1 {
		var said []string
		for _, tr := range r.Tries {
			said = append(said, fmt.Sprintf("%d %s", tr.Status, tr.Error))
		}
		t.Fatalf("the dropped connection took %d tries (%s), want 1: sent again by the transport at once", len(r.Tries), strings.Join(said, "; "))
	}
}

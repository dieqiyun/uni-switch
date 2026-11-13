package gateway

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"io"
	"net"
	"net/http"
	"net/http/httptest"

	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/netproxy"
)

// silentPath sits between the gateway and a vendor and can go silent on
// the connections open through it: it goes on taking what the gateway
// writes, so every write succeeds, and passes nothing either way, as a
// local proxy whose own way out died, or a NAT that forgot the flow, does.
// A connection opened after it went silent passes as before.
type silentPath struct {
	ln   net.Listener
	to   string
	mu   sync.Mutex
	open []*silentConn
	wg   sync.WaitGroup
}

type silentConn struct {
	a, b   net.Conn
	silent atomic.Bool
}

func newSilentPath(t *testing.T, to string) *silentPath {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	p := &silentPath{ln: ln, to: to}
	p.wg.Add(1)
	go func() {
		defer p.wg.Done()
		for {
			a, err := ln.Accept()
			if err != nil {
				return
			}
			b, err := net.Dial("tcp", to)
			if err != nil {
				a.Close()
				continue
			}
			c := &silentConn{a: a, b: b}
			p.mu.Lock()
			p.open = append(p.open, c)
			p.mu.Unlock()
			pipe := func(dst, src net.Conn) {
				defer p.wg.Done()
				buf := make([]byte, 32<<10)
				for {
					n, err := src.Read(buf)
					if n > 0 && !c.silent.Load() {
						if _, werr := dst.Write(buf[:n]); werr != nil {
							return
						}
					}
					if err != nil {
						return
					}
				}
			}
			p.wg.Add(2)
			go pipe(b, a)
			go pipe(a, b)
		}
	}()
	t.Cleanup(func() {
		p.ln.Close()
		p.mu.Lock()
		for _, c := range p.open {
			c.a.Close()
			c.b.Close()
		}
		p.mu.Unlock()
		p.wg.Wait()
	})
	return p
}

// hush silences every connection open now.
func (p *silentPath) hush() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, c := range p.open {
		c.silent.Store(true)
	}
	return len(p.open)
}

func (p *silentPath) conns() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.open)
}

// quickPings has the gateway's transports, made after it, ping a quiet
// HTTP/2 connection after 200ms and drop it 300ms later, and trust
// vendor's certificate.
func quickPings(t *testing.T, vendor *httptest.Server) {
	pool := x509.NewCertPool()
	pool.AddCert(vendor.Certificate())
	wasTLS, wasAfter, wasWait := upstreamTLS, netproxy.PingAfter, netproxy.PingWait
	upstreamTLS = &tls.Config{RootCAs: pool, ServerName: "127.0.0.1"}
	netproxy.PingAfter, netproxy.PingWait = 200*time.Millisecond, 300*time.Millisecond
	t.Cleanup(func() { upstreamTLS, netproxy.PingAfter, netproxy.PingWait = wasTLS, wasAfter, wasWait })
}

func h2Vendor(t *testing.T, h http.HandlerFunc) *httptest.Server {
	vendor := httptest.NewUnstartedServer(h)
	vendor.EnableHTTP2 = true
	vendor.StartTLS()
	t.Cleanup(vendor.Close)
	return vendor
}

// #2843 (pinewhite): every request of a group to ClinePass hung with no
// first byte for half an hour, and a restart answered the same request in
// 1.6s. Over HTTP/2 every request to a vendor shares one connection, and
// one gone silent without closing (a proxy's way out died, a NAT dropped
// the flow) was kept and given every new request, each waiting up to the
// 10-minute ResponseHeaderTimeout, while the kernel took the writes. The
// gateway's transport now pings a connection quiet for a while and drops
// it when the ping goes unanswered: the request on it fails (so the group
// moves on) and the next one dials a new connection.
func TestSilentHTTP2ConnectionIsDropped(t *testing.T) {
	fresh(t)
	vendor := h2Vendor(t, func(w http.ResponseWriter, r *http.Request) {
		io.Copy(io.Discard, r.Body)
		io.WriteString(w, r.Proto)
	})
	path := newSilentPath(t, vendor.Listener.Addr().String())
	quickPings(t, vendor)
	s := New()
	t.Cleanup(s.client.CloseIdleConnections)
	u := "https://" + path.ln.Addr().String() + "/v1/chat/completions"
	ask := func(within time.Duration) (string, error) {
		ctx, cancel := context.WithTimeout(context.Background(), within)
		defer cancel()
		req, _ := http.NewRequestWithContext(ctx, http.MethodPost, u, nil)
		res, err := s.client.Do(req)
		if err != nil {
			return "", err
		}
		defer res.Body.Close()
		b, err := io.ReadAll(res.Body)
		return string(b), err
	}
	if got, err := ask(10 * time.Second); err != nil || got != "HTTP/2.0" {
		t.Fatalf("first request: %q, %v; want HTTP/2.0", got, err)
	}
	if n := path.hush(); n != 1 {
		t.Fatalf("%d connections open before the path went silent, want 1", n)
	}
	// the stalled request ends well before a client gives up (curl -m 55
	// in the report), here 10s against a ping bound of 0.5s
	began := time.Now()
	_, err := ask(10 * time.Second)
	if err == nil {
		t.Fatal("a request on the silent connection was answered")
	}
	if d := time.Since(began); d >= 10*time.Second {
		t.Fatalf("a request on the silent connection hung %v, until the client gave up: %v", d, err)
	}
	// and the one after it goes out on a new connection, without a restart
	if got, err := ask(10 * time.Second); err != nil || got != "HTTP/2.0" {
		t.Fatalf("request after the silent connection: %q, %v; want HTTP/2.0 on a new connection", got, err)
	}
	if n := path.conns(); n != 2 {
		t.Errorf("%d connections dialled, want 2: the silent one and a new one", n)
	}
}

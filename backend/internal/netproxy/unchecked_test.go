package netproxy

import (
	"context"
	"errors"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// selfSigned is an HTTPS server whose certificate nothing trusts, as a
// vLLM on the LAN or an Ollama behind a self-signed cert is (#2383).
func selfSigned(t *testing.T, h http.HandlerFunc) *httptest.Server {
	t.Helper()
	s := httptest.NewUnstartedServer(h)
	s.EnableHTTP2 = true
	s.Config.ErrorLog = log.New(io.Discard, "", 0) // the refused handshakes
	s.StartTLS()
	t.Cleanup(s.Close)
	return s
}

func get(t *testing.T, c *http.Client, ctx context.Context, u string) error {
	t.Helper()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	res, err := c.Do(req)
	if err != nil {
		return err
	}
	io.Copy(io.Discard, res.Body)
	res.Body.Close()
	return nil
}

func certRefused(err error) bool {
	return err != nil && strings.Contains(err.Error(), "certificate")
}

// A provider's leave not to check its certificate reaches its own host
// alone: the host it names is reached, while the same request without
// it, one to another host (a redirect there too), and one made for
// another provider (With) are all checked and refused.
func TestUncheckedTLSOnlyItsHost(t *testing.T) {
	other := selfSigned(t, func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "other") })
	own := selfSigned(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/elsewhere" {
			http.Redirect(w, r, other.URL+"/", http.StatusFound)
			return
		}
		io.WriteString(w, r.Proto)
	})
	d := Dispatch(&http.Transport{Proxy: Func, ForceAttemptHTTP2: true}).(*dispatch)
	t.Cleanup(d.CloseIdleConnections)
	c := &http.Client{Transport: d}
	bg := context.Background()
	mine := WithUncheckedTLS(bg, own.URL+"/v1")

	if err := get(t, c, bg, own.URL); !certRefused(err) {
		t.Fatalf("without the leave: want a certificate error, got %v", err)
	}
	if err := get(t, c, mine, own.URL+"/v1/models"); err != nil {
		t.Fatalf("with the leave: %v", err)
	}
	if err := get(t, c, mine, other.URL); !certRefused(err) {
		t.Fatalf("another host with the leave: want a certificate error, got %v", err)
	}
	if err := get(t, c, mine, own.URL+"/elsewhere"); !certRefused(err) {
		t.Fatalf("a redirect to another host: want a certificate error, got %v", err)
	}
	if err := get(t, c, With(mine, ""), own.URL); !certRefused(err) {
		t.Fatalf("a request made for another provider: want a certificate error, got %v", err)
	}
	if err := get(t, c, bg, own.URL); !certRefused(err) {
		t.Fatalf("after: want a certificate error, got %v", err)
	}
	// http:// is no leave for https://
	if err := get(t, c, WithUncheckedTLS(bg, strings.Replace(own.URL, "https:", "http:", 1)), own.URL); !certRefused(err) {
		t.Fatalf("an http:// base: want a certificate error, got %v", err)
	}
}

// The provider's own proxy, when it speaks https://, would be dialled with
// the same TLS config as the provider's host, and its certificate go
// unchecked too: the request is refused, the proxy never asked. Through an
// http:// proxy the same request goes through.
func TestUncheckedTLSStillChecksItsProxy(t *testing.T) {
	own := selfSigned(t, func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "ok") })
	var mu sync.Mutex
	var conns []net.Conn
	overTLS := 0 // CONNECTs the https:// proxy was asked
	t.Cleanup(func() {
		mu.Lock()
		defer mu.Unlock()
		for _, c := range conns {
			c.Close()
		}
	})
	// a CONNECT proxy taking every host to own: the provider's host is a
	// name, not loopback, which is never proxied
	connect := func(w http.ResponseWriter, r *http.Request) {
		if r.TLS != nil {
			mu.Lock()
			overTLS++
			mu.Unlock()
		}
		if r.Method != http.MethodConnect {
			http.Error(w, "CONNECT only", http.StatusMethodNotAllowed)
			return
		}
		up, err := net.Dial("tcp", own.Listener.Addr().String())
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		c, _, err := w.(http.Hijacker).Hijack()
		if err != nil {
			up.Close()
			return
		}
		mu.Lock()
		conns = append(conns, c, up)
		mu.Unlock()
		io.WriteString(c, "HTTP/1.1 200 Connection established\r\n\r\n")
		go func() { io.Copy(up, c); up.Close() }()
		io.Copy(c, up)
		c.Close()
	}
	plain := httptest.NewServer(http.HandlerFunc(connect))
	t.Cleanup(plain.Close)
	tlsProxy := httptest.NewUnstartedServer(http.HandlerFunc(connect))
	tlsProxy.Config.ErrorLog = log.New(io.Discard, "", 0)
	tlsProxy.StartTLS()
	t.Cleanup(tlsProxy.Close)

	d := Dispatch(&http.Transport{Proxy: Func, ForceAttemptHTTP2: true}).(*dispatch)
	t.Cleanup(d.CloseIdleConnections)
	c := &http.Client{Transport: d}
	const base = "https://vllm.lan.test:8443/v1"
	via := func(proxy string) context.Context { return WithUncheckedTLS(With(context.Background(), proxy), base) }
	if err := get(t, c, via(plain.URL), base+"/models"); err != nil {
		t.Fatalf("through an http:// proxy: %v", err)
	}
	if err := get(t, c, With(context.Background(), plain.URL), base+"/models"); !certRefused(err) {
		t.Fatalf("through an http:// proxy without the leave: want a certificate error, got %v", err)
	}
	if err := get(t, c, via(tlsProxy.URL), base+"/models"); !errors.Is(err, ErrUncheckedOverHTTPSProxy) {
		t.Fatalf("an https:// proxy: want it refused, got %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if overTLS != 0 {
		t.Fatalf("the https:// proxy was asked %d times, want none", overTLS)
	}
}

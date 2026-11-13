package provider

import (
	"bufio"
	"context"
	"crypto/tls"
	"crypto/x509"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/netproxy"
	"github.com/dieqiyun/uni-switch/backend/internal/settings"
)

// h2Vendor is a TLS server offering HTTP/2 that notes, for each request,
// its path, the protocol it came over and whether the TLS hello offered h2.
type h2Vendor struct {
	*httptest.Server
	mu  sync.Mutex
	got []string
}

func newH2Vendor(t *testing.T, answer func(path string) string) *h2Vendor {
	t.Helper()
	v := &h2Vendor{}
	offered := map[string]bool{} // the client's address → its hello offered h2
	v.Server = httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		v.mu.Lock()
		h2 := offered[r.RemoteAddr]
		v.got = append(v.got, r.URL.Path+" "+r.Proto+map[bool]string{true: " (h2 offered)"}[h2])
		v.mu.Unlock()
		io.WriteString(w, answer(r.URL.Path))
	}))
	v.EnableHTTP2 = true
	v.TLS = &tls.Config{GetConfigForClient: func(h *tls.ClientHelloInfo) (*tls.Config, error) {
		v.mu.Lock()
		offered[h.Conn.RemoteAddr().String()] = slices.Contains(h.SupportedProtos, "h2")
		v.mu.Unlock()
		return nil, nil
	}}
	v.StartTLS()
	t.Cleanup(v.Close)
	return v
}

// asked is what the vendor was asked since the last call.
func (v *h2Vendor) asked() []string {
	v.mu.Lock()
	defer v.mu.Unlock()
	got := v.got
	v.got = nil
	return got
}

// tunnel is an HTTP proxy that tunnels every CONNECT to addr and notes
// the host:port asked for.
type tunnel struct {
	ln  net.Listener
	mu  sync.Mutex
	got []string
}

func newTunnel(t *testing.T, addr string) *tunnel {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	p := &tunnel{ln: ln}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go p.serve(c, addr)
		}
	}()
	return p
}

func (p *tunnel) URL() string { return "http://" + p.ln.Addr().String() }

func (p *tunnel) connects() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return slices.Clone(p.got)
}

func (p *tunnel) serve(c net.Conn, addr string) {
	defer c.Close()
	br := bufio.NewReader(c)
	req, err := http.ReadRequest(br)
	if err != nil || req.Method != http.MethodConnect {
		io.WriteString(c, "HTTP/1.1 405 Method Not Allowed\r\nContent-Length: 0\r\n\r\n")
		return
	}
	p.mu.Lock()
	p.got = append(p.got, req.Host)
	p.mu.Unlock()
	up, err := net.Dial("tcp", addr)
	if err != nil {
		io.WriteString(c, "HTTP/1.1 502 Bad Gateway\r\nContent-Length: 0\r\n\r\n")
		return
	}
	defer up.Close()
	io.WriteString(c, "HTTP/1.1 200 Connection Established\r\n\r\n")
	done := make(chan struct{}, 2)
	go func() { io.Copy(up, br); done <- struct{}{} }()
	go func() { io.Copy(c, up); done <- struct{}{} }()
	<-done
}

// What uni-switch asks the ChatGPT backend on its own (the models list, the
// warm-up, the allowance, its resets and spending one) goes over HTTP/1.1,
// as the gateway's turns there do and Codex itself does, and its TLS hello
// offers no h2: directly, through Settings' proxy and through the
// account's own. Another vendor's allowance is still asked over HTTP/2.
// (reporter-provided: Codex's POST to /backend-api/codex/responses failed
// with PROTOCOL_ERROR through a local proxy until HTTP/2 was turned off.)
func TestCodexBackendOwnRequestsOverHTTP1(t *testing.T) {
	chatgpt := newH2Vendor(t, func(path string) string {
		switch {
		case strings.HasSuffix(path, "/codex/models"):
			return `{"models":[{"slug":"gpt-6.1-sol","visibility":"list"}]}`
		case strings.HasSuffix(path, "/consume"):
			return `{"code":"reset","windows_reset":1}`
		}
		return `{}`
	})
	other := newH2Vendor(t, func(string) string { return `{}` })
	pool := x509.NewCertPool()
	pool.AddCert(chatgpt.Certificate())
	pool.AddCert(other.Certificate())

	// http.DefaultClient as netproxy.Install leaves it: HTTP/2 wherever a
	// server offers it
	h2 := http.DefaultTransport.(*http.Transport).Clone()
	h2.Proxy, h2.TLSClientConfig = netproxy.Func, &tls.Config{RootCAs: pool}
	dispatch := netproxy.Dispatch(h2)
	oldClient, oldBase := http.DefaultClient.Transport, CodexBase
	http.DefaultClient.Transport = dispatch
	t.Cleanup(func() {
		dispatch.(interface{ CloseIdleConnections() }).CloseIdleConnections()
		http.DefaultClient.Transport, CodexBase = oldClient, oldBase
	})
	sign := func(_ context.Context, r *http.Request, _ []byte) error {
		r.Header.Set("Authorization", "Bearer t")
		return nil
	}
	ask := func(ctx context.Context) {
		t.Helper()
		if _, err := codexModels(ctx, sign); err != nil {
			t.Fatalf("models: %v", err)
		}
		if err := warmCodex(ctx, sign, "gpt-6.1-sol", "low"); err != nil {
			t.Fatalf("warm-up: %v", err)
		}
		if _, _, _, _, _, err := codexWindows(ctx, "t", "acct"); err != nil {
			t.Fatalf("allowance: %v", err)
		}
		base := strings.TrimSuffix(CodexBase, "/codex")
		codexResets(ctx, base, "t", "acct", 1)
		if _, err := consumeCodexReset(ctx, base, "t", "acct", "c1", "r1"); err != nil {
			t.Fatalf("reset: %v", err)
		}
	}
	want := []string{
		"/backend-api/codex/models HTTP/1.1",
		"/backend-api/codex/responses HTTP/1.1",
		"/backend-api/wham/usage HTTP/1.1",
		"/backend-api/wham/rate-limit-reset-credits HTTP/1.1",
		"/backend-api/wham/rate-limit-reset-credits/consume HTTP/1.1",
	}
	check := func(what string) {
		t.Helper()
		if got := chatgpt.asked(); !slices.Equal(got, want) {
			t.Errorf("%s: the ChatGPT backend was asked\n%s\nwant\n%s", what, strings.Join(got, "\n"), strings.Join(want, "\n"))
		}
	}

	CodexBase = chatgpt.URL + "/backend-api/codex"
	ask(context.Background())
	check("directly")

	var bill struct{}
	if err := accountJSON(context.Background(), other.URL+"/v1/billing", "k", nil, &bill); err != nil {
		t.Fatal(err)
	}
	if got := other.asked(); !slices.Equal(got, []string{"/v1/billing HTTP/2.0 (h2 offered)"}) {
		t.Errorf("another vendor was asked %v, not over HTTP/2", got)
	}

	// through a proxy the ChatGPT backend is a name, not this computer
	// (loopback is never proxied): example.com is in the test certificate
	CodexBase = "https://example.com/backend-api/codex"
	global := newTunnel(t, chatgpt.Listener.Addr().String())
	st := settings.Load()
	st.Proxy = global.URL()
	if err := settings.Save(st); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		st := settings.Load()
		st.Proxy = ""
		settings.Save(st)
	})
	ask(context.Background())
	check("through Settings' proxy")
	if got := global.connects(); len(got) == 0 || slices.ContainsFunc(got, func(h string) bool { return h != "example.com:443" }) {
		t.Errorf("Settings' proxy was asked to CONNECT %v", got)
	}

	own := newTunnel(t, chatgpt.Listener.Addr().String())
	st = settings.Load()
	st.Proxy = "direct"
	if err := settings.Save(st); err != nil {
		t.Fatal(err)
	}
	ask(netproxy.With(context.Background(), own.URL()))
	check("through the account's own proxy")
	if got := own.connects(); len(got) == 0 {
		t.Errorf("the account's own proxy was never asked")
	}
}

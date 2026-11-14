package gui

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/netproxy"
)

// proxyCheckRig: a trace page at a name that resolves nowhere
// (trace.example.invalid), answered as 198.51.100.7 in CN by a server the
// test's dialer sends a direct request to, and a forward proxy that
// answers it as 203.0.113.9 in US (or as exit, when given). Nothing
// leaves the machine: the name is .invalid and both servers are loopback.
func proxyCheckRig(t *testing.T, exit ...string) (client *http.Client, proxyURL string, asked *int) {
	t.Helper()
	answer := "fl=1\nip=203.0.113.9\nloc=US\n"
	if len(exit) > 0 {
		answer = exit[0]
	}
	asked = new(int)
	direct := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		fmt.Fprint(rw, "fl=1\nh=trace.example.invalid\nip=198.51.100.7\nts=1\nloc=CN\nwarp=off\n")
	}))
	t.Cleanup(direct.Close)
	proxy := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		*asked++
		if r.URL.Host != "trace.example.invalid" || r.URL.Path != "/cdn-cgi/trace" {
			http.Error(rw, "not the trace page: "+r.URL.String(), http.StatusBadGateway)
			return
		}
		fmt.Fprint(rw, answer)
	}))
	t.Cleanup(proxy.Close)
	old := proxyTraceURL
	proxyTraceURL = "http://trace.example.invalid/cdn-cgi/trace"
	t.Cleanup(func() { proxyTraceURL = old })
	base := &http.Transport{Proxy: netproxy.Func}
	dial := (&net.Dialer{}).DialContext
	base.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
		if addr == "trace.example.invalid:80" {
			addr = direct.Listener.Addr().String()
		}
		return dial(ctx, network, addr)
	}
	tr := netproxy.Dispatch(base)
	t.Cleanup(base.CloseIdleConnections)
	return &http.Client{Transport: tr}, proxy.URL, asked
}

func TestProxyCheckTellsTheProxyFromDirect(t *testing.T) {
	client, proxyURL, asked := proxyCheckRig(t)
	ctx := context.Background()
	got := checkProxy(ctx, client, netproxy.With(ctx, proxyURL), true)
	if got.Through == nil || got.Through.IP != "203.0.113.9" || got.Through.Loc != "US" || got.Through.Error != "" {
		t.Fatalf("through the proxy: %+v", got.Through)
	}
	if got.Direct.IP != "198.51.100.7" || got.Direct.Loc != "CN" || got.Direct.Error != "" {
		t.Fatalf("direct: %+v", got.Direct)
	}
	if *asked != 1 {
		t.Fatalf("the proxy was asked %d times, want 1 (the direct request went through it?)", *asked)
	}
	if !got.Through.Proxied || got.Direct.Proxied {
		t.Fatalf("proxied: through %v, direct %v; want true, false", got.Through.Proxied, got.Direct.Proxied)
	}
}

// #2369 (Emt-lin): under a TUN that takes every connection (FlClash with
// auto-route), the direct request leaves by the proxy's exit too, so both
// say one address. The request through the proxy still went to it, and
// the answer says so: Proxied, not "as if there were no proxy".
func TestProxyCheckSaysTheProxyTookItUnderOneExit(t *testing.T) {
	client, proxyURL, asked := proxyCheckRig(t, "fl=1\nip=198.51.100.7\nloc=CN\n")
	ctx := context.Background()
	got := checkProxy(ctx, client, netproxy.With(ctx, proxyURL), true)
	if got.Through == nil || got.Through.IP != got.Direct.IP || got.Through.Error != "" {
		t.Fatalf("one exit both ways: through %+v, direct %+v", got.Through, got.Direct)
	}
	if !got.Through.Proxied || *asked != 1 {
		t.Fatalf("the request through the proxy reached it (asked %d), yet proxied = %v", *asked, got.Through.Proxied)
	}
}

// A proxy the trace page is passed by (NO_PROXY names it): the request
// "through" it went direct, and Proxied says so.
func TestProxyCheckSaysAProxyPassedBy(t *testing.T) {
	client, proxyURL, asked := proxyCheckRig(t)
	t.Setenv("ALL_PROXY", proxyURL)
	t.Setenv("NO_PROXY", "trace.example.invalid")
	ctx := context.Background()
	got := checkProxy(ctx, client, ctx, true)
	if got.Through == nil || got.Through.IP != "198.51.100.7" || *asked != 0 {
		t.Fatalf("passed by: %+v, the proxy asked %d times", got.Through, *asked)
	}
	if got.Through.Proxied {
		t.Fatal("a request that went direct says it went through the proxy")
	}
}

// connectedTo matches a proxy named by host name to the address it
// resolves to, and tells another port apart.
func TestProxyCheckConnectedToByName(t *testing.T) {
	ctx := context.Background()
	at := &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 7890}
	for raw, want := range map[string]bool{
		"http://localhost:7890":   true,
		"http://127.0.0.1:7890":   true,
		"http://127.0.0.1:7891":   false,
		"socks5://127.0.0.2:7890": false,
	} {
		u, _ := url.Parse(raw)
		if got := connectedTo(ctx, at, u); got != want {
			t.Errorf("%s: connectedTo = %v, want %v", raw, got, want)
		}
	}
}

// The proxy in force for every request (here ALL_PROXY; Settings' and the
// system's are read the same way, through netproxy.For): the request
// through it goes as a vendor request with no proxy of its own would, and
// the direct one must go around it, or the two would always agree.
func TestProxyCheckGoesAroundTheGlobalProxyForDirect(t *testing.T) {
	client, proxyURL, asked := proxyCheckRig(t)
	t.Setenv("ALL_PROXY", proxyURL)
	t.Setenv("NO_PROXY", "")
	ctx := context.Background()
	got := checkProxy(ctx, client, ctx, true)
	if got.Through == nil || got.Through.IP != "203.0.113.9" {
		t.Fatalf("through the global proxy: %+v", got.Through)
	}
	if got.Direct.IP != "198.51.100.7" || *asked != 1 {
		t.Fatalf("direct: %+v, the proxy asked %d times (want 1)", got.Direct, *asked)
	}
}

func TestProxyCheckWithNoProxyAsksOnlyDirect(t *testing.T) {
	client, _, asked := proxyCheckRig(t)
	ctx := context.Background()
	got := checkProxy(ctx, client, ctx, false)
	if got.Through != nil {
		t.Fatalf("no proxy in force, yet a request through one: %+v", got.Through)
	}
	if got.Direct.IP != "198.51.100.7" || *asked != 0 {
		t.Fatalf("direct: %+v, proxy asked %d", got.Direct, *asked)
	}
}

func TestProxyCheckSaysAProxyDownWithoutItsPassword(t *testing.T) {
	client, _, _ := proxyCheckRig(t)
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	dead := l.Addr().String()
	l.Close() // refused from here on
	ctx := context.Background()
	got := checkProxy(ctx, client, netproxy.With(ctx, "http://me:s3cret-pw@"+dead), true)
	if got.Through == nil || got.Through.Error == "" || got.Through.IP != "" {
		t.Fatalf("a proxy that refuses: %+v", got.Through)
	}
	if strings.Contains(got.Through.Error, "s3cret-pw") {
		t.Fatalf("the error shows the proxy's password: %q", got.Through.Error)
	}
	if got.Direct.IP != "198.51.100.7" {
		t.Fatalf("direct still answers: %+v", got.Direct)
	}
}

func TestProxyCheckReadsNoAddressAsAFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		fmt.Fprint(rw, "<html>captive portal</html>")
	}))
	defer srv.Close()
	old := proxyTraceURL
	proxyTraceURL = srv.URL
	defer func() { proxyTraceURL = old }()
	if leg := traceLeg(context.Background(), srv.Client()); leg.Error == "" || leg.IP != "" {
		t.Fatalf("a page with no ip= line: %+v", leg)
	}
}

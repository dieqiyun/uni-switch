package netproxy

import (
	"net/http"
	"testing"
)

// http.DefaultClient's HTTP/2 connections (usage cards, catalogs,
// sign-ins) are checked as the gateway's are: one gone silent is pinged
// and dropped rather than given every request after it (#2843).
func TestInstallChecksHTTP2Connections(t *testing.T) {
	d, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		t.Skip("http.DefaultTransport is not an *http.Transport")
	}
	was, wasClient, wasProxy := d.HTTP2, http.DefaultClient.Transport, d.Proxy
	t.Cleanup(func() { d.HTTP2, http.DefaultClient.Transport, d.Proxy = was, wasClient, wasProxy })
	d.HTTP2 = nil
	Install()
	if d.HTTP2 == nil || d.HTTP2.SendPingTimeout != PingAfter || d.HTTP2.PingTimeout != PingWait {
		t.Fatalf("http.DefaultTransport's HTTP/2 after Install: %+v; want pings after %v, dropped after %v", d.HTTP2, PingAfter, PingWait)
	}
}

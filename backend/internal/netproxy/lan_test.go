package netproxy

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/settings"
)

// stubLookup answers names from m for the test, and forgets what earlier
// lookups found.
func stubLookup(t *testing.T, m map[string]string) {
	old := lanLookup
	lanLookup = func(_ context.Context, host string) ([]net.IPAddr, error) {
		ip, ok := m[host]
		if !ok {
			return nil, errors.New("no such host")
		}
		return []net.IPAddr{{IP: net.ParseIP(ip)}}, nil
	}
	clear(lanCache.m)
	t.Cleanup(func() { lanLookup = old; clear(lanCache.m) })
}

// #982: with a proxy set, a Remote uni-switch on the user's network is asked
// directly; a vendor still goes through the proxy, and so does a provider
// with a proxy of its own.
func TestLANGoesAroundTheProxy(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	settings.Save(settings.Settings{Proxy: "127.0.0.1:7890"})
	stubLookup(t, map[string]string{"server.lan": "192.168.131.20", "nas.tail1234.ts.net": "100.101.102.103", "api.openai.com": "162.159.140.245"})
	req := func(host, choice string) *http.Request {
		return (&http.Request{URL: &url.URL{Scheme: "http", Host: host}}).WithContext(With(context.Background(), choice))
	}
	for _, h := range []string{"192.168.131.20:3425", "10.0.0.5:3425", "172.20.1.1", "100.101.102.103:3425", "[fd7a:115c:a1e0::1]:3425", "server.lan:3425", "nas.tail1234.ts.net:3425", "mac-mini.local:3425"} {
		if p, err := Func(req(h, "")); p != nil || err != nil {
			t.Errorf("%s: through %v (%v), want direct", h, p, err)
		}
	}
	for _, h := range []string{"api.openai.com", "unresolvable.example", "8.8.8.8"} {
		if p, _ := Func(req(h, "")); p == nil || p.Host != "127.0.0.1:7890" {
			t.Errorf("%s: %v, want the proxy", h, p)
		}
	}
	if p, _ := Func(req("192.168.131.20:3425", "socks5://127.0.0.1:1080")); p == nil || p.Host != "127.0.0.1:1080" {
		t.Errorf("a provider's own proxy: %v", p)
	}
}

// The reporter's case end to end: a proxy that answers 502 Bad Gateway to
// whatever it is asked (as Clash does for a host it can't reach), set in
// uni-switch, and another uni-switch at this computer's LAN address. The list
// comes from the other uni-switch, not the proxy's 502.
func TestLANModelListNotThroughProxy(t *testing.T) {
	var lan net.IP
	addrs, _ := net.InterfaceAddrs()
	for _, a := range addrs {
		if n, ok := a.(*net.IPNet); ok && n.IP.To4() != nil && n.IP.IsPrivate() {
			lan = n.IP
			break
		}
	}
	if lan == nil {
		t.Skip("no private IPv4 address on this computer")
	}
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer proxy.Close()
	ln, err := net.Listen("tcp", net.JoinHostPort(lan.String(), "0"))
	if err != nil {
		t.Skip("can't listen on", lan, err)
	}
	remote := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer sk-uni-switch-key" {
			http.Error(w, "no API key was sent", http.StatusUnauthorized)
			return
		}
		io.WriteString(w, `{"object":"list","data":[{"id":"codex/gpt-6.1-sol"}]}`)
	}))
	remote.Listener = ln
	remote.Start()
	defer remote.Close()

	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	settings.Save(settings.Settings{Proxy: proxy.URL})
	clear(lanCache.m)
	c := &http.Client{Transport: Dispatch(&http.Transport{Proxy: Func})}
	req, _ := http.NewRequest("GET", remote.URL+"/v1/models", nil)
	req.Header.Set("Authorization", "Bearer sk-uni-switch-key")
	res, err := c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if res.StatusCode != 200 {
		t.Fatalf("%s: %s %s", remote.URL, res.Status, b)
	}
	if !strings.Contains(string(b), "codex/gpt-6.1-sol") {
		t.Fatalf("not the other uni-switch's list: %s", b)
	}
}

package netproxy

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/settings"
)

func TestParseScutil(t *testing.T) {
	out := `<dictionary> {
  ExceptionsList : <array> {
    0 : 127.0.0.1
    1 : *.local
  }
  HTTPEnable : 1
  HTTPPort : 7890
  HTTPProxy : 127.0.0.1
  HTTPSEnable : 1
  HTTPSPort : 7891
  HTTPSProxy : 127.0.0.1
  SOCKSEnable : 1
  SOCKSPort : 7892
  SOCKSProxy : 127.0.0.1
}`
	p := parseScutil(out)
	if p.URL != "http://127.0.0.1:7891" || len(p.Bypass) != 2 || p.Bypass[1] != "*.local" {
		t.Fatalf("%+v", p)
	}
	// each entry alone, for Codex's reqwest, which takes the HTTP one for
	// an http:// URL and never the HTTPS or SOCKS one
	if p.HTTP != "http://127.0.0.1:7890" || p.SOCKS != "socks5://127.0.0.1:7892" {
		t.Fatalf("entries: %+v", p)
	}
	if p := parseScutil("<dictionary> {\n  SOCKSEnable : 1\n  SOCKSPort : 1080\n  SOCKSProxy : 10.0.0.2\n}"); p.URL != "socks5://10.0.0.2:1080" || p.HTTP != "" || p.SOCKS != p.URL {
		t.Fatalf("socks: %+v", p)
	}
	if p := parseScutil("<dictionary> {\n  HTTPSEnable : 1\n  HTTPSPort : 7890\n  HTTPSProxy : 127.0.0.1\n}"); p.URL != "http://127.0.0.1:7890" || p.HTTP != "" {
		t.Fatalf("https only: %+v", p)
	}
	if p := parseScutil("<dictionary> {\n  HTTPEnable : 0\n  HTTPProxy : x\n}"); p.URL != "" {
		t.Fatalf("off: %+v", p)
	}
}

func TestParseWindows(t *testing.T) {
	for server, want := range map[string]string{
		"127.0.0.1:7890":                     "http://127.0.0.1:7890",
		"http=127.0.0.1:1;https=127.0.0.1:2": "http://127.0.0.1:2",
		"socks=127.0.0.1:1080":               "socks5://127.0.0.1:1080",
		"":                                   "",
	} {
		if got := parseWindows(server, "").URL; got != want {
			t.Errorf("%q: %q, want %q", server, got, want)
		}
	}
	// reqwest reads ProxyServer as one address: a list per protocol isn't one
	if p := parseWindows("127.0.0.1:7890", ""); p.HTTP != "http://127.0.0.1:7890" {
		t.Fatalf("one for all: %+v", p)
	}
	if p := parseWindows("http=127.0.0.1:1;https=127.0.0.1:2;socks=127.0.0.1:3", ""); p.HTTP != "" || p.SOCKS != "socks5://127.0.0.1:3" {
		t.Fatalf("per protocol: %+v", p)
	}
	p := parseWindows("127.0.0.1:7890", "*.corp;<local>")
	if !bypassed("git.corp", p.Bypass) || !bypassed("intranet", p.Bypass) || bypassed("chatgpt.com", p.Bypass) {
		t.Fatalf("bypass: %+v", p.Bypass)
	}
}

func TestFor(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	for _, k := range []string{"HTTPS_PROXY", "https_proxy", "HTTP_PROXY", "http_proxy", "ALL_PROXY", "all_proxy", "NO_PROXY", "no_proxy"} {
		t.Setenv(k, "")
	}
	sysCache.at, sysCache.p = farFuture, Proxy{URL: "http://127.0.0.1:7890", Bypass: []string{"*.corp"}}
	t.Cleanup(func() { sysCache.at = zero })

	u, _ := url.Parse("https://chatgpt.com/backend-api/codex/responses")
	if p, _ := For(u); p == nil || p.String() != "http://127.0.0.1:7890" {
		t.Fatalf("system: %v", p)
	}
	corp, _ := url.Parse("https://git.corp/x")
	if p, _ := For(corp); p != nil {
		t.Fatalf("bypass: %v", p)
	}
	local := &http.Request{URL: &url.URL{Scheme: "http", Host: "127.0.0.1:3425"}}
	if p, _ := Func(local); p != nil {
		t.Fatalf("loopback: %v", p)
	}

	// uni-switch's own setting wins; "direct" turns it off
	settings.Save(settings.Settings{Proxy: "socks5://127.0.0.1:1080"})
	if p, _ := For(u); p == nil || p.String() != "socks5://127.0.0.1:1080" {
		t.Fatalf("setting: %v", p)
	}
	settings.Save(settings.Settings{Proxy: "direct"})
	if p, _ := For(u); p != nil {
		t.Fatalf("direct: %v", p)
	}
	if err := settings.Save(settings.Settings{Proxy: "ftp://x"}); err == nil {
		t.Fatal("ftp proxy accepted")
	}
	if settings.Save(settings.Settings{Proxy: "127.0.0.1:7890"}) != nil || filepath.Dir(settings.Path()) != filepath.Join(dir, "uni-switch") {
		t.Fatal("host:port proxy refused")
	}
}

func TestEnv(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	sysCache.at, sysCache.p = farFuture, Proxy{URL: "http://127.0.0.1:7890", Bypass: []string{"*.corp", "<local>"}}
	t.Cleanup(func() { sysCache.at = zero })
	get := func(env []string, k string) string {
		v := ""
		for _, e := range env {
			if key, val, _ := strings.Cut(e, "="); key == k {
				v = val
			}
		}
		return v
	}

	// the system's proxy, where the environment names none
	env := Env([]string{"PATH=/bin", "HOME=/h"})
	if get(env, "HTTPS_PROXY") != "http://127.0.0.1:7890" || get(env, "all_proxy") != "http://127.0.0.1:7890" ||
		get(env, "NO_PROXY") != "localhost,127.0.0.1,::1,.corp" || get(env, "PATH") != "/bin" {
		t.Fatalf("system: %v", env)
	}
	// one the environment names is kept
	env = Env([]string{"https_proxy=http://10.0.0.1:1", "NO_PROXY=x"})
	if len(env) != 2 || get(env, "https_proxy") != "http://10.0.0.1:1" {
		t.Fatalf("environment: %v", env)
	}
	// uni-switch's own replaces the environment's
	settings.Save(settings.Settings{Proxy: "127.0.0.1:6152"})
	env = Env([]string{"HTTPS_PROXY=http://10.0.0.1:1", "PATH=/bin"})
	if get(env, "HTTPS_PROXY") != "http://127.0.0.1:6152" || get(env, "http_proxy") != "http://127.0.0.1:6152" ||
		get(env, "NO_PROXY") != "localhost,127.0.0.1,::1" || len(env) != 9 {
		t.Fatalf("setting: %v", env)
	}
	// and "direct" leaves none
	settings.Save(settings.Settings{Proxy: "direct"})
	if env = Env([]string{"HTTPS_PROXY=http://10.0.0.1:1", "PATH=/bin"}); len(env) != 1 {
		t.Fatalf("direct: %v", env)
	}
}

var farFuture = time.Now().Add(time.Hour)
var zero time.Time

// A provider's own proxy (#237) wins over the global one, "direct" over
// both, and each proxy is given one transport of its own, kept.
func TestWith(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	settings.Save(settings.Settings{Proxy: "127.0.0.1:6152"})
	u := &url.URL{Scheme: "https", Host: "api.openai.com"}
	req := func(choice string) *http.Request {
		return (&http.Request{URL: u}).WithContext(With(context.Background(), choice))
	}
	if p, _ := Func(req("")); p == nil || p.String() != "http://127.0.0.1:6152" {
		t.Fatalf("follow: %v", p)
	}
	if p, _ := Func(req("socks5://127.0.0.1:1080")); p == nil || p.String() != "socks5://127.0.0.1:1080" {
		t.Fatalf("own: %v", p)
	}
	if p, _ := Func(req("direct")); p != nil {
		t.Fatalf("direct: %v", p)
	}
	// a request for a provider that follows the global proxy, made while
	// serving one set to its own or to none, follows the global one
	// (#1522: Codex's search inside a turn of a provider set to direct)
	for _, outer := range []string{"direct", "socks5://127.0.0.1:1080"} {
		inner := (&http.Request{URL: u}).WithContext(With(With(context.Background(), outer), ""))
		if p, _ := Func(inner); p == nil || p.String() != "http://127.0.0.1:6152" {
			t.Fatalf("follow inside %s: %v", outer, p)
		}
		if c := Choice(inner.Context()); c != "" {
			t.Fatalf("follow inside %s: choice %q", outer, c)
		}
	}
	local := (&http.Request{URL: &url.URL{Scheme: "http", Host: "127.0.0.1:3425"}}).WithContext(With(context.Background(), "127.0.0.1:1"))
	if p, _ := Func(local); p != nil {
		t.Fatalf("loopback: %v", p)
	}

	d := Dispatch(&http.Transport{Proxy: Func}).(*dispatch)
	a, b := d.transport(route{choice: "127.0.0.1:1"}), d.transport(route{choice: "direct"})
	if a == b || d.transport(route{choice: "127.0.0.1:1"}) != a || len(d.own) != 2 {
		t.Fatalf("transports: %d kept", len(d.own))
	}
	if p, _ := a.Proxy(req("")); p == nil || p.Host != "127.0.0.1:1" {
		t.Fatalf("own transport: %v", p)
	}
	if p, _ := b.Proxy(req("")); p != nil {
		t.Fatalf("direct transport: %v", p)
	}

	get := func(env []string, k string) string {
		for _, e := range env {
			if key, val, _ := strings.Cut(e, "="); key == k {
				return val
			}
		}
		return ""
	}
	if env := EnvWith("socks5://127.0.0.1:1080", []string{"PATH=/bin"}); get(env, "HTTPS_PROXY") != "socks5://127.0.0.1:1080" {
		t.Fatalf("own env: %v", env)
	}
	if env := EnvWith("direct", []string{"HTTPS_PROXY=http://x:1", "PATH=/bin"}); len(env) != 1 {
		t.Fatalf("direct env: %v", env)
	}
	if env := EnvWith("", []string{"PATH=/bin"}); get(env, "HTTPS_PROXY") != "http://127.0.0.1:6152" {
		t.Fatalf("follow env: %v", env)
	}
}

// A request HTTP1 names goes over HTTP/1.1, its TLS hello offering no h2,
// though its transport has already spoken HTTP/2 to that server (which
// leaves h2 in the TLS config a clone copies: a server that took it would
// be sent HTTP/1.1 on an HTTP/2 connection), and with a provider's own
// proxy too. Every other request still gets HTTP/2.
func TestDispatchHTTP1(t *testing.T) {
	var mu sync.Mutex
	offered := map[string]bool{} // the client's address → its hello offered h2
	var got []string
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		got = append(got, r.URL.Path+" "+r.Proto+map[bool]string{true: " (h2 offered)"}[offered[r.RemoteAddr]])
	}))
	srv.EnableHTTP2 = true
	srv.TLS = &tls.Config{GetConfigForClient: func(h *tls.ClientHelloInfo) (*tls.Config, error) {
		mu.Lock()
		defer mu.Unlock()
		offered[h.Conn.RemoteAddr().String()] = slices.Contains(h.SupportedProtos, "h2")
		return nil, nil
	}}
	srv.StartTLS()
	t.Cleanup(srv.Close)
	pool := x509.NewCertPool()
	pool.AddCert(srv.Certificate())
	d := Dispatch(&http.Transport{Proxy: Func, ForceAttemptHTTP2: true, TLSClientConfig: &tls.Config{RootCAs: pool}}).(*dispatch)
	t.Cleanup(d.CloseIdleConnections)
	was := HTTP1
	HTTP1 = func(u *url.URL) bool { return strings.HasPrefix(u.Path, "/h1") }
	t.Cleanup(func() { HTTP1 = was })
	for _, c := range []struct {
		path   string
		choice string
	}{{"/h2", ""}, {"/h1", ""}, {"/h1/own", "direct"}, {"/h2/own", "direct"}} {
		req, _ := http.NewRequestWithContext(With(context.Background(), c.choice), http.MethodGet, srv.URL+c.path, nil)
		res, err := d.RoundTrip(req)
		if err != nil {
			t.Fatalf("%s: %v", c.path, err)
		}
		io.Copy(io.Discard, res.Body)
		res.Body.Close()
	}
	want := []string{"/h2 HTTP/2.0 (h2 offered)", "/h1 HTTP/1.1", "/h1/own HTTP/1.1", "/h2/own HTTP/2.0 (h2 offered)"}
	if !slices.Equal(got, want) {
		t.Fatalf("asked\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

// A switch of the global proxy leaves the connections its transport kept
// through the old one behind: an HTTP/2 connection is pooled by the
// target's authority alone, so the request after the save rode the old
// proxy's connection and left by the old exit while the new proxy was
// the one shown (#4523). The dispatch closes the idle connections when
// the setting moves, so the request after it dials through the proxy in
// force.
func TestGlobalSwitchDropsTheOldProxyConnections(t *testing.T) {
	clearProxyEnv(t)
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "exit") }))
	srv.EnableHTTP2 = true
	srv.StartTLS()
	t.Cleanup(srv.Close)
	pool := x509.NewCertPool()
	pool.AddCert(srv.Certificate())
	a, na := fakeSOCKS(t, "", "")
	b, nb := fakeSOCKS(t, "", "")
	d := Dispatch(&http.Transport{
		// For, not Func: the test servers live on the loopback Func skips
		Proxy:             func(r *http.Request) (*url.URL, error) { return For(r.URL) },
		ForceAttemptHTTP2: true,
		TLSClientConfig:   &tls.Config{RootCAs: pool},
	}).(*dispatch)
	t.Cleanup(d.CloseIdleConnections)
	c := &http.Client{Transport: d, Timeout: 10 * time.Second}
	get := func() {
		res, err := c.Get(srv.URL)
		if err != nil {
			t.Fatalf("through the proxy: %v", err)
		}
		io.Copy(io.Discard, res.Body)
		res.Body.Close()
		if res.ProtoMajor != 2 {
			t.Fatalf("the request went over %s, want HTTP/2", res.Proto)
		}
	}
	if err := settings.Save(settings.Settings{Proxy: a.String()}); err != nil {
		t.Fatal(err)
	}
	get()
	if na.Load() != 1 || nb.Load() != 0 {
		t.Fatalf("the first request: %d connections through A, %d through B", na.Load(), nb.Load())
	}
	if err := settings.Save(settings.Settings{Proxy: b.String()}); err != nil {
		t.Fatal(err)
	}
	get()
	if nb.Load() != 1 || na.Load() != 1 {
		t.Fatalf("after the switch: %d connections through A, %d through B, want the fresh dial through B and no new one through A", na.Load(), nb.Load())
	}
}

// Each check Watch hands out keeps its own last value: a move is seen,
// once, by every check, on the first call it makes after it (#4824).
func TestWatchSeesTheGlobalProxyMove(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	w := Watch()
	settings.Save(settings.Settings{Proxy: "socks5://127.0.0.1:1080"})
	if w() {
		t.Fatal("the first call sees a move")
	}
	settings.Save(settings.Settings{Proxy: "direct"})
	if !w() {
		t.Fatal("the move went unseen")
	}
	if w() {
		t.Fatal("the setting standing is a move the second time")
	}
	v := Watch()
	if v() {
		t.Fatal("a check of its own starts in the middle")
	}
}

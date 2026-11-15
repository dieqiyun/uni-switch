package netproxy

import (
	"encoding/binary"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"slices"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// fakeSOCKS is a SOCKS5 proxy taking user u and password pw ("" for none),
// counting the connections it carried.
func fakeSOCKS(t *testing.T, u, pw string) (*url.URL, *atomic.Int32) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	var n atomic.Int32
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				var h [2]byte
				io.ReadFull(c, h[:])
				ms := make([]byte, h[1])
				io.ReadFull(c, ms)
				if u != "" {
					c.Write([]byte{5, 2})
					var v [2]byte
					io.ReadFull(c, v[:])
					got := make([]byte, v[1])
					io.ReadFull(c, got)
					var l [1]byte
					io.ReadFull(c, l[:])
					p := make([]byte, l[0])
					io.ReadFull(c, p)
					if string(got) != u || string(p) != pw {
						c.Write([]byte{1, 1})
						return
					}
					c.Write([]byte{1, 0})
				} else {
					c.Write([]byte{5, 0})
				}
				var r [4]byte
				io.ReadFull(c, r[:])
				var host string
				switch r[3] {
				case 1:
					b := make([]byte, 4)
					io.ReadFull(c, b)
					host = net.IP(b).String()
				case 3:
					var l [1]byte
					io.ReadFull(c, l[:])
					b := make([]byte, l[0])
					io.ReadFull(c, b)
					host = string(b)
				}
				var pb [2]byte
				io.ReadFull(c, pb[:])
				up, err := net.Dial("tcp", net.JoinHostPort(host, strconv.Itoa(int(binary.BigEndian.Uint16(pb[:])))))
				if err != nil {
					c.Write([]byte{5, 5, 0, 1, 0, 0, 0, 0, 0, 0})
					return
				}
				defer up.Close()
				n.Add(1)
				c.Write([]byte{5, 0, 0, 1, 127, 0, 0, 1, 0, 0})
				go io.Copy(up, c)
				io.Copy(c, up)
			}(c)
		}
	}()
	s := &url.URL{Scheme: "socks5", Host: ln.Addr().String()}
	if u != "" {
		s.User = url.UserPassword(u, pw)
	}
	return s, &n
}

// A plugin's request goes through a SOCKS5 proxy as a built-in's does:
// the bridge carries a plain http:// request and an https:// one's
// CONNECT tunnel through it, signing in to it when it asks.
func TestBridgeCarriesThroughSOCKS(t *testing.T) {
	plain := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "plain "+r.URL.Path) }))
	defer plain.Close()
	tls := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "tls "+r.URL.Path) }))
	defer tls.Close()
	for _, auth := range []bool{false, true} {
		u, pw := "", ""
		if auth {
			u, pw = "me", "secret"
		}
		socks, n := fakeSOCKS(t, u, pw)
		b, err := Bridge(socks)
		if err != nil {
			t.Fatal(err)
		}
		if again, _ := Bridge(socks); again != b {
			t.Fatalf("a second bridge %s for the same proxy, not %s", again, b)
		}
		if ForBun(socks.String()) != b || !strings.HasPrefix(b, "http://127.0.0.1:") {
			t.Fatalf("ForBun(%s) = %s, want %s", socks, ForBun(socks.String()), b)
		}
		pu, _ := url.Parse(b)
		tr := tls.Client().Transport.(*http.Transport).Clone()
		tr.Proxy = http.ProxyURL(pu)
		c := &http.Client{Transport: tr}
		for _, x := range []struct{ url, want string }{{plain.URL + "/a", "plain /a"}, {tls.URL + "/b", "tls /b"}} {
			res, err := c.Get(x.url)
			if err != nil {
				t.Fatalf("auth=%v %s: %v", auth, x.url, err)
			}
			got, _ := io.ReadAll(res.Body)
			res.Body.Close()
			if string(got) != x.want {
				t.Fatalf("auth=%v %s: %q, want %q", auth, x.url, got, x.want)
			}
		}
		if n.Load() != 2 {
			t.Fatalf("auth=%v: the SOCKS proxy carried %d connections, want 2", auth, n.Load())
		}
	}
	for _, p := range []string{"http://127.0.0.1:7890", "direct", ""} {
		if got := ForBun(p); got != p {
			t.Fatalf("ForBun(%q) = %q, want it as it is", p, got)
		}
	}
}

// A SOCKS proxy that isn't there is said as Go's transport says it, so
// the gateway takes it for the proxy's failure, not the vendor's.
func TestBridgeSaysTheProxyIsDown(t *testing.T) {
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	addr := ln.Addr().String()
	ln.Close()
	b, err := Bridge(&url.URL{Scheme: "socks5h", Host: addr})
	if err != nil {
		t.Fatal(err)
	}
	c, err := net.Dial("tcp", strings.TrimPrefix(b, "http://"))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	io.WriteString(c, "CONNECT example.com:443 HTTP/1.1\r\nHost: example.com:443\r\n\r\n")
	got, _ := io.ReadAll(c)
	if !strings.HasPrefix(string(got), "HTTP/1.1 502") || !strings.Contains(string(got), "socks connect tcp "+addr) {
		t.Fatalf("answered %q", got)
	}
}

// A bridge handed the port of the SOCKS proxy it carries to, which a port
// just let go can be (Linux CI gave TestPluginSOCKSProxy's dead proxy port
// to its bridge), listens elsewhere: carrying to itself, it held each
// request for a minute instead of saying the proxy is down.
func TestBridgeNeverCarriesToItself(t *testing.T) {
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	addr := ln.Addr().String()
	ln.Close()
	first := true
	listenLoopback = func() (net.Listener, error) {
		if first {
			first = false
			return net.Listen("tcp", addr)
		}
		return net.Listen("tcp", "127.0.0.1:0")
	}
	t.Cleanup(func() { listenLoopback = func() (net.Listener, error) { return net.Listen("tcp", "127.0.0.1:0") } })
	for _, host := range []string{addr, "localhost:" + strings.Split(addr, ":")[1]} {
		b, err := Bridge(&url.URL{Scheme: "socks5h", Host: host})
		if err != nil {
			t.Fatal(err)
		}
		if strings.TrimPrefix(b, "http://") == addr {
			t.Fatalf("the bridge for %s listens at %s, the proxy's own port", host, b)
		}
		c, err := net.Dial("tcp", strings.TrimPrefix(b, "http://"))
		if err != nil {
			t.Fatal(err)
		}
		c.SetDeadline(time.Now().Add(10 * time.Second))
		io.WriteString(c, "CONNECT example.com:443 HTTP/1.1\r\nHost: example.com:443\r\n\r\n")
		got, _ := io.ReadAll(c)
		c.Close()
		if !strings.HasPrefix(string(got), "HTTP/1.1 502") {
			t.Fatalf("through %s: answered %q", host, got)
		}
		first = true
	}
}

// Bun itself, the plugin host's runtime, fetches through the bridge: run
// when a bun is at hand ($UNI_SWITCH_BUN or on PATH).
func TestBunThroughBridge(t *testing.T) {
	bun := os.Getenv("UNI_SWITCH_BUN")
	if bun == "" {
		bun, _ = exec.LookPath("bun")
	}
	if bun == "" {
		t.Skip("no bun")
	}
	plain := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "plain") }))
	defer plain.Close()
	tls := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "tls") }))
	defer tls.Close()
	socks, n := fakeSOCKS(t, "me", "pw")
	b, _ := Bridge(socks)
	// loopback is never proxied by uni-switch, but the test's servers are on it:
	// Bun is given the proxy outright
	js := `for (const u of process.env.URLS.split(" ")) console.log(await (await fetch(u, { proxy: "` + b + `", tls: { rejectUnauthorized: false } })).text())`
	cmd := exec.Command(bun, "-e", js)
	cmd.Env = append(os.Environ(), "URLS="+plain.URL+" "+tls.URL)
	out, err := cmd.CombinedOutput()
	if err != nil || strings.TrimSpace(string(out)) != "plain\ntls" {
		t.Fatalf("bun: %v %s", err, out)
	}
	if n.Load() != 2 {
		t.Fatalf("the SOCKS proxy carried %d connections, want 2", n.Load())
	}
}

// A Mac with only a SOCKS system proxy (#1409): what Env gives a bun
// names the bridge in front of it, so bun add doesn't fail as
// UnsupportedProxyProtocol, and the bridge carries through the SOCKS
// proxy. An http:// proxy, and the rest of env, are left as they are.
func TestEnvForBunBridgesSystemSOCKS(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	socks, n := fakeSOCKS(t, "", "")
	sysCache.at, sysCache.p = farFuture, Proxy{URL: socks.String()}
	t.Cleanup(func() { sysCache.at = zero })

	env := EnvForBun(Env([]string{"PATH=/bin", "HOME=/h"}))
	var bridge string
	for _, kv := range env {
		k, v, _ := strings.Cut(kv, "=")
		switch strings.ToUpper(k) {
		case "HTTPS_PROXY", "HTTP_PROXY", "ALL_PROXY":
			if !strings.HasPrefix(v, "http://127.0.0.1:") || (bridge != "" && v != bridge) {
				t.Fatalf("bun is given %s", kv)
			}
			bridge = v
		}
	}
	if bridge == "" || !slices.Contains(env, "PATH=/bin") || !slices.Contains(env, "HOME=/h") {
		t.Fatalf("env: %q", env)
	}
	plain := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "ok") }))
	defer plain.Close()
	pu, _ := url.Parse(bridge)
	c := &http.Client{Transport: &http.Transport{Proxy: http.ProxyURL(pu)}}
	res, err := c.Get(plain.URL)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if string(b) != "ok" || n.Load() != 1 {
		t.Fatalf("through the bridge: %q, the SOCKS proxy carried %d", b, n.Load())
	}

	httpEnv := []string{"HTTPS_PROXY=http://127.0.0.1:7890", "all_proxy=", "NO_PROXY=localhost"}
	if got := EnvForBun(httpEnv); strings.Join(got, "\n") != strings.Join(httpEnv, "\n") {
		t.Fatalf("an http proxy: %q", got)
	}
}

// #2651: a Bun-built CLI run on a provider's behalf (Claude Code) is given
// the bridge in front of the provider's SOCKS proxy, the bridge carrying
// to it. Where the CLI can't reach the bridge (WSL under NAT), or the
// bridge can't start, it is an error at once, never socks5:// handed on
// for the CLI to fail with on every try.
func TestEnvForBunWithBridgesTheProvidersSOCKS(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	socks, n := fakeSOCKS(t, "", "")
	env, err := EnvForBunWith(socks.String(), []string{"PATH=/bin", "HTTPS_PROXY=http://old:1"}, true)
	if err != nil {
		t.Fatal(err)
	}
	var bridge string
	for _, kv := range env {
		k, v, _ := strings.Cut(kv, "=")
		switch strings.ToUpper(k) {
		case "HTTPS_PROXY", "HTTP_PROXY", "ALL_PROXY":
			if !strings.HasPrefix(v, "http://127.0.0.1:") {
				t.Fatalf("the CLI is given %s", kv)
			}
			bridge = v
		}
	}
	if bridge == "" || !slices.Contains(env, "PATH=/bin") {
		t.Fatalf("env: %q", env)
	}
	plain := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "ok") }))
	defer plain.Close()
	pu, _ := url.Parse(bridge)
	res, err := (&http.Client{Transport: &http.Transport{Proxy: http.ProxyURL(pu)}}).Get(plain.URL)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if string(b) != "ok" || n.Load() != 1 {
		t.Fatalf("through the bridge: %q, the SOCKS proxy carried %d", b, n.Load())
	}

	// an http:// proxy, and "direct", are EnvWith's as they are
	for _, c := range []string{"http://127.0.0.1:7890", "direct"} {
		got, err := EnvForBunWith(c, []string{"PATH=/bin"}, false)
		if want := EnvWith(c, []string{"PATH=/bin"}); err != nil || strings.Join(got, "\n") != strings.Join(want, "\n") {
			t.Fatalf("%s: %q %v, want %q", c, got, err, want)
		}
	}

	if _, err := EnvForBunWith("socks5://u:secret@10.0.0.2:1080", []string{"PATH=/bin"}, false); err == nil ||
		!strings.Contains(err.Error(), "UnsupportedProxyProtocol") || strings.Contains(err.Error(), "secret") {
		t.Fatalf("out of the bridge's reach: %v", err)
	}
	old := listenLoopback
	listenLoopback = func() (net.Listener, error) { return nil, io.ErrClosedPipe }
	t.Cleanup(func() { listenLoopback = old })
	if _, err := EnvForBunWith("socks5://10.0.0.3:1080", []string{"PATH=/bin"}, true); err == nil || !strings.Contains(err.Error(), "didn't start") {
		t.Fatalf("no bridge: %v", err)
	}
}

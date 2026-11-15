package netproxy

import (
	"encoding/base64"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/settings"
)

// The login Settings › Proxy saves (微雨南山 on Discord): a username and
// a password typed in fields of their own, each percent-encoded into the
// address's userinfo as the page's proxyAddress does
// (encodeURIComponent). These are the page's own bytes for them.
const (
	loginUser    = "me@corp:x"
	loginPass    = "p@ss:w/rd% #1 x"
	loginUserEnc = "me%40corp%3Ax"
	loginPassEnc = "p%40ss%3Aw%2Frd%25%20%231%20x"
)

func clearProxyEnv(t *testing.T) {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	for _, k := range []string{"HTTPS_PROXY", "https_proxy", "HTTP_PROXY", "http_proxy", "ALL_PROXY", "all_proxy", "NO_PROXY", "no_proxy"} {
		t.Setenv(k, "")
	}
	sysCache.at, sysCache.p = farFuture, Proxy{}
	t.Cleanup(func() { sysCache.at = zero })
}

// fakeHTTPProxy is an HTTP proxy that carries a request only with the
// user and password given, answering 407 otherwise.
func fakeHTTPProxy(t *testing.T, user, pass string) *httptest.Server {
	t.Helper()
	want := "Basic " + base64.StdEncoding.EncodeToString([]byte(user+":"+pass))
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Proxy-Authorization") != want {
			w.Header().Set("Proxy-Authenticate", `Basic realm="fake"`)
			http.Error(w, "proxy login refused", http.StatusProxyAuthRequired)
			return
		}
		io.WriteString(w, "carried "+r.URL.String())
	}))
	t.Cleanup(srv.Close)
	return srv
}

// throughSaved gets target through the proxy settings name, as a vendor
// request takes it (For; Func skips the test servers' loopback).
func throughSaved(t *testing.T, target string) (string, int) {
	t.Helper()
	tr := &http.Transport{Proxy: func(r *http.Request) (*url.URL, error) { return For(r.URL) }}
	defer tr.CloseIdleConnections()
	res, err := (&http.Client{Transport: tr}).Get(target)
	if err != nil {
		t.Fatalf("GET %s through the saved proxy: %v", target, err)
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	return string(b), res.StatusCode
}

// A password holding @ : / % # and spaces, saved as the Settings page
// saves it, reads back as typed and signs in to an HTTP proxy and a
// SOCKS5 one; one off by a character is refused, so the fakes check it.
// Nothing shown of the address carries either part of the login.
func TestProxyLoginWithReservedCharacters(t *testing.T) {
	vendor := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "vendor "+r.URL.Path) }))
	defer vendor.Close()

	t.Run("http", func(t *testing.T) {
		clearProxyEnv(t)
		p := fakeHTTPProxy(t, loginUser, loginPass)
		saved := "http://" + loginUserEnc + ":" + loginPassEnc + "@" + strings.TrimPrefix(p.URL, "http://")
		if err := settings.Save(settings.Settings{Proxy: saved}); err != nil {
			t.Fatal(err)
		}
		if got := settings.Load().Proxy; got != saved {
			t.Fatalf("saved %q, read back %q", saved, got)
		}
		u, _ := url.Parse("http://vendor.example/v1/models")
		pu, err := For(u)
		if err != nil || pu == nil {
			t.Fatalf("For = %v, %v", pu, err)
		}
		if pw, _ := pu.User.Password(); pu.User.Username() != loginUser || pw != loginPass {
			t.Fatalf("login read back as %q / %q", pu.User.Username(), pw)
		}
		if body, code := throughSaved(t, "http://vendor.example/v1/models"); code != 200 || body != "carried http://vendor.example/v1/models" {
			t.Fatalf("through the HTTP proxy: %d %q", code, body)
		}
		// a CLI uni-switch starts gets the same login in its *_PROXY
		for _, e := range EnvWith("", []string{}) {
			if k, v, _ := strings.Cut(e, "="); k == "HTTPS_PROXY" {
				eu, err := url.Parse(v)
				if err != nil {
					t.Fatal(err)
				}
				if pw, _ := eu.User.Password(); eu.User.Username() != loginUser || pw != loginPass {
					t.Fatalf("HTTPS_PROXY's login is %q / %q", eu.User.Username(), pw)
				}
			}
		}
		// one character off is refused
		wrong := "http://" + loginUserEnc + ":" + strings.Replace(loginPassEnc, "%23", "%2523", 1) + "@" + strings.TrimPrefix(p.URL, "http://")
		if err := settings.Save(settings.Settings{Proxy: wrong}); err != nil {
			t.Fatal(err)
		}
		if _, code := throughSaved(t, "http://vendor.example/"); code != http.StatusProxyAuthRequired {
			t.Fatalf("a wrong password was carried: %d", code)
		}
		assertHidden(t)
	})

	t.Run("socks5", func(t *testing.T) {
		clearProxyEnv(t)
		socks, n := fakeSOCKS(t, loginUser, loginPass)
		saved := "socks5://" + loginUserEnc + ":" + loginPassEnc + "@" + socks.Host
		if err := settings.Save(settings.Settings{Proxy: saved}); err != nil {
			t.Fatal(err)
		}
		// Go's transport speaks SOCKS5 itself
		if body, code := throughSaved(t, vendor.URL+"/go"); code != 200 || body != "vendor /go" {
			t.Fatalf("through the SOCKS5 proxy: %d %q", code, body)
		}
		// and the plugin host's Bun goes through the bridge, whose own
		// handshake (socksHandshake) sends the login
		pu, err := Parse(saved)
		if err != nil {
			t.Fatal(err)
		}
		b, err := Bridge(pu)
		if err != nil {
			t.Fatal(err)
		}
		bu, _ := url.Parse(b)
		res, err := (&http.Client{Transport: &http.Transport{Proxy: http.ProxyURL(bu)}}).Get(vendor.URL + "/bridge")
		if err != nil {
			t.Fatal(err)
		}
		got, _ := io.ReadAll(res.Body)
		res.Body.Close()
		if string(got) != "vendor /bridge" {
			t.Fatalf("through the bridge: %d %q", res.StatusCode, got)
		}
		if n.Load() != 2 {
			t.Fatalf("the SOCKS proxy carried %d connections, want 2", n.Load())
		}
		wrong := "socks5://" + loginUserEnc + ":" + strings.TrimSuffix(loginPassEnc, "x") + "@" + socks.Host
		if err := settings.Save(settings.Settings{Proxy: wrong}); err != nil {
			t.Fatal(err)
		}
		tr := &http.Transport{Proxy: func(r *http.Request) (*url.URL, error) { return For(r.URL) }}
		defer tr.CloseIdleConnections()
		if _, err := (&http.Client{Transport: tr}).Get(vendor.URL + "/no"); err == nil {
			t.Fatal("a wrong SOCKS password was carried")
		}
		assertHidden(t)
	})
}

// assertHidden: what Settings shows of the saved proxy names neither
// part of the login, decoded or not.
func assertHidden(t *testing.T) {
	t.Helper()
	shown, source := Describe()
	for _, secret := range []string{loginUser, loginPass, loginUserEnc, loginPassEnc, "p@ss", "p%40ss", "corp"} {
		if strings.Contains(shown, secret) || strings.Contains(Redact(settings.Load().Proxy), secret) {
			t.Fatalf("Describe (%s) shows %q: %q", source, secret, shown)
		}
	}
}

// Bun, which the plugin host's fetches and Claude Code run on, signs in
// to an HTTP proxy with the same login: it decodes the userinfo as Go
// does. Run when a bun is at hand ($UNI_SWITCH_BUN or on PATH).
func TestBunProxyLoginWithReservedCharacters(t *testing.T) {
	bun := os.Getenv("UNI_SWITCH_BUN")
	if bun == "" {
		bun, _ = exec.LookPath("bun")
	}
	if bun == "" {
		t.Skip("no bun")
	}
	p := fakeHTTPProxy(t, loginUser, loginPass)
	proxy := "http://" + loginUserEnc + ":" + loginPassEnc + "@" + strings.TrimPrefix(p.URL, "http://")
	js := `console.log(await (await fetch("http://vendor.example/x", { proxy: process.env.P })).text())`
	cmd := exec.Command(bun, "-e", js)
	cmd.Env = append(os.Environ(), "P="+proxy)
	out, err := cmd.CombinedOutput()
	if err != nil || strings.TrimSpace(string(out)) != "carried http://vendor.example/x" {
		t.Fatalf("bun through the HTTP proxy: %v %s", err, out)
	}
}

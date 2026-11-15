package gateway

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// A web page whose site's DNS turns to 127.0.0.1 after it loads (DNS
// rebinding) calls the gateway as its own origin: a GET carries no Origin,
// a POST one equal to the Host, and on loopback no key is asked. Such a
// browser request under a name that isn't this computer's is refused; an
// agent, a browser on localhost or an IP, a container runtime's name for
// the host, and a call with an enabled gateway key go through as before.
func TestReboundPageRefused(t *testing.T) {
	fresh(t)
	t.Setenv("UNI_SWITCH_ADDR", "")
	t.Setenv("UNI_SWITCH_PUBLIC_URL", "")
	reached := false
	h := localGuard(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { reached = true }))
	call := func(method, host string, hdr ...string) int {
		reached = false
		r := httptest.NewRequest(method, "/v1/models", nil)
		r.RemoteAddr = "127.0.0.1:51234"
		r.Host = host
		for i := 0; i+1 < len(hdr); i += 2 {
			r.Header.Set(hdr[i], hdr[i+1])
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code == http.StatusForbidden && !strings.Contains(w.Body.String(), "localhost or 127.0.0.1") {
			t.Fatalf("%s %s: %q", method, host, w.Body.String())
		}
		return w.Code
	}
	chrome := []string{"User-Agent", "Mozilla/5.0 (Macintosh) Chrome/140", "Sec-Fetch-Site", "same-origin", "Sec-Fetch-Mode", "cors"}
	evil := "rebind.attacker.example:3425"

	if c := call("GET", evil, chrome...); c != http.StatusForbidden || reached {
		t.Fatal("a rebound page's GET:", c)
	}
	if c := call("POST", evil, append([]string{"Origin", "http://" + evil}, chrome...)...); c != http.StatusForbidden || reached {
		t.Fatal("a rebound page's POST:", c)
	}
	// Safari before Sec-Fetch: the user agent alone
	if c := call("GET", evil, "User-Agent", "Mozilla/5.0 (Macintosh) Safari/605"); c != http.StatusForbidden || reached {
		t.Fatal("an older browser:", c)
	}
	for _, host := range []string{"127.0.0.1:3425", "localhost:3425", "[::1]:3425", "host.docker.internal:3425", "LOCALHOST.:3425"} {
		if c := call("GET", host, chrome...); c != 200 || !reached {
			t.Fatal("a browser at", host, c)
		}
	}
	// a subdomain of localhost is a name Safari asks the DNS for (#2256)
	if c := call("GET", "wails.localhost", chrome...); c != http.StatusForbidden || reached {
		t.Fatal("a browser at a localhost subdomain:", c)
	}
	// an agent sends no browser marks, whatever name it was given
	if c := call("POST", evil, "Authorization", "Bearer uni-switch", "User-Agent", "claude-cli/2.1"); c != 200 || !reached {
		t.Fatal("an agent:", c)
	}
	t.Setenv("UNI_SWITCH_PUBLIC_URL", "https://uni-switch.example.net")
	if c := call("GET", "uni-switch.example.net", chrome...); c != http.StatusForbidden || reached {
		t.Fatal("the public URL's host:", c)
	}
	_, secrets := newCaller(t, "Page")
	if c := call("GET", evil, append([]string{"Authorization", "Bearer " + secrets[0]}, chrome...)...); c != 200 || !reached {
		t.Fatal("with an enabled gateway key:", c)
	}
	if c := call("GET", evil, append([]string{"Authorization", "Bearer sk-uni-switch-wrong"}, chrome...)...); c != http.StatusForbidden || reached {
		t.Fatal("with a wrong key:", c)
	}
}

package gateway

import (
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/settings"
)

// ownIPv4 is an IPv4 address of one of this computer's interfaces that
// isn't loopback, "" when it has none.
func ownIPv4() string {
	ifs, _ := net.Interfaces()
	for _, i := range ifs {
		if i.Flags&net.FlagUp == 0 || i.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, _ := i.Addrs()
		for _, a := range addrs {
			if n, ok := a.(*net.IPNet); ok && n.IP.To4() != nil && !n.IP.IsLoopback() && !n.IP.IsLinkLocalUnicast() {
				return n.IP.String()
			}
		}
	}
	return ""
}

// A live Claude callback remains usable locally but never permits a
// remote peer, a forwarded call or another route to bypass the local boundary.
func TestCallbackTokenCannotPermitRemoteAccess(t *testing.T) {
	fresh(t)
	t.Setenv("UNI_SWITCH_ADDR", "0.0.0.0:3425")
	t.Setenv("UNI_SWITCH_TRUST_PROXY", "")
	if err := settings.Save(settings.Settings{}); err != nil {
		t.Fatal(err)
	}
	newCaller(t, "Host")
	h := localGuard(New().Handler())
	token := randomToken()
	addCallback(token)
	t.Cleanup(func() { dropCallback(token) })
	// The same live token still reaches the local callback handler.
	r := httptest.NewRequest("POST", claudeMCPPath+token, strings.NewReader(`{"tool_call_id":"x","name":"read","arguments":{}}`))
	r.RemoteAddr = "127.0.0.1:5000"
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusNotFound || !strings.Contains(w.Body.String(), "unknown or expired Claude run") {
		t.Fatalf("local callback did not reach its handler: %d %s", w.Code, w.Body)
	}
	call := func(method, path string, hdr ...string) (int, string) {
		r := httptest.NewRequest(method, path, strings.NewReader(`{"tool_call_id":"x","name":"read","arguments":{}}`))
		r.RemoteAddr = "192.168.1.9:5000"
		for i := 0; i+1 < len(hdr); i += 2 {
			r.Header.Set(hdr[i], hdr[i+1])
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w.Code, w.Body.String()
	}
	if c, b := call("POST", claudeMCPPath+token); c != http.StatusForbidden {
		t.Fatalf("the live token: %d %s", c, b)
	}
	for _, p := range [][2]string{
		{"POST", claudeMCPPath + randomToken()},
		{"POST", claudeMCPPath + token[:len(token)-1]},
		{"POST", claudeMCPPath + token + "0"},
		{"POST", claudeMCPPath},
		{"POST", claudeMCPPath + token + "/x"},
		{"POST", claudeMCPPath + fmt.Sprintf("%%%02X", token[0]) + token[1:]},
		{"POST", "/x" + claudeMCPPath + token},
		{"GET", claudeMCPPath + token},
		{"POST", "/v1/messages"}, {"POST", "/v1/chat/completions"}, {"GET", "/v1/models"},
		{"GET", "/v1/uni-switch/quotas"}, {"POST", "/mcp/neon"},
	} {
		if c, b := call(p[0], p[1]); c != http.StatusForbidden {
			t.Errorf("%s %s: %d %.120s", p[0], p[1], c, b)
		}
		if c, b := call(p[0], p[1], "Authorization", "Bearer "+token); c != http.StatusForbidden {
			t.Errorf("%s %s with the token as a key: %d %.120s", p[0], p[1], c, b)
		}
	}
	// a run let go takes its token with it
	dropCallback(token)
	if c, b := call("POST", claudeMCPPath+token); c != http.StatusForbidden {
		t.Fatalf("a spent token: %d %s", c, b)
	}
}

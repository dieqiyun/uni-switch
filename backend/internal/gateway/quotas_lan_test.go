package gateway

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// GET /v1/uni-switch/quotas answers another machine with the key of the
// gateway shared on the local network (#389: read over a LAN or tailscale);
// a wrong key, none, or a gateway not shared still get nothing, and the
// refusal says how.
func TestQuotasOverLAN(t *testing.T) {
	fresh(t)
	t.Setenv("UNI_SWITCH_ADDR", "")
	h := localGuard(New().Handler())
	call := func(from string, hdr ...string) (int, string) {
		r := httptest.NewRequest("GET", "/v1/uni-switch/quotas", nil)
		r.RemoteAddr = from
		for i := 0; i+1 < len(hdr); i += 2 {
			r.Header.Set(hdr[i], hdr[i+1])
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w.Code, w.Body.String()
	}
	_, secrets := newCaller(t, "Quota client")
	for _, addr := range []string{"", "0.0.0.0:3425", "192.168.1.9:3425"} {
		t.Setenv("UNI_SWITCH_ADDR", addr)
		for _, from := range []string{"192.168.1.9:5000", "100.64.0.7:5000"} {
			for _, secret := range []string{"", "wrong", secrets[0]} {
				if c, b := call(from, "Authorization", "Bearer "+secret); c != http.StatusForbidden || !strings.Contains(b, "this computer only") {
					t.Fatalf("remote %s: %d %s", from, c, b)
				}
			}
		}
	}
	for _, from := range []string{"127.0.0.1:5000", "[::1]:5000"} {
		if c, b := call(from); c != 200 {
			t.Fatalf("local %s: %d %s", from, c, b)
		}
	}
}

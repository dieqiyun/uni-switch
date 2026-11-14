package gateway

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/access"
	"github.com/dieqiyun/uni-switch/backend/internal/settings"
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
	keys, secrets := newCaller(t, "Quota client")
	key := secrets[0]
	if c, b := call("100.64.0.7:5000", "Authorization", "Bearer "+key); c != 200 || !strings.Contains(b, `"object":"list"`) {
		t.Fatal("shared, with the key:", c, b)
	}
	if c, b := call("192.168.1.9:5000", "x-api-key", key); c != 200 {
		t.Fatal("shared, x-api-key:", c, b)
	}
	if c, _ := call("192.168.1.9:5000", "Authorization", "Bearer wrong"); c != http.StatusUnauthorized {
		t.Fatal("a wrong key got", c)
	}
	if c, _ := call("192.168.1.9:5000"); c != http.StatusUnauthorized {
		t.Fatal("no key got", c)
	}
	if c, _ := call("127.0.0.1:5000"); c != 200 {
		t.Fatal("loopback got", c)
	}

	if _, err := access.Update("off-key", access.Change{Key: keys[0].ID}); err != nil {
		t.Fatal(err)
	}
	if c, _ := call("192.168.1.9:5000", "Authorization", "Bearer "+key); c != http.StatusUnauthorized {
		t.Fatal("disabled key got", c)
	}
	shared := settings.Load()

	if err := settings.Save(shared); err != nil {
		t.Fatal(err)
	}
	if c, _ := call("192.168.1.9:5000", "Authorization", "Bearer "+key); c != http.StatusForbidden {
		t.Fatal("not shared got", c)
	}
	// a gateway UNI_SWITCH_ADDR puts on the network asks another machine for a
	// gateway key, shared or not, and answers one that has it
	t.Setenv("UNI_SWITCH_ADDR", "0.0.0.0:3425")
	if c, b := call("192.168.1.9:5000"); c != http.StatusUnauthorized || !strings.Contains(b, "no API key was sent") {
		t.Fatal("UNI_SWITCH_ADDR, not shared, no key:", c, b)
	}
	if _, err := access.Update("on-key", access.Change{Key: keys[0].ID}); err != nil {
		t.Fatal(err)
	}
	if c, b := call("192.168.1.9:5000", "Authorization", "Bearer "+key); c != 200 || !strings.Contains(b, `"object":"list"`) {
		t.Fatal("UNI_SWITCH_ADDR, not shared, with the key:", c, b)
	}
	if c, _ := call("[::1]:5000"); c != 200 {
		t.Fatal("loopback got", c)
	}
}

package gateway

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/settings"
)

// gaubeebangeel on X: `uni-switch update`, run by an agent uni-switch serves,
// asks the uni-switch serving the gateway to restart into the version it put
// in. Only a program on this computer may (the heap profile's callers),
// never another machine with a key, a page, or another Host; the
// restarter isn't called for them. Without a restarter (uni-switch serve)
// the answer says so.
func TestRestartAskedOnlyFromThisComputer(t *testing.T) {
	fresh(t)
	t.Setenv("UNI_SWITCH_ADDR", "")
	t.Setenv("UNI_SWITCH_PUBLIC_URL", "")
	_, secrets := newCaller(t, "Page")
	s := settings.Load()
	s.CORSOrigins = []string{"https://listed.example"}
	if err := settings.Save(s); err != nil {
		t.Fatal(err)
	}
	var asked []string
	SetRestarter(func(v string) RestartReply {
		asked = append(asked, v)
		return RestartReply{State: "restarting", Handover: true}
	})
	t.Cleanup(func() { SetRestarter(nil) })
	srv := New()
	call := func(guarded bool, remote, host string, hdr ...string) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest("POST", RestartPath, strings.NewReader(`{"version":"v0.1.9999"}`))
		r.RemoteAddr = remote
		r.Host = host
		for i := 0; i+1 < len(hdr); i += 2 {
			r.Header.Set(hdr[i], hdr[i+1])
		}
		w := httptest.NewRecorder()
		h := srv.Handler()
		if guarded {
			h = localGuard(h)
		}
		h.ServeHTTP(w, r)
		return w
	}
	for _, c := range profileCallersRefused(secrets[0]) {
		for _, guarded := range []bool{true, false} {
			if w := call(guarded, c.remote, c.host, c.hdr...); w.Code != http.StatusForbidden && w.Code != http.StatusUnauthorized {
				t.Errorf("%s (lanGuard %v): %d %q", c.name, guarded, w.Code, w.Body.String())
			}
		}
	}
	if len(asked) != 0 {
		t.Fatalf("restarted for a refused caller: %v", asked)
	}
	w := call(true, "127.0.0.1:50123", "127.0.0.1:3425", "User-Agent", "Go-http-client/1.1")
	var got RestartReply
	if err := json.Unmarshal(w.Body.Bytes(), &got); w.Code != http.StatusOK || err != nil {
		t.Fatalf("uni-switch update's ask: %d %q", w.Code, w.Body.String())
	}
	if got.State != "restarting" || !got.Handover || got.Version != Version || len(asked) != 1 || asked[0] != "v0.1.9999" {
		t.Fatalf("answered %+v, restarter asked %v", got, asked)
	}

	SetRestarter(nil)
	w = call(true, "127.0.0.1:50123", "localhost:3425", "User-Agent", "Go-http-client/1.1")
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil || got.State != "unsupported" {
		t.Fatalf("without a restarter: %d %q", w.Code, w.Body.String())
	}
}

package gateway

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/access"
)

func TestLocalGuardRejectsRemoteAndForwardedRequests(t *testing.T) {
	fresh(t)
	secret, err := access.Update("add-key", access.Change{Name: "Local test client"})
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("UNI_SWITCH_ADDR", "0.0.0.0:3425")
	t.Setenv("UNI_SWITCH_TRUST_PROXY", "1")
	t.Setenv("UNI_SWITCH_PUBLIC_URL", "https://gateway.example")
	for _, path := range []string{"/v1/models", "/v1/messages", "/v1/responses", "/v1/chat/completions", "/v1beta/models", "/mcp/uni-switch", "/me", "/v1/uni-switch/usage/summary", "/v1/uni-switch/routing/events"} {
		for _, peer := range []string{"192.168.1.2:4567", "10.0.0.2:4567", "[fd00::2]:4567", "127.0.0.1:4567"} {
			for _, key := range []string{"", secret} {
				r := httptest.NewRequest("GET", "http://localhost"+path, nil)
				r.RemoteAddr = peer
				r.Header.Set("Authorization", "Bearer "+key)
				if peer == "127.0.0.1:4567" {
					r.Header.Set("X-Forwarded-For", "192.168.1.2")
				}
				w := httptest.NewRecorder()
				New().Front().ServeHTTP(w, r)
				if w.Code != http.StatusForbidden {
					t.Errorf("%s peer=%s named=%v: %d", path, peer, key != "", w.Code)
				}
			}
		}
	}
}

func TestLocalGuardPreservesNamedKeyAttribution(t *testing.T) {
	fresh(t)
	secret, err := access.Update("add-key", access.Change{Name: "Desktop client"})
	if err != nil {
		t.Fatal(err)
	}
	keys, _ := access.List()
	called := false
	wantIdentity := true
	h := localGuard(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		if who := access.Caller(r.Context()); wantIdentity && (who.KeyID != keys[0].ID || who.KeyName != "Desktop client") {
			t.Errorf("local caller lost identity: %+v", who)
		}
	}))
	r := httptest.NewRequest("GET", "http://localhost/v1/models", nil)
	r.RemoteAddr = "127.0.0.1:4567"
	r.Header.Set("Authorization", "Bearer "+secret)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 200 || !called {
		t.Fatalf("local key failed: %d %s", w.Code, w.Body)
	}
	if _, err := access.Update("off-key", access.Change{Key: keys[0].ID}); err != nil {
		t.Fatal(err)
	}
	called = false
	wantIdentity = false
	r.Header.Set("Authorization", "Bearer "+secret)
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 200 || !called {
		t.Fatalf("trusted local access changed: %d", w.Code)
	}
}

func TestListenRejectsNonLoopback(t *testing.T) {
	for _, addr := range []string{":0", "0.0.0.0:0", "[::]:0", "192.168.1.2:0"} {
		if ln, err := Listen(addr); err == nil {
			ln.Close()
			t.Errorf("listener bound to %q", addr)
		}
	}
	ln, err := Listen("localhost:0")
	if err != nil {
		t.Fatal(err)
	}
	ln.Close()
}

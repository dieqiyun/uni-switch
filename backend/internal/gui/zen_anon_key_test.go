package gui

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// #1795 (cuddly-guacamole): an OpenCode Zen provider with a key of the
// user's own, given Add another key with nothing pasted, adds public, the
// key Zen's free models answer with no key of the user's, as a new Zen
// provider saved with no key gets it. The provider's row offers it
// (anonKey) until it has it; asked again it says it has it. Any other
// provider still wants a key pasted, and never offers one.
func TestZenAddAnotherKeyEmptyAddsPublic(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("USERPROFILE", os.Getenv("HOME"))
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	mux := http.NewServeMux()
	providerRoutes(mux, nil)
	call := func(method, path, body string) (int, string) {
		t.Helper()
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest(method, path, strings.NewReader(body)))
		return w.Code, w.Body.String()
	}
	anonOf := func(id string) string {
		t.Helper()
		code, body := call("GET", "/api/providers", "")
		if code != 200 {
			t.Fatalf("providers: %d %s", code, body)
		}
		var out struct {
			Providers []struct {
				ID      string `json:"id"`
				AnonKey string `json:"anonKey"`
			} `json:"providers"`
		}
		if err := json.Unmarshal([]byte(body), &out); err != nil {
			t.Fatal(err)
		}
		for _, p := range out.Providers {
			if p.ID == id {
				return p.AnonKey
			}
		}
		t.Fatalf("no %s in %s", id, body)
		return ""
	}

	zen, err := provider.FromPreset("opencode-zen")
	if err != nil {
		t.Fatal(err)
	}
	zen.ID, zen.Key = "opencode-zen", "sk-own-zen-key"
	if err := provider.Save(zen); err != nil {
		t.Fatal(err)
	}
	if err := provider.Save(provider.Provider{ID: "relay", Name: "Relay", Chat: "http://127.0.0.1:1/v1", Key: "sk-relay-1"}); err != nil {
		t.Fatal(err)
	}
	if got := anonOf("opencode-zen"); got != provider.OpenCodeAnonymousKey {
		t.Fatalf("Zen with its own key offers %q", got)
	}
	if got := anonOf("relay"); got != "" {
		t.Fatalf("a relay offers %q", got)
	}

	if code, body := call("POST", "/api/keys/add", `{"id":"opencode-zen","name":"","key":"  ","protocol":""}`); code != 200 {
		t.Fatalf("empty key on Zen: %d %s", code, body)
	}
	p, _ := provider.Find("opencode-zen")
	if p.Key != "sk-own-zen-key" || len(p.Keys) != 1 || p.Keys[0].Key != provider.OpenCodeAnonymousKey || p.Keys[0].Off {
		t.Fatalf("Zen keys: first %q, others %+v", p.Key, p.Keys)
	}
	if got := anonOf("opencode-zen"); got != "" {
		t.Fatalf("Zen with public still offers %q", got)
	}
	code, body := call("POST", "/api/keys/add", `{"id":"opencode-zen","name":"","key":"","protocol":""}`)
	if code == 200 || !strings.Contains(body, "already has public") {
		t.Fatalf("public twice: %d %s", code, body)
	}

	code, body = call("POST", "/api/keys/add", `{"id":"relay","name":"","key":"","protocol":""}`)
	if code == 200 || !strings.Contains(body, "paste the key to add") {
		t.Fatalf("empty key on a relay: %d %s", code, body)
	}
	if p, _ := provider.Find("relay"); p.Key != "sk-relay-1" || len(p.Keys) != 0 {
		t.Fatalf("relay keys %q %+v", p.Key, p.Keys)
	}
}

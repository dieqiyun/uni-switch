package gateway

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// revokedBody is what the ChatGPT backend answered a request on a revoked
// sign-in with (ysicing, #1793).
const revokedBody = `{"error":{"message":"Encountered invalidated oauth token for user, failing request","type":null,"code":"token_revoked","param":null},"status":401}`

// A Codex whose ChatGPT sign-in was revoked asks for its model list beside
// it (jjt on Discord, #4308). The backend's 401 went to Codex, whose one
// refresh fails for good for such a sign-in, and Codex kept its bundled
// list: none of uni-switch's models until the user signed in again. Now the
// list is Codex's last of its own and uni-switch's. A 401 that says the token
// expired is still Codex's to refresh, and a sign-in made again has the
// backend's list on the next ask: nothing of the revoked one is kept.
func TestCodexModelListRevokedSignIn(t *testing.T) {
	setup(t, provider.Chat, &fake{t: t})
	forgetCodexLists(t)
	var say atomic.Value
	say.Store("revoked")
	chatgpt(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch say.Load() {
		case "revoked":
			w.WriteHeader(401)
			io.WriteString(w, revokedBody)
		case "expired":
			w.WriteHeader(401)
			io.WriteString(w, `{"error":{"message":"Provided authentication token is expired. Please try refreshing it.","code":"token_expired"},"status":401}`)
		default:
			io.WriteString(w, `{"models":[{"slug":"gpt-new","priority":1}]}`)
		}
	})
	home, _ := os.UserHomeDir()
	os.MkdirAll(filepath.Join(home, ".codex"), 0o755)
	os.WriteFile(filepath.Join(home, ".codex", "models_cache.json"),
		[]byte(`{"etag":"\"v1\"","models":[{"slug":"gpt-5.5","priority":1,"base_instructions":"x"}]}`), 0o644)
	was := codexListFor
	codexListFor = time.Nanosecond
	t.Cleanup(func() { codexListFor = was })
	h := New().Handler()

	if code, has := codexListAsk(h); code != 200 || !has["fake/m1"] || !has["gpt-5.5"] {
		t.Fatalf("revoked sign-in: %d %v, want 200 with uni-switch's models and Codex's last list", code, has)
	}
	codexLists.asking.Wait()

	say.Store("expired")
	if code, _ := codexListAsk(h); code != 401 {
		t.Fatalf("expired token: %d, want the backend's 401 for Codex to refresh", code)
	}
	codexLists.asking.Wait()

	say.Store("ok")
	if code, has := codexListAsk(h); code != 200 || !has["gpt-new"] || !has["fake/m1"] {
		t.Fatalf("signed in again: %d %v, want the backend's list with uni-switch's", code, has)
	}
}

// The Codex app's file list (CodexDesktopCatalog) for a sign-in the
// backend calls revoked, with no list of the backend's from before: Codex's
// last list of its own and uni-switch's models, told as a change for the file
// to be written. A network error after it says nothing either way, and a
// sign-in made again brings the backend's list back.
func TestCodexDesktopCatalogRevokedSignIn(t *testing.T) {
	codexSignedIn(t)
	codexDeskReset(t)
	if err := provider.Save(provider.Provider{ID: "relay", Name: "Relay", Key: "k", Anthropic: "http://127.0.0.1:1",
		Models: []string{"claude-opus-5.5"}}); err != nil {
		t.Fatal(err)
	}
	home, _ := os.UserHomeDir()
	os.WriteFile(filepath.Join(home, ".codex", "models_cache.json"),
		[]byte(`{"etag":"\"v1\"","models":[{"slug":"gpt-5.5","priority":1,"base_instructions":"x"}]}`), 0o644)
	var say atomic.Value
	say.Store("revoked")
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch say.Load() {
		case "revoked":
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(401)
			io.WriteString(w, revokedBody)
		case "down":
			http.Error(w, "down", http.StatusBadGateway)
		default:
			io.WriteString(w, `{"models":[{"slug":"gpt-new","priority":1,"visibility":"list"}]}`)
		}
	}))
	defer up.Close()
	was := provider.CodexBase
	provider.CodexBase = up.URL + "/backend-api/codex"
	defer func() { provider.CodexBase = was }()
	var changed atomic.Int32
	CodexDesktopChanged = func() { changed.Add(1) }
	slugs := func(b []byte) map[string]bool {
		var got struct {
			Models []map[string]any `json:"models"`
		}
		json.Unmarshal(b, &got)
		out := map[string]bool{}
		for _, m := range got.Models {
			s, _ := m["slug"].(string)
			out[s] = true
		}
		return out
	}
	again := func() {
		codexDesk.Lock()
		codexDesk.got, codexDesk.tried = time.Time{}, time.Time{}
		codexDesk.Unlock()
		CodexDesktopCatalog()
		codexDeskAsked(t)
	}

	if _, ok := CodexDesktopCatalog(); ok {
		t.Fatal("known before the backend answered")
	}
	codexDeskAsked(t)
	if changed.Load() != 1 {
		t.Fatalf("revoked told %d times, want once", changed.Load())
	}
	b, ok := CodexDesktopCatalog()
	if s := slugs(b); !ok || !s["relay/claude-opus-5.5"] || !s["gpt-5.5"] {
		t.Fatalf("revoked sign-in: %v %s", ok, b)
	}

	say.Store("down")
	again()
	if _, ok := CodexDesktopCatalog(); ok {
		t.Fatal("a 502 after the revoked sign-in taken as an answer")
	}

	say.Store("ok")
	again()
	b, ok = CodexDesktopCatalog()
	if s := slugs(b); !ok || !s["gpt-new"] || !s["relay/claude-opus-5.5"] || s["gpt-5.5"] {
		t.Fatalf("signed in again: %v %s", ok, b)
	}
}

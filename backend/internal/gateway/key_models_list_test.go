package gateway

import (
	"fmt"
	"net/http"
	"net/http/httptest"

	"strings"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/access"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
	"github.com/dieqiyun/uni-switch/backend/internal/settings"
)

func listedNow(who access.Identity, es []provider.Entry) []string {
	r := httptest.NewRequest("GET", "/v1/models", nil)
	r = r.WithContext(access.WithIdentity(r.Context(), who))
	var out []string
	for _, e := range keyAllowed(r, es) {
		out = append(out, e.ID)
	}
	return out
}

// BenchmarkHeldKeyListsModels is #1762's catalog: two providers of 500
// models each serving the same ids, so uni-switch finds 500 groups for them,
// listed for a key held to some models, one held to some accounts, and
// one held to none.
func BenchmarkHeldKeyListsModels(b *testing.B) {
	b.Setenv("XDG_CONFIG_HOME", b.TempDir())
	b.Setenv("XDG_CACHE_HOME", b.TempDir())
	var ms []string
	for i := range 500 {
		ms = append(ms, fmt.Sprintf("model-%03d", i))
	}
	for _, id := range []string{"plan", "spare"} {
		if err := provider.Save(provider.Provider{ID: id, Name: id, Key: "k-" + id, Chat: "https://" + id + ".invalid/v1", Models: ms}); err != nil {
			b.Fatal(err)
		}
	}
	s := settings.Load()

	if err := settings.Save(s); err != nil {
		b.Fatal(err)
	}
	var secrets []string
	for _, name := range []string{"Models", "Accounts", "Free"} {
		secret, err := access.Update("add-key", access.Change{Name: name})
		if err != nil {
			b.Fatal(err)
		}
		secrets = append(secrets, secret)
	}
	keys, _ := access.List()
	keys = keys[len(keys)-3:]
	if _, err := access.Update("models-key", access.Change{Key: keys[0].ID, Models: []string{
		"plan/model-00*", "spare/MODEL-1*", "plan/model-250", "group/auto-model-3*", "group/auto-model-499",
	}}); err != nil {
		b.Fatal(err)
	}
	if _, err := access.Update("accounts-key", access.Change{Key: keys[1].ID, Accounts: []string{"plan/" + provider.KeyID("k-plan")}}); err != nil {
		b.Fatal(err)
	}
	h := New().Handler()
	for i, name := range []string{"models", "accounts", "free"} {
		b.Run(name, func(b *testing.B) {
			for b.Loop() {
				r := httptest.NewRequest("GET", "/v1/models", nil)
				r.Header.Set("Authorization", "Bearer "+secrets[i])
				w := httptest.NewRecorder()
				h.ServeHTTP(w, r)
				if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"group/auto-model-499"`) {
					b.Fatal(w.Code, w.Body.String())
				}
			}
		})
	}
}

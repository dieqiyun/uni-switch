package gateway

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// Best in family on the codex provider keeps Codex's own list to the
// newest of each family the ChatGPT account lists (Steven on Discord:
// GPT-6.1-Sol, GPT-6-Astra, GPT-6-Luna, GPT-5.6-Terra), as it does the
// general catalog: the hidden jobs and other providers' models stay, the
// ETag changes with it, and spawn_agent names only those.
func TestCodexBestInFamilyCatalog(t *testing.T) {
	codexSignedIn(t)
	const own = `[{"slug":"gpt-6.1-sol","visibility":"list","priority":1},{"slug":"gpt-6-astra","visibility":"list","priority":2},{"slug":"gpt-6-sol","visibility":"list","priority":3},{"slug":"gpt-6-luna","visibility":"list","priority":4},{"slug":"gpt-5.6-sol","visibility":"list","priority":5},{"slug":"gpt-5.6-terra","visibility":"list","priority":6},{"slug":"gpt-5.6-luna","visibility":"list","priority":7},{"slug":"codex-auto-review","visibility":"hide","priority":8}]`
	os.MkdirAll(filepath.Join(os.Getenv("HOME"), ".codex"), 0o700)
	os.WriteFile(filepath.Join(os.Getenv("HOME"), ".codex", "models_cache.json"), []byte(`{"etag":"\"v1\"","models":`+own+`}`), 0o600)
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"models":`+own+`}`)
	}))
	t.Cleanup(up.Close)
	was := provider.CodexBase
	provider.CodexBase = up.URL + "/backend-api/codex"
	t.Cleanup(func() { provider.CodexBase = was })
	if err := provider.Save(provider.Provider{ID: "fake", Name: "Fake", Key: "k", Models: []string{"m1"}, Chat: up.URL + "/v1"}); err != nil {
		t.Fatal(err)
	}
	gw := httptest.NewServer(New().Handler())
	t.Cleanup(gw.Close)
	all := []string{"gpt-6.1-sol", "gpt-6-astra", "gpt-6-sol", "gpt-6-luna", "gpt-5.6-sol", "gpt-5.6-terra", "gpt-5.6-luna"}
	tags := map[string]string{}
	for _, tc := range []struct {
		name string
		p    provider.Provider
		want []string
	}{
		{"vendor list", provider.Provider{ID: "codex"}, all},
		{"best in family", provider.Provider{ID: "codex", BestInFamily: true}, []string{"gpt-6.1-sol", "gpt-6-astra", "gpt-6-luna", "gpt-5.6-terra"}},
		{"picked", provider.Provider{ID: "codex", Models: []string{"gpt-6-sol"}, BestInFamily: true}, []string{"gpt-6-sol"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := provider.Save(tc.p); err != nil {
				t.Fatal(err)
			}
			req, err := http.NewRequest(http.MethodGet, gw.URL+CodexPath+"/models?client_version=0.163.0", nil)
			if err != nil {
				t.Fatal(err)
			}
			req.Header.Set("Authorization", "Bearer chatgpt-token")
			res, err := gw.Client().Do(req)
			if err != nil {
				t.Fatal(err)
			}
			defer res.Body.Close()
			if res.StatusCode != http.StatusOK {
				t.Fatalf("model list status %d", res.StatusCode)
			}
			var list struct {
				Models []struct {
					Slug string `json:"slug"`
				} `json:"models"`
			}
			if err := json.NewDecoder(res.Body).Decode(&list); err != nil {
				t.Fatal(err)
			}
			var got []string
			for _, m := range list.Models {
				got = append(got, m.Slug)
			}
			want := append(slices.Clone(tc.want), "codex-auto-review", "fake/m1")
			slices.Sort(got)
			slices.Sort(want)
			if !slices.Equal(got, want) {
				t.Fatalf("models = %v, want %v", got, want)
			}
			tags[tc.name] = res.Header.Get("ETag")
			var spawn []string
			for _, c := range spawnChoices(httptest.NewRequest("POST", CodexPath+"/responses", nil), "gpt-6.1-sol", true) {
				if slices.Contains(all, c.slug) {
					spawn = append(spawn, c.slug)
				}
			}
			if !slices.Equal(spawn, tc.want) {
				t.Errorf("spawn_agent offers the account's %v, want %v", spawn, tc.want)
			}
		})
	}
	if tags["best in family"] == tags["vendor list"] {
		t.Errorf("ETag kept across Best in family: %q", tags)
	}
}

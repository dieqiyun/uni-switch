package gateway

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/access"
	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// codexModelInfo is what codex-rs's ModelInfo can't do without
// (protocol/src/openai_models.rs): a field missing here fails the whole
// list there, "failed to decode models response".
type codexModelInfo struct {
	Slug             *string           `json:"slug"`
	DisplayName      *string           `json:"display_name"`
	BaseInstructions *string           `json:"base_instructions"`
	Levels           *[]map[string]any `json:"supported_reasoning_levels"`
	ShellType        *string           `json:"shell_type"`
	Visibility       *string           `json:"visibility"`
	SupportedInAPI   *bool             `json:"supported_in_api"`
	Priority         *int              `json:"priority"`
	Verbosity        *bool             `json:"support_verbosity"`
	Truncation       *struct {
		Mode  string `json:"mode"`
		Limit int    `json:"limit"`
	} `json:"truncation_policy"`
	Tools *[]string `json:"experimental_supported_tools"`
}

// Local Codex clients receive the model catalog in Codex's native shape.
// Named keys filter their models; the default local token remains usable.
func TestCodexCatalogForAGatewayKey(t *testing.T) {
	fresh(t)
	t.Setenv("UNI_SWITCH_ADDR", "")
	plan := &fake{t: t, ctype: "application/json", reply: `{}`}
	spare := &fake{t: t, ctype: "application/json", reply: `{}`}
	twoProviders(t, plan, spare)
	keys, secrets := newCaller(t, "Held", "Free")
	if _, err := access.Update("models-key", access.Change{Key: keys[0].ID, Models: []string{"plan/*"}}); err != nil {
		t.Fatal(err)
	}
	h := localGuard(New().Handler())
	host := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.RemoteAddr = "127.0.0.1:5000"
		h.ServeHTTP(w, r)
	}))
	t.Cleanup(host.Close)
	get := func(path, key string) (*http.Response, []byte) {
		req, _ := http.NewRequest("GET", host.URL+path, nil)
		if key != "" {
			req.Header.Set("Authorization", "Bearer "+key)
		}
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		b, _ := io.ReadAll(res.Body)
		return res, b
	}
	// what Codex asks: model_catalog_url with ?client_version= after it
	slugs := func(key string) []string {
		t.Helper()
		res, b := get(CodexCatalogPath+"?client_version=0.161.0", key)
		if res.StatusCode != 200 {
			t.Fatalf("%d %s", res.StatusCode, b)
		}
		var list struct {
			Models []codexModelInfo `json:"models"`
		}
		if err := json.Unmarshal(b, &list); err != nil {
			t.Fatal(err, string(b))
		}
		var out []string
		for _, m := range list.Models {
			if m.Slug == nil || m.DisplayName == nil || m.BaseInstructions == nil || *m.BaseInstructions == "" ||
				m.Levels == nil || m.ShellType == nil || m.Visibility == nil || m.SupportedInAPI == nil ||
				m.Priority == nil || m.Verbosity == nil || m.Truncation == nil || m.Tools == nil {
				t.Fatalf("an entry Codex can't read: %s", b)
			}
			if slices.Contains(out, *m.Slug) {
				t.Fatalf("slug %s twice: Codex refuses the list", *m.Slug)
			}
			out = append(out, *m.Slug)
		}
		return out
	}
	if got := slugs(secrets[1]); !slices.Contains(got, "plan/m1") || !slices.Contains(got, "spare/m2") {
		t.Fatalf("free key: %v", got)
	}
	// a key held to some models is listed only those, as on /v1/models
	if got := slugs(secrets[0]); !slices.Contains(got, "plan/m1") || slices.Contains(got, "spare/m2") {
		t.Fatalf("held key: %v", got)
	}
	if res, b := get(CodexCatalogPath, ""); res.StatusCode != 200 {
		t.Fatalf("no key: %d %s", res.StatusCode, b)
	}
	if res, b := get(CodexCatalogPath, "sk-not-a-key"); res.StatusCode != 200 {
		t.Fatalf("a wrong key: %d %s", res.StatusCode, b)
	}
	// /v1/models is OpenAI's list as it was
	res, b := get("/v1/models", secrets[1])
	var l struct {
		Object string `json:"object"`
		Data   []struct {
			ID string `json:"id"`
		} `json:"data"`
		Models any `json:"models"`
	}
	if json.Unmarshal(b, &l); res.StatusCode != 200 || l.Object != "list" || len(l.Data) == 0 || l.Models != nil {
		t.Fatalf("/v1/models: %d %s", res.StatusCode, b)
	}
}

// Codex before 0.166 reads at most 1 MiB of a model_catalog_url and
// refuses the whole list past it; every entry carries the instructions
// official Codex sends its model (~21 KB), so a gateway with hundreds of
// models hands such a Codex as many as fit, and says how many it left out,
// rather than a list Codex loads none of. Codex 0.166, which says so in
// its ?client_version=, reads the list whole and gets every model.
func TestCodexCatalogFitsCodexsLimit(t *testing.T) {
	fresh(t)
	var ms []string
	for i := range 300 {
		ms = append(ms, fmt.Sprintf("model-%03d", i))
	}
	if err := provider.Save(provider.Provider{ID: "many", Name: "Many", Key: "k", Chat: "http://127.0.0.1:9/v1", Models: ms}); err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	New().Handler().ServeHTTP(w, httptest.NewRequest("GET", CodexCatalogPath, nil))
	if w.Code != 200 || w.Body.Len() > codexCatalogMost {
		t.Fatalf("%d, %d bytes", w.Code, w.Body.Len())
	}
	var list struct {
		Models []codexModelInfo `json:"models"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &list); err != nil {
		t.Fatal(err)
	}
	out, _ := strconv.Atoi(w.Header().Get("X-Uni-Switch-Left-Out"))
	t.Logf("%d listed in %d bytes, %d left out", len(list.Models), w.Body.Len(), out)
	if len(list.Models) < 40 || out == 0 || len(list.Models)+out != 300 {
		t.Fatalf("%d listed, %d left out", len(list.Models), out)
	}
	if first := *list.Models[0].Slug; !strings.HasPrefix(first, "many/model-000") {
		t.Fatalf("the first kept is %s", first)
	}
	for _, v := range []string{"0.165.0", "0.99.1", "", "x"} {
		w := httptest.NewRecorder()
		New().Handler().ServeHTTP(w, httptest.NewRequest("GET", CodexCatalogPath+"?client_version="+v, nil))
		if w.Body.Len() > codexCatalogMost || w.Header().Get("X-Uni-Switch-Left-Out") == "" {
			t.Errorf("client_version %q: %d bytes, %q left out", v, w.Body.Len(), w.Header().Get("X-Uni-Switch-Left-Out"))
		}
	}
	for _, v := range []string{"0.166.0", "0.167.0-alpha.1", "1.0.0"} {
		w := httptest.NewRecorder()
		New().Handler().ServeHTTP(w, httptest.NewRequest("GET", CodexCatalogPath+"?client_version="+v, nil))
		if err := json.Unmarshal(w.Body.Bytes(), &list); err != nil {
			t.Fatal(err)
		}
		if len(list.Models) != 300 || w.Header().Get("X-Uni-Switch-Left-Out") != "" {
			t.Errorf("client_version %s: %d listed, %q left out", v, len(list.Models), w.Header().Get("X-Uni-Switch-Left-Out"))
		}
	}
}

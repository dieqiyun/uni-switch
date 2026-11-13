package gateway

import (
	"encoding/json"

	"net/http"
	"net/http/httptest"

	"slices"
	"strings"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// An embeddings model the user disabled is refused before its provider is
// asked.
func TestDisabledEmbeddingsModelRefused(t *testing.T) {
	s, l := shelved(t)
	if err := provider.DisableModels("lib/embed-1"); err != nil {
		t.Fatal(err)
	}
	for _, model := range []string{"lib/embed-1", "embed-1"} {
		code, raw := postRetrieval(t, s, "/v1/embeddings", `{"model":"`+model+`","input":"hi"}`)
		if code != 403 || !strings.Contains(raw, "is disabled in uni-switch") {
			t.Fatalf("%s: %d %s", model, code, raw)
		}
	}
	if sent := l.got("/v1/embeddings"); len(sent) != 0 {
		t.Fatal("a disabled embeddings model was asked", sent)
	}
}

// Codex's own models, disabled: gone from the list Codex reads, refused at
// the ChatGPT backend's route before it is asked, and a subagent put on
// one stays on its lead's model.
func TestDisabledCodexModels(t *testing.T) {
	up := codexSubagentSetup(t)
	if err := provider.SetCodexSubagentModel("codex/gpt-6-astra"); err != nil {
		t.Fatal(err)
	}
	if err := provider.DisableModels("gpt-6-astra"); err != nil {
		t.Fatal(err)
	}
	s := New()
	rec := codexAsks(s, "gpt-5.5", "collab_spawn", sealedTaskFernet)
	if rec.Code != 200 {
		t.Fatalf("subagent: %d %s", rec.Code, rec.Body.String())
	}
	if got := up.asked(); len(got) != 1 || got[0] != "gpt-5.5" {
		t.Fatalf("ChatGPT was asked for %v", got)
	}
	if r := lastRoute(s); r.Subagent == nil || r.Subagent.Kept != "it is disabled in uni-switch" {
		t.Fatalf("trace %+v", r.Subagent)
	}
	rec = codexAsks(s, "gpt-6-astra", "", leadAsk)
	if rec.Code != 403 || !strings.Contains(rec.Body.String(), "is disabled in uni-switch") {
		t.Fatalf("lead on a disabled model: %d %s", rec.Code, rec.Body.String())
	}
	if got := up.asked(); len(got) != 1 {
		t.Fatalf("ChatGPT was asked for %v", got)
	}
}

// Codex's model list leaves a disabled one of its own out.
func TestDisabledCodexModelsListed(t *testing.T) {
	setup(t, provider.Chat, &fake{t: t})
	chatgpt(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("ETag", `W/"up"`)
		w.Write([]byte(`{"models":[{"slug":"gpt-6","priority":1},{"slug":"gpt-5.5","priority":2}]}`))
	})
	list := func() []string {
		t.Helper()
		rec := httptest.NewRecorder()
		req := httptest.NewRequest("GET", CodexPath+"/models?client_version=0.159.2", nil)
		req.Header.Set("Authorization", "Bearer chatgpt-token")
		New().Handler().ServeHTTP(rec, req)
		var l struct {
			Models []map[string]any `json:"models"`
		}
		if rec.Code != 200 || json.Unmarshal(rec.Body.Bytes(), &l) != nil {
			t.Fatalf("%d %s", rec.Code, rec.Body.String())
		}
		var slugs []string
		for _, m := range l.Models {
			slugs = append(slugs, m["slug"].(string))
		}
		return slugs
	}
	if got := list(); !slices.Contains(got, "gpt-6") || !slices.Contains(got, "fake/m1") {
		t.Fatalf("before: %v", got)
	}
	if err := provider.DisableModels("gpt-6", "fake/*"); err != nil {
		t.Fatal(err)
	}
	if got := list(); slices.Contains(got, "gpt-6") || slices.Contains(got, "fake/m1") || !slices.Contains(got, "gpt-5.5") {
		t.Fatalf("after: %v", got)
	}
}

package provider

import (
	"context"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/catalog"
)

func TestPlanModels(t *testing.T) {
	p, err := FromPreset("clinepass")
	if err != nil {
		t.Fatal(err)
	}
	n := len(Preset("clinepass").Models)
	// the Cline API's list: its paid models, one of the plan's among them;
	// its ":free" ones aren't what Cline offers free, and are left out
	got := p.planModels([]catalog.Model{{ID: "anthropic/claude-opus-5-5"}, {ID: "cline-pass/glm-5.3"}, {ID: "qwen/qwen3.8-27b:free"}})
	if len(got) != 1+len(clineFree) || got[0].ID != "cline-pass/glm-5.3" || got[1].ID != "cline-free/deepseek-v4.1-flash" {
		t.Fatalf("kept: %+v", got)
	}
	// none of the plan's listed: the plan's own, then Cline's free models
	// as its desktop app lists them (lml on Discord)
	got = p.planModels(nil)
	if len(got) != n+len(clineFree) || got[0].ID != "cline-pass/glm-5.3" {
		t.Fatalf("plan's: %+v", got)
	}
	var free []string
	for _, m := range got {
		if m.Free {
			free = append(free, m.ID)
		}
	}
	if want := []string{"cline-free/deepseek-v4.1-flash", "stealth/space-bunny-alpha", "cline-free/mimo-v2.6-flash", "cline-free/muse-spark-1.3-contributor"}; !slices.Equal(free, want) {
		t.Fatalf("free: %v", free)
	}
	// a free model costs nothing, not its paid model's price
	if pr, ok := p.ListPrice("cline-free/deepseek-v4.1-flash"); !ok || !pr.Same(catalog.Price{}) {
		t.Fatalf("free price: %+v %v", pr, ok)
	}
	// another provider's list is left as it is
	o, _ := FromPreset("opencode-go")
	if got := o.planModels([]catalog.Model{{ID: "a"}, {ID: "b"}}); len(got) != 2 {
		t.Fatalf("other: %+v", got)
	}
}

// Cline's clients take their plan and free models from the
// recommended-models feed, asked as the desktop app (whose client type is
// given the desktop's free list) and with no key; ClinePass lists both,
// the free ones marked Free and priced at nothing, and no paid model.
func TestClineFeed(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	var asked *http.Request
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/ai/cline/recommended-models" {
			http.NotFound(w, r)
			return
		}
		asked = r
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"recommended":[{"id":"anthropic/claude-opus-5.5","name":"claude-opus-5.5"}],
			"free":[{"id":"cline-free/deepseek-v4.1-flash","name":"Deepseek-v4.1-Flash"},{"id":"stealth/space-bunny-alpha","name":"space-bunny-alpha"},
				{"id":"stealth/new-free-one","name":"stealth/new-free-one"}],
			"clinePass":[{"id":"cline-pass/glm-5.3","name":"cline-pass/glm-5.3"},{"id":"cline-pass/mimo-v2.6-pro","name":"cline-pass/mimo-v2.6-pro"}]}`))
	}))
	defer srv.Close()
	p, err := FromPreset("clinepass")
	if err != nil {
		t.Fatal(err)
	}
	p.ID, p.Key, p.Chat = "cline-test", "clp_secret", srv.URL+"/api/v1"
	ms, err := p.Fetch(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if asked == nil {
		t.Fatal("the feed wasn't asked")
	}
	if asked.Header.Get("X-CLIENT-TYPE") != "cline-desktop" || asked.Header.Get("User-Agent") != "Cline/"+ClineVersion || asked.Header.Get("Authorization") != "" {
		t.Fatalf("feed asked with %v", asked.Header)
	}
	var ids, free []string
	for _, m := range ms {
		ids = append(ids, m.ID)
		if m.Free {
			free = append(free, m.ID)
		}
	}
	if want := []string{"cline-pass/glm-5.3", "cline-pass/mimo-v2.6-pro", "cline-free/deepseek-v4.1-flash", "stealth/space-bunny-alpha", "stealth/new-free-one"}; !slices.Equal(ids, want) {
		t.Fatalf("listed %v", ids)
	}
	if want := []string{"cline-free/deepseek-v4.1-flash", "stealth/space-bunny-alpha", "stealth/new-free-one"}; !slices.Equal(free, want) {
		t.Fatalf("free %v", free)
	}
	// a free model the feed named and no id gives away costs nothing too,
	// a plan's model is not called free
	for _, id := range []string{"stealth/new-free-one", "stealth/space-bunny-alpha"} {
		if pr, ok := p.ListPrice(id); !ok || !pr.Same(catalog.Price{}) {
			t.Fatalf("%s: %+v %v", id, pr, ok)
		}
	}
	if p.clineFreeModel("cline-pass/glm-5.3") {
		t.Fatal("a plan's model is free")
	}
}

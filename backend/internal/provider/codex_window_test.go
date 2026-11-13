package provider

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/catalog"
)

// A subscription and an API provider advertise the same product default,
// even when the native cache and models.dev give different windows.
func TestCodexModelsTakeCodexWindow(t *testing.T) {
	home := signIn(t)
	cache := catalog.CachePath()
	os.MkdirAll(filepath.Dir(cache), 0o755)
	if err := os.WriteFile(cache, []byte(`{"openai":{"id":"openai","models":{
	  "gpt-5.5":{"id":"gpt-5.5","limit":{"context":1050000,"input":922000,"output":128000}}}}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	catalog.Reset()
	t.Cleanup(catalog.Reset)
	if err := os.WriteFile(filepath.Join(home, ".codex", "models_cache.json"), []byte(`{"etag":"\"e1\"","client_version":"0.159.0","models":[
	  {"slug":"gpt-5.5","display_name":"GPT-5.5","priority":1,"visibility":"list","context_window":272000,"max_context_window":272000}]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	ForgetAccounts()
	t.Cleanup(ForgetAccounts)

	p, ok := codexAccount(home)
	if !ok {
		t.Fatal("no Codex sign-in")
	}
	var got int
	for _, m := range p.Available() {
		if m.ID == "gpt-5.5" {
			got = p.WindowOf(m)
		}
	}
	if got != DefaultContextWindow {
		t.Errorf("Codex subscription's gpt-5.5 window %d, want default 256K", got)
	}
	if n := CodexWindow("", "gpt-5.5"); n != DefaultContextWindow {
		t.Errorf("CodexWindow(gpt-5.5) = %d, want default 256K", n)
	}
	if n := CodexWindow("", "gpt-unlisted"); n != DefaultContextWindow {
		t.Errorf("CodexWindow(gpt-unlisted) = %d, want default 256K", n)
	}
	for _, e := range Catalog() {
		if e.Provider.ID == "codex" && e.Model == "gpt-5.5" && e.Context != DefaultContextWindow {
			t.Errorf("catalog entry %s told agents %d, want default 256K", e.ID, e.Context)
		}
	}
	for _, restore := range []bool{false, true} {
		for _, id := range []string{"gpt-5.5", "gpt-unlisted"} {
			n, _, ok := CodexNativeWindow(restore)(id)
			if !ok || n != DefaultContextWindow {
				t.Errorf("native %s (restore=%v): %d, %v; want default 256K", id, restore, n, ok)
			}
		}
	}

	// The same id on an API provider uses the same default.
	oai := Provider{ID: "oai", Name: "OAI", Key: "k", Catalog: "openai", Responses: "https://api.example.invalid/v1"}
	got = 0
	for _, m := range oai.Available() {
		if m.ID == "gpt-5.5" {
			got = oai.WindowOf(m)
		}
	}
	if got != DefaultContextWindow {
		t.Errorf("OpenAI API gpt-5.5 window %d, want default 256K", got)
	}
	if err := Save(Provider{ID: "codex", Contexts: map[string]int{"*": 512000, "gpt-5.5": 128000}}); err != nil {
		t.Fatal(err)
	}
	for id, want := range map[string]int{"gpt-5.5": 128000, "gpt-unlisted": 512000} {
		if n := CodexWindow("", id); n != want {
			t.Errorf("manual %s: %d, want %d", id, n, want)
		}
		for _, restore := range []bool{false, true} {
			n, _, ok := CodexNativeWindow(restore)(id)
			if !ok || n != want {
				t.Errorf("native manual %s: %d, %v; want %d", id, n, ok, want)
			}
		}
	}
}

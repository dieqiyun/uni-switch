package gui

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/agent"
	"github.com/dieqiyun/uni-switch/backend/internal/provider"
	"github.com/dieqiyun/uni-switch/backend/internal/settings"
)

// zgjfjfl on Discord (10-31): Codex on a relay's gpt-6-A compacted near
// 128K with Long conversations at 544K. Codex compacts at 90% of the
// window uni-switch tells it, and no threshold above that window can move it,
// so Settings names the model Codex's config runs and its window when
// Long conversations doesn't come below it. Under Full window, below the
// window, or with a threshold on the model, it says nothing, and a model
// Codex runs on its own provider is not uni-switch's to name.
func TestCodexWindowUnderLongConversations(t *testing.T) {
	h := sandboxHome(t)
	if _, err := provider.Add(provider.Provider{ID: "relay", Name: "Relay", Chat: "http://127.0.0.1:1/v1", Key: "sk-relay",
		Models: []string{"gpt-6-A"}, Contexts: map[string]int{"gpt-6-A": 128000}}); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(h, ".codex")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := func(body string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, "config.toml"), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	save := func(s settings.Settings) {
		t.Helper()
		if err := settings.Save(s); err != nil {
			t.Fatal(err)
		}
	}
	got := func() *codexRunWindow { return settingsState().CodexWindow }

	cfg("model = \"relay/gpt-6-A\"\nmodel_provider = \"uni-switch\"\n")
	save(settings.Settings{CompactAt: 544000})
	if w := got(); w == nil || *w != (codexRunWindow{Model: "relay/gpt-6-A", Window: 128000, Size: "128K"}) {
		t.Fatalf("544K over a 128K model: codexWindow = %+v; want relay/gpt-6-A at 128000", w)
	}

	// the profile the config picks is the model Codex runs
	cfg("model = \"gpt-5.5\"\nmodel_provider = \"openai\"\nprofile = \"r\"\n\n[profiles.r]\nmodel = \"relay/gpt-6-A\"\nmodel_provider = \"uni-switch\"\n")
	if w := got(); w == nil || w.Model != "relay/gpt-6-A" {
		t.Fatalf("profile's model: codexWindow = %+v", w)
	}

	cfg("model = \"gpt-5.5\"\nmodel_provider = \"openai\"\n")
	if w := got(); w != nil {
		t.Fatalf("Codex's own provider: codexWindow = %+v; want none", w)
	}

	cfg("model = \"relay/gpt-6-A\"\nmodel_provider = \"uni-switch\"\n")
	save(settings.Settings{CompactAt: 100000})
	if w := got(); w != nil {
		t.Fatalf("100K under the window: codexWindow = %+v; want none", w)
	}
	save(settings.Settings{CompactAt: 544000, FullContext: true})
	if w := got(); w != nil {
		t.Fatalf("Full window: codexWindow = %+v; want none", w)
	}
	save(settings.Settings{CompactAt: 544000, ModelCompacts: map[string]int{"relay/gpt-6-A": 100000}})
	if w := got(); w != nil {
		t.Fatalf("the model's own threshold: codexWindow = %+v; want none", w)
	}
}

func TestCodexKeysTextAndSize(t *testing.T) {
	got := codexKeysText([]agent.CodexCompactHold{{Key: "model_context_window", Value: "128000"}, {Key: "model_auto_compact_token_limit", Profile: "work", Value: "120000"}})
	if want := "model_context_window = 128000, [profiles.work] model_auto_compact_token_limit = 120000"; got != want {
		t.Fatalf("codexKeysText = %q; want %q", got, want)
	}
	if got := codexKeysText(nil); got != "" {
		t.Fatalf("no keys: %q", got)
	}
	for n, want := range map[int]string{128000: "128K", 1000000: "1M", 2000000: "2M", 1050000: "1050K", 128500: "128500"} {
		if got := tokensSize(n); got != want {
			t.Errorf("tokensSize(%d) = %q; want %q", n, got, want)
		}
	}
}

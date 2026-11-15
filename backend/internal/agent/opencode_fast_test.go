package agent

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/catalog"
	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// #2379 (jiz4oh): OpenCode has no /fast. A GPT model a ChatGPT account
// serves through uni-switch is written into opencode.json with each reasoning
// level again as a fast variant, low-fast to xhigh-fast, asking for
// service_tier "priority" as Codex's /fast does. The same model on an
// OpenAI-style key of the user's, and a Codex model that isn't GPT, get no
// fast variants. A variant the user put on one of uni-switch's models stays
// through a sync, as does one they wrote over under a name of uni-switch's; a
// variant of uni-switch's the model no longer has goes; a second sync writes
// nothing.
func TestOpenCodeCodexFastVariants(t *testing.T) {
	claims := func(m map[string]any) string {
		b, _ := json.Marshal(m)
		return "h." + base64.RawURLEncoding.EncodeToString(b) + ".s"
	}
	auth, _ := json.Marshal(map[string]any{"auth_mode": "chatgpt", "tokens": map[string]any{
		"id_token":      claims(map[string]any{"email": "me@example.com"}),
		"access_token":  claims(map[string]any{"exp": time.Now().Add(time.Hour).Unix()}),
		"refresh_token": "r-1", "account_id": "acct-1"}})
	home, _ := codexHome(t, string(auth), "model = \"gpt-5.5\"\n")
	levels := `"supported_reasoning_levels":[{"effort":"low"},{"effort":"medium"},{"effort":"high"},{"effort":"xhigh"}]`
	os.WriteFile(filepath.Join(home, ".codex", "models_cache.json"), []byte(`{"models":[
	  {"slug":"gpt-5.5","display_name":"GPT-5.5","priority":1,`+levels+`},
	  {"slug":"codex-auto-review","display_name":"Auto review","priority":2,`+levels+`}]}`), 0o644)
	if err := provider.Save(provider.Provider{
		ID: "relay", Name: "Relay", Chat: "https://relay.example.test/v1", Key: "key", Models: []string{"gpt-5.5"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := catalog.SaveLive("relay", "https://relay.example.test/v1", []catalog.Model{
		{ID: "gpt-5.5", Efforts: []string{"low", "medium", "high", "xhigh"}},
	}); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(home, ".config", "opencode", "opencode.json")
	// the user's own: a variant of their own on uni-switch's GPT-5.5, and
	// high-fast written over; max is a variant an older uni-switch wrote that
	// the model hasn't
	writeFile(t, path, `{"theme":"dark","provider":{"uni-switch":{"models":{"codex/gpt-5.5":{"variants":{
	  "max":{"reasoningEffort":"max"},
	  "turbo":{"reasoningEffort":"low","service_tier":"priority","textVerbosity":"low"},
	  "high-fast":{"reasoningEffort":"high","service_tier":"flex"}}}}}}}`)
	oc := opencode(home, filepath.Join(home, ".config"))
	if err := oc.Sync(); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var cfg struct {
		Theme    string `json:"theme"`
		Provider map[string]struct {
			Models map[string]struct {
				Variants map[string]map[string]any `json:"variants"`
			} `json:"models"`
		} `json:"provider"`
	}
	if err := json.Unmarshal(b, &cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.Theme != "dark" {
		t.Fatalf("theme lost: %s", b)
	}
	ms := cfg.Provider["uni-switch"].Models
	gpt, ok := ms["codex/gpt-5.5"]
	if !ok {
		t.Fatalf("no codex/gpt-5.5: %s", b)
	}
	fast := func(level string) map[string]any {
		return map[string]any{"reasoningEffort": level, "service_tier": "priority",
			"extraBody": map[string]any{"service_tier": "priority"}}
	}
	want := map[string]map[string]any{
		"low": {"reasoningEffort": "low"}, "medium": {"reasoningEffort": "medium"},
		"high": {"reasoningEffort": "high"}, "xhigh": {"reasoningEffort": "xhigh"},
		"low-fast": fast("low"), "medium-fast": fast("medium"), "xhigh-fast": fast("xhigh"),
		// the user's, kept as they wrote them
		"high-fast": {"reasoningEffort": "high", "service_tier": "flex"},
		"turbo":     {"reasoningEffort": "low", "service_tier": "priority", "textVerbosity": "low"},
	}
	if !reflect.DeepEqual(gpt.Variants, want) {
		t.Errorf("codex/gpt-5.5 variants:\n got %v\nwant %v", gpt.Variants, want)
	}
	// in order: uni-switch's weakest first, fast after, the user's own last
	order := []string{`"low":`, `"medium":`, `"high":`, `"xhigh":`, `"low-fast":`, `"medium-fast":`, `"high-fast":`, `"xhigh-fast":`, `"turbo":`}
	i := bytes.Index(b, []byte(`"codex/gpt-5.5"`))
	at := -1
	for _, k := range order {
		j := bytes.Index(b[i:], []byte(k))
		if j <= at {
			t.Fatalf("%s out of order in\n%s", k, b)
		}
		at = j
	}
	for _, id := range []string{"relay/gpt-5.5", "codex/codex-auto-review"} {
		m, ok := ms[id]
		if !ok {
			t.Errorf("%s missing: %s", id, b)
			continue
		}
		if len(m.Variants) != 4 {
			t.Errorf("%s: variants %v, want the four levels alone", id, m.Variants)
		}
		for k, v := range m.Variants {
			if _, ok := v["service_tier"]; ok {
				t.Errorf("%s %s asks for a tier: %v", id, k, v)
			}
		}
	}
	if err := oc.Sync(); err != nil {
		t.Fatal(err)
	}
	if b2, _ := os.ReadFile(path); !bytes.Equal(b, b2) {
		t.Fatalf("a second sync rewrote it:\n%s\n%s", b, b2)
	}
}

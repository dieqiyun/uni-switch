package gui

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"

	"testing"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/catalog"
	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// codexSignedInSandbox signs a ChatGPT account in to a sandbox Codex, with
// the models its backend lists, and returns their ids.
func codexSignedInSandbox(t *testing.T) []string {
	t.Helper()
	home := sandboxHome(t)
	codexHome := filepath.Join(home, ".codex")
	t.Setenv("CODEX_HOME", codexHome)
	os.MkdirAll(codexHome, 0o700)
	jwt := func(c any) string {
		b, _ := json.Marshal(c)
		return "e30." + base64.RawURLEncoding.EncodeToString(b) + ".sig"
	}
	auth, _ := json.Marshal(map[string]any{"auth_mode": "chatgpt", "tokens": map[string]any{
		"id_token":     jwt(map[string]any{"email": "a@x.y", "https://api.openai.com/auth": map[string]any{"chatgpt_plan_type": "plus", "chatgpt_account_id": "acc"}}),
		"access_token": jwt(map[string]any{"exp": time.Now().Add(24 * time.Hour).Unix()}), "refresh_token": "r", "account_id": "acc"}})
	os.WriteFile(filepath.Join(codexHome, "auth.json"), auth, 0o600)
	os.WriteFile(filepath.Join(codexHome, "config.toml"), []byte("model = \"gpt-5.5\"\n"), 0o600)
	ids := []string{"gpt-5.5", "gpt-5.4", "gpt-5.4-mini", "gpt-5.3-codex", "gpt-5.2"}
	var cache []map[string]any
	var live []catalog.Model
	for i, id := range ids {
		cache = append(cache, map[string]any{"slug": id, "display_name": id, "visibility": "list", "priority": i, "context_window": 400000})
		live = append(live, catalog.Model{ID: id, Context: 400000})
	}
	b, _ := json.Marshal(map[string]any{"etag": "x", "models": cache})
	os.WriteFile(filepath.Join(codexHome, "models_cache.json"), b, 0o600)
	provider.ForgetAccounts()
	t.Cleanup(provider.ForgetAccounts)
	catalog.Reset()
	t.Cleanup(catalog.Reset)
	if err := catalog.SaveLive("codex", provider.CodexBase, live); err != nil {
		t.Fatal(err)
	}
	return ids
}

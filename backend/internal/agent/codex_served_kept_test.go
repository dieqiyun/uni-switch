package agent

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// Which of Codex's own models uni-switch serves on the user's ChatGPT account
// is kept while nothing the catalog is built from changed: Codex's
// options, asked per field of its Drift on every drift poll, built
// Codex's whole catalog each time for these few names. A provider saved
// since is seen at once.
func TestCodexServedKeptWhileCatalogStays(t *testing.T) {
	claims := func(m map[string]any) string {
		b, _ := json.Marshal(m)
		return "h." + base64.RawURLEncoding.EncodeToString(b) + ".s"
	}
	me, _ := json.Marshal(map[string]any{"auth_mode": "chatgpt", "tokens": map[string]any{
		"id_token":      claims(map[string]any{"email": "me@example.com"}),
		"access_token":  claims(map[string]any{"exp": time.Now().Add(time.Hour).Unix()}),
		"refresh_token": "r-1", "account_id": "acct-1"}})
	home, _ := codexHome(t, string(me), "model = \"gpt-5.5\"\n")
	os.WriteFile(filepath.Join(home, ".codex", "models_cache.json"),
		[]byte(`{"models":[{"slug":"gpt-5.5","display_name":"GPT-5.5","priority":1},
			{"slug":"gpt-5.4","display_name":"GPT-5.4","priority":2}]}`), 0o644)
	var models []string
	for i := range 300 {
		models = append(models, fmt.Sprintf("m-%d", i))
	}
	if err := provider.Save(provider.Provider{ID: "relay", Name: "Relay", Key: "k", Chat: "http://127.0.0.1:1/v1", Models: models}); err != nil {
		t.Fatal(err)
	}
	// written long enough ago that its time tells a later write apart
	old := time.Now().Add(-time.Hour)
	os.Chtimes(provider.Path(), old, old)

	served := func(model string) bool {
		return codexInUniSwitch([]Option{{Value: model}})[0].Served
	}
	if !served("gpt-5.5") {
		t.Fatalf("gpt-5.5 on the user's account not marked served: %v", codexServedModels())
	}
	if served("m-1") {
		t.Fatal("a relay's model marked as served on the account")
	}
	bytesPer := func(f func()) uint64 {
		f()
		var a, b runtime.MemStats
		runtime.ReadMemStats(&a)
		for range 20 {
			f()
		}
		runtime.ReadMemStats(&b)
		return (b.TotalAlloc - a.TotalAlloc) / 20
	}
	built := bytesPer(func() { codexServedModels() })
	kept := bytesPer(func() { served("gpt-5.5") })
	if kept*20 > built {
		t.Fatalf("a set already found took %d bytes, finding it %d", kept, built)
	}

	// the account's models picked since are seen at once
	if err := provider.Save(provider.Provider{ID: "codex", Models: []string{"gpt-5.4"}}); err != nil {
		t.Fatal(err)
	}
	if served("gpt-5.5") || !served("gpt-5.4") {
		t.Fatalf("the account's picks since not followed: %v", codexServedModels())
	}
}

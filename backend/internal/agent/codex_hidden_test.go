package agent

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// A model Codex hides from its own picker (gpt-6.1-terra) but the user
// ticked on the Codex subscription is still offered in Codex's picker,
// via uni-switch, while one Codex lists is offered once, as Codex's own
// (ysm119 on Discord: 4 ticked, 3 to pick from).
func TestCodexHiddenTickedModelOffered(t *testing.T) {
	claims := func(m map[string]any) string {
		b, _ := json.Marshal(m)
		return "h." + base64.RawURLEncoding.EncodeToString(b) + ".s"
	}
	me, _ := json.Marshal(map[string]any{"auth_mode": "chatgpt", "tokens": map[string]any{
		"id_token":      claims(map[string]any{"email": "me@example.com"}),
		"access_token":  claims(map[string]any{"exp": time.Now().Add(time.Hour).Unix()}),
		"refresh_token": "r", "account_id": "acct-1"}})
	home, _ := codexHome(t, string(me), "model = \"gpt-a\"\n")
	os.WriteFile(filepath.Join(home, ".codex", "models_cache.json"), []byte(`{"models":[
		{"slug":"gpt-a","display_name":"A","priority":1},
		{"slug":"gpt-h","display_name":"H","priority":2,"visibility":"hide"}]}`), 0o644)
	if err := provider.Save(provider.Provider{ID: "codex", Models: []string{"gpt-a", "gpt-h"}}); err != nil {
		t.Fatal(err)
	}
	seen := map[string]int{}
	var all []string
	opts := codex(home).Fields[0].Options(nil)
	groups := map[string]string{}
	for _, o := range opts {
		m := o.Value
		if own := codexOwnOf(o.Value); own != "" {
			m = own
		}
		groups[m] = o.Group
	}
	// one section: the hidden one beside Codex's own, not under a heading
	// of its own (ysm119: "为啥会出现2 个分组")
	if groups["gpt-h"] != groups["gpt-a"] {
		t.Errorf("gpt-h in %q, gpt-a in %q", groups["gpt-h"], groups["gpt-a"])
	}
	for _, o := range opts {
		all = append(all, o.Group+":"+o.Value)
		m := o.Value
		if own := codexOwnOf(o.Value); own != "" {
			m = own
		}
		seen[m]++
	}
	if seen["gpt-h"] != 1 || seen["gpt-a"] != 1 {
		t.Fatalf("want gpt-a and gpt-h once each: %v", all)
	}
}

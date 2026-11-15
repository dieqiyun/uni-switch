package agent

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/gateway"
	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// Unsafe legacy listener settings fall back to the local gateway for account failover.
func TestCodexFailoverIgnoresNonlocalListenerEnv(t *testing.T) {
	claims := func(m map[string]any) string {
		b, _ := json.Marshal(m)
		return "h." + base64.RawURLEncoding.EncodeToString(b) + ".s"
	}
	auth := func(email, acct string) map[string]any {
		return map[string]any{"auth_mode": "chatgpt", "tokens": map[string]any{
			"id_token":      claims(map[string]any{"email": email}),
			"access_token":  claims(map[string]any{"exp": time.Now().Add(time.Hour).Unix()}),
			"refresh_token": "r-" + acct, "account_id": acct}}
	}
	me, _ := json.Marshal(auth("me@example.com", "acct-1"))
	home, read := codexHome(t, string(me), "model = \"gpt-5.5\"\n")
	b, _ := json.Marshal([]map[string]any{{"agent": "codex", "user": "spare@example.com", "on": true,
		"seen": time.Now(), "auth": auth("spare@example.com", "acct-2")}})
	os.MkdirAll(filepath.Dir(provider.Path()), 0o755)
	os.WriteFile(filepath.Join(filepath.Dir(provider.Path()), "logins.json"), b, 0o600)
	t.Setenv("UNI_SWITCH_ADDR", "172.23.80.1:3425")
	cx := codex(home)
	if err := cx.Sync(); err != nil {
		t.Fatal(err)
	}
	if cfg := read(); !strings.Contains(cfg, gateway.URL()+gateway.CodexPath) || strings.Contains(cfg, "172.23.80.1") {
		t.Fatalf("failover did not use the local listener:\n%s", cfg)
	}
	if s := stashLoad(); s["codex.failover"] != "1" {
		t.Fatalf("local failover was not recorded: %v", s)
	}

	// Sync removes an owned remote failover URL written by an older version.
	if err := os.WriteFile(filepath.Join(home, ".codex", "config.toml"), []byte("model = \"gpt-5.5\"\nopenai_base_url = \"http://172.23.80.1:3425/backend-api/codex\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	stash(map[string]string{"codex.failover": "1"})
	if err := codex(home).Sync(); err != nil {
		t.Fatal(err)
	}
	if cfg := read(); strings.Contains(cfg, "openai_base_url") || strings.Contains(cfg, "172.23.80.1") {
		t.Fatalf("legacy remote failover remained in the client configuration:\n%s", cfg)
	}
}

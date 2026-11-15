package agent

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// #1907 (foobra): two ChatGPT accounts, both on in uni-switch, for Codex
// Desktop and the CLI alone, not for the gateway. With more accounts on,
// uni-switch points Codex at the gateway to fail over between them (#1385);
// switch-only takes that base URL out, so Codex goes straight to OpenAI and
// uni-switch only switches its sign-in, and nothing puts it back while it is
// on. Off again, failover writes it back. A base URL the user wrote by hand
// is theirs, and stays either way (#856).
func TestCodexSwitchOnlyKeepsCodexOffTheGateway(t *testing.T) {
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

	cx := codex(home)
	if err := cx.Sync(); err != nil {
		t.Fatal(err)
	}
	if cfg := read(); !strings.Contains(cfg, "openai_base_url") || !cx.FailingOver() {
		t.Fatalf("not the reporter's state, failing over %v:\n%s", cx.FailingOver(), cfg)
	}

	if err := provider.SetSwitchOnly("codex", true); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err := cx.Sync(); err != nil {
			t.Fatal(err)
		}
		if cfg := read(); strings.Contains(cfg, "openai_base_url") || !strings.Contains(cfg, `model = "gpt-5.5"`) {
			t.Fatalf("switch-only, sync %d:\n%s", i, cfg)
		}
		if cx.FailingOver() || cx.FailoverSaid() != "" || CodexOwnOnly() {
			t.Fatalf("switch-only, sync %d: failing over %v, said %q, own only %v", i, cx.FailingOver(), cx.FailoverSaid(), CodexOwnOnly())
		}
	}

	if err := provider.SetSwitchOnly("codex", false); err != nil {
		t.Fatal(err)
	}
	if err := cx.Sync(); err != nil {
		t.Fatal(err)
	}
	if cfg := read(); !strings.Contains(cfg, "openai_base_url") || !cx.FailingOver() {
		t.Fatalf("switch-only off, failing over %v:\n%s", cx.FailingOver(), cfg)
	}

	// the user's own base URL, switch-only on: theirs, kept
	if err := cx.EndFailover(); err != nil {
		t.Fatal(err)
	}
	forget("codex.failover")
	base := codexGatewayURL()
	cfg := filepath.Join(home, ".codex", "config.toml")
	os.WriteFile(cfg, []byte("model = \"gpt-5.5\"\nopenai_base_url = \""+base+"\"\n"), 0o644)
	if err := provider.SetSwitchOnly("codex", true); err != nil {
		t.Fatal(err)
	}
	if err := cx.Sync(); err != nil {
		t.Fatal(err)
	}
	if got := read(); !strings.Contains(got, `openai_base_url = "`+base+`"`) {
		t.Fatalf("the user's base URL taken away:\n%s", got)
	}
}

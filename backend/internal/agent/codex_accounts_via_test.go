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

// #1603 (SwimmingLiu): two Codex accounts on in uni-switch, Codex on CC
// Switch's custom relay and not through uni-switch. Codex's picker had its
// GPT models direct only: the ones uni-switch serves on those accounts
// (codex/…) were left out as "Codex's own sign-in, one more hop". They
// are not Codex's sign-in while it is on a relay, and with several
// accounts uni-switch routes over them; they are offered via uni-switch, beside
// the direct ones. Listing them writes nothing. With Codex on its own
// ChatGPT sign-in and that one account alone in uni-switch, they still add
// only a hop and stay out.
func TestCodexAccountsOfferedViaUniSwitch(t *testing.T) {
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
	cache := []byte(`{"models":[{"slug":"gpt-5.5","display_name":"GPT-5.5","priority":1}]}`)
	logins := func(on bool) {
		b, _ := json.Marshal([]map[string]any{{"agent": "codex", "user": "spare@example.com", "on": on,
			"seen": time.Now(), "auth": auth("spare@example.com", "acct-2")}})
		os.MkdirAll(filepath.Dir(provider.Path()), 0o755)
		os.WriteFile(filepath.Join(filepath.Dir(provider.Path()), "logins.json"), b, 0o600)
	}
	via := func(a *Agent) (direct, uniSwitch []Option) {
		for _, o := range a.Fields[0].Options(nil) {
			switch {
			case o.Value == "gpt-5.5" && o.Direct != "":
				direct = append(direct, o)
			case strings.HasPrefix(o.Value, "codex/") && o.Direct == "" && strings.HasSuffix(o.Note, "via uni-switch"):
				uniSwitch = append(uniSwitch, o)
			}
		}
		return
	}

	t.Run("on a relay, two accounts", func(t *testing.T) {
		home, read := codexHome(t, string(me), ccSwitchCodex)
		os.WriteFile(filepath.Join(home, ".codex", "models_cache.json"), cache, 0o644)
		logins(true)
		before := read()
		direct, uniSwitch := via(codex(home))
		if len(direct) == 0 {
			t.Error("the relay's own gpt-5.5 isn't offered direct")
		}
		if !hasValue(uniSwitch, "codex/gpt-5.5") {
			t.Errorf("no codex/gpt-5.5 via uni-switch: %+v", uniSwitch)
		}
		if read() != before {
			t.Errorf("listing the models wrote config.toml:\n%s", read())
		}
	})

	t.Run("on a relay, one account", func(t *testing.T) {
		home, _ := codexHome(t, string(me), ccSwitchCodex)
		os.WriteFile(filepath.Join(home, ".codex", "models_cache.json"), cache, 0o644)
		logins(false)
		if _, uniSwitch := via(codex(home)); !hasValue(uniSwitch, "codex/gpt-5.5") {
			t.Errorf("the account in uni-switch isn't the relay Codex is on: %+v", uniSwitch)
		}
	})

	t.Run("on its own sign-in, one account", func(t *testing.T) {
		home, _ := codexHome(t, string(me), "model = \"gpt-5.5\"\n")
		os.WriteFile(filepath.Join(home, ".codex", "models_cache.json"), cache, 0o644)
		logins(false)
		if _, uniSwitch := via(codex(home)); len(uniSwitch) != 0 {
			t.Errorf("Codex's own sign-in, again via uni-switch: %+v", uniSwitch)
		}
	})
}

func hasValue(opts []Option, v string) bool {
	for _, o := range opts {
		if o.Value == v {
			return true
		}
	}
	return false
}

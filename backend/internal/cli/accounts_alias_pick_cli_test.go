package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/agent"
	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// StringKe (#4532): the name the user gave an account picks it wherever
// its email does — switch, forget, alias, the pool's --accounts, a cap —
// and is refused when it names two accounts or is another account's
// email, rather than picking one of them by guess.
func TestAccountsAliasPicksTheAccount(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
	t.Setenv("CLAUDE_CONFIG_DIR", filepath.Join(home, ".claude"))
	t.Setenv("COPILOT_HOME", filepath.Join(home, ".copilot"))
	logins := filepath.Join(filepath.Dir(provider.Path()), "logins.json")
	if err := os.MkdirAll(filepath.Dir(logins), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(logins, []byte(`[
  {"agent": "copilot", "user": "mona", "seen": "2026-10-05T10:00:00Z", "on": true, "auth": {"oauth_token": "gho_m"}},
  {"agent": "copilot", "user": "hubot", "seen": "2026-10-06T10:00:00Z", "on": true, "auth": {"oauth_token": "gho_h"}},
  {"agent": "copilot", "user": "octo", "seen": "2026-10-06T10:00:00Z", "on": true, "auth": {"oauth_token": "gho_o"}}
]`), 0o600); err != nil {
		t.Fatal(err)
	}
	provider.ForgetAccounts()
	t.Cleanup(provider.ForgetAccounts)
	t.Cleanup(agent.SyncCatalogSettled) // the pool's save syncs the agents' files behind it
	run := func(args ...string) (string, error) {
		t.Helper()
		return said(t, func() error { return accountsCmd(append([]string{"accounts"}, args...)) })
	}
	first := func() string {
		t.Helper()
		for _, l := range provider.Logins("copilot") {
			if l.Active {
				return l.User
			}
		}
		return ""
	}
	for _, a := range [][]string{{"hubot", "Work"}, {"octo", "Spare"}, {"mona", "Spare2"}} {
		if _, err := run("alias", "copilot", a[0], a[1]); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := run("switch", "copilot", "work"); err != nil || first() != "hubot" {
		t.Fatalf("switch by its name: %v, first %s", err, first())
	}
	if out, err := run("alias", "copilot", "Work"); err != nil || !strings.Contains(out, "Work") {
		t.Fatalf("alias by its name: %v %q", err, out)
	}
	// a name two accounts have
	if _, err := run("alias", "copilot", "Spare2", "Spare"); err != nil {
		t.Fatal(err)
	}
	if _, err := run("switch", "copilot", "spare"); err == nil || !strings.Contains(err.Error(), "2 accounts") || first() != "hubot" {
		t.Fatalf("a name of two accounts was taken: %v, first %s", err, first())
	}
	// a name that is another account's own
	if _, err := run("alias", "copilot", "octo", "mona"); err != nil {
		t.Fatal(err)
	}
	if _, err := run("switch", "copilot", "mona"); err == nil || !strings.Contains(err.Error(), "the email of mona") || first() != "hubot" {
		t.Fatalf("a name that is another account's was taken: %v, first %s", err, first())
	}
	if _, err := run("alias", "copilot", "octo", "Third"); err != nil {
		t.Fatal(err)
	}
	if _, err := run("switch", "copilot", "THIRD"); err != nil || first() != "octo" {
		t.Fatalf("switch by its new name: %v, first %s", err, first())
	}
	// the pool's --accounts takes the name too
	if _, err := said(t, func() error { return poolCmd([]string{"copilot", "--accounts", "third,Work"}) }); err != nil {
		t.Fatalf("pool --accounts by names: %v", err)
	}
	var on []string
	for _, l := range provider.Logins("copilot") {
		if l.On {
			on = append(on, l.User)
		}
	}
	if strings.Join(on, ",") != "octo,hubot" {
		t.Fatalf("pool by names: on %v", on)
	}
	// and a cap, as every account setting by a ref does (accountRef)
	if _, err := said(t, func() error { return accountCapCmd([]string{"copilot", "work", "50"}) }); err != nil {
		t.Fatalf("account-cap by its name: %v", err)
	}
	if got := provider.AccountCapsOf("copilot", "hubot").All; got != 50 {
		t.Fatalf("account-cap by its name: hubot's cap is %d", got)
	}
	if _, err := said(t, func() error { return poolCmd([]string{"copilot", "--accounts", "spare"}) }); err == nil {
		t.Fatal("pool --accounts took a name of two accounts")
	}
}

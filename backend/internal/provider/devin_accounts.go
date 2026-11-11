package provider

// PLUGIN-SERVED (see AGENTS.md): Devin ("devin") is a retired built-in
// subscription, served by its plugin alone,
// the retired opencode-devin-auth (provider.BuiltinRetired). Its
// sign-in, requests and usage are the plugin's; what is left here reads
// the accounts signed in to the built-in for the move onto the plugin
// (migrate_side.go), installs and runs the Devin CLI the plugin and the
// devin agent use, and shapes the models listed under "devin" (families,
// variants, contexts, images). A fix belongs in the plugin
// (github.com/dieqiyun/uni-switch, packages/devin), with the mover's
// min raised in internal/provider/migrate_side.go.

// Several Devin subscriptions. The devin CLI keeps one account, in its
// credentials.toml. Each further account the built-in signed in got a home
// of uni-switch's own, laid out as the
// CLI's data folder, so the CLI itself answers for it (`devin auth status`
// and `devin models list` run with that home as their data folder) and the
// CLI's own sign-in is never touched. logins.json names those homes; the
// CLI's own account is remembered there too, with no home, so it can stand
// behind another or be turned off.

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/dieqiyun/uni-switch/backend/internal/netproxy"
	"github.com/dieqiyun/uni-switch/backend/internal/proc"
)

// devinAccountsDir holds the homes of the Devin accounts uni-switch signed in.
func devinAccountsDir() string { return filepath.Join(filepath.Dir(Path()), "devin") }

// devinCredentialsAt is where an account's credentials.toml is: the CLI's
// own for home "", else the one in uni-switch's home.
func devinCredentialsAt(home string) string {
	if home == "" {
		return DevinCredentialsPath()
	}
	return filepath.Join(home, "devin", "credentials.toml")
}

// devinCommand runs the devin CLI for an account: the CLI's own for home
// "", else with uni-switch's home as its data folder. It goes through the
// proxy ctx names (a sign-in's), else the account's own (devinProxy), as
// the account's requests do, never always the global one (#1663).
func devinCommand(ctx context.Context, home, path string, args ...string) *exec.Cmd {
	cmd := proc.CommandContext(ctx, path, args...)
	proxy := netproxy.Choice(ctx)
	if proxy == "" {
		proxy = devinProxy(home)
	}
	if home == "" {
		cmd.Env = netproxy.EnvWith(proxy, nil)
		return cmd
	}
	var env []string
	for _, e := range os.Environ() {
		k, _, _ := strings.Cut(e, "=")
		switch strings.ToUpper(k) {
		case "XDG_DATA_HOME", "APPDATA":
			continue
		}
		env = append(env, e)
	}
	cmd.Env = netproxy.EnvWith(proxy, append(env, "XDG_DATA_HOME="+home, "APPDATA="+home))
	return cmd
}

// devinProxy is the proxy of the account signed in in home (the CLI's own
// for ""), as logins.json names it: that account's (ProxyOfLogin), or the
// subscription's for a home no account is kept for yet. It reads
// logins.json without loginsMu, which a caller may hold.
func devinProxy(home string) string {
	for _, l := range readLogins() {
		if l.Agent == "devin" && l.Home == home && (home != "" || l.own()) {
			return ProxyOfLogin("devin", l.User)
		}
	}
	return ProxyOf("devin")
}

// devinLogin is a Devin account and the home it is signed in in, "" for
// the CLI's own.
type devinLogin struct {
	Login
	Home string
}

// devinLogins lists the Devin accounts that are signed in, the first in
// use first, then the rest as they were added.
func devinLogins() []devinLogin {
	own, plan, ok := devinIdentity()
	if !ok {
		own = ""
	}
	var out []devinLogin
	for _, l := range sideLogins("devin", own, func(l savedLogin) bool {
		_, _, err := DevinAuthAt(l.Home)
		return l.Home != "" && err == nil
	}) {
		if l.saved.own() && l.Plan == "" {
			l.Plan = plan // the CLI's own, by the tier `devin auth status` says, as the plugin names it
		}
		out = append(out, devinLogin{l.Login, l.saved.Home})
	}
	return out
}

// newDevinHome makes the home a further Devin account is signed in in.
func newDevinHome() (string, error) {
	home := filepath.Join(devinAccountsDir(), strings.ToLower(strings.NewReplacer("-", "", "_", "").Replace(randomToken(9))))
	return home, os.MkdirAll(filepath.Join(home, "devin"), 0o700)
}

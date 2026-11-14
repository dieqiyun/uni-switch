package provider

// PLUGIN-SERVED (see AGENTS.md): Grok ("grok") is a retired built-in
// subscription, served by its plugin alone,
// the retired opencode-grok-auth (provider.BuiltinRetired). Its
// sign-in, models, requests and usage are the plugin's; what is left here
// reads the accounts signed in to the built-in for the move onto the plugin
// (migrate_side.go), installs and finds the Grok CLI the plugin signs in
// with, and shapes the plugin's request bodies (grokBody). A fix belongs in
// the plugin (github.com/dieqiyun/uni-switch, packages/grok), with the
// mover's min raised in internal/provider/migrate_side.go.

// Several Grok subscriptions. The Grok CLI keeps one account, in its own
// home (~/.grok), which uni-switch only ever reads. Each further account gets a
// home of uni-switch's own, signed in there by the CLI's own `login`, so the
// user's sign-in is never touched and each refresh token has one holder.
// logins.json names those homes; the CLI's own account is remembered there
// too, with no home, so it can stand behind another or be turned off.

// grokLogin is a Grok account and the home it is signed in in.
type grokLogin struct {
	Login
	Home string
}

// grokLogins lists the Grok accounts that are signed in, the first in use
// first, then the rest as they were added.
func grokLogins() []grokLogin {
	own := ""
	if c, ok := readGrokCredential(GrokHome()); ok {
		own = c.Email
	}
	var out []grokLogin
	for _, l := range sideLogins("grok", own, func(l savedLogin) bool {
		_, ok := readGrokCredential(l.Home)
		return ok
	}) {
		home := l.saved.Home
		if home == "" {
			home = GrokHome()
		}
		out = append(out, grokLogin{l.Login, home})
	}
	return out
}

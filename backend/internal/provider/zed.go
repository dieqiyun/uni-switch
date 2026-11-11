package provider

// PLUGIN-SERVED (see AGENTS.md): Zed ("zed") is a retired built-in
// subscription, served by its plugin alone,
// the retired opencode-zed-auth (provider.BuiltinRetired). Its sign-in,
// models, requests and usage are the plugin's; all that is left here is
// reading the accounts signed in to the built-in, for the move onto the
// plugin (migrate_zed.go). A fix belongs in the plugin
// (github.com/dieqiyun/uni-switch, packages/zed), with the mover's min
// raised in internal/provider/migrate_zed.go.

import "encoding/json"

// zedCreds is what a Zed sign-in left: the account's id and access token
// (which last until Zed refuses them), the machine id they were issued with,
// the organization its models are asked under, and the model list last read.
type zedCreds struct {
	UserID   string          `json:"userId"`
	Access   string          `json:"accessToken"`
	SystemID string          `json:"systemId"`
	Org      string          `json:"organizationId,omitempty"`
	Login    string          `json:"login,omitempty"`
	Name     string          `json:"name,omitempty"`
	Plan     string          `json:"plan,omitempty"` // plan_v3, e.g. zed_pro
	Models   json.RawMessage `json:"models,omitempty"`
}

func zedSaved(l savedLogin) (zedCreds, bool) {
	var c zedCreds
	if json.Unmarshal(l.Auth, &c) != nil || c.UserID == "" || c.Access == "" {
		return zedCreds{}, false
	}
	return c, true
}

type zedLogin struct {
	Login
	creds zedCreds
}

// zedLogins is every Zed account signed in to the built-in, the one in use
// first.
func zedLogins() []zedLogin {
	var out []zedLogin
	for _, l := range sideLogins("zed", "", func(l savedLogin) bool {
		_, ok := zedSaved(l)
		return ok
	}) {
		c, _ := zedSaved(l.saved)
		a := zedLogin{Login: l.Login, creds: c}
		a.Lapsed = l.saved.Lapsed
		out = append(out, a)
	}
	return out
}

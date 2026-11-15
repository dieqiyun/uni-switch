package provider

// PLUGIN-SERVED (see AGENTS.md): Xiaomi MiMo ("mimo-app") is a retired
// built-in subscription, served by its plugin alone,
// the retired opencode-mimo-auth (provider.BuiltinRetired). Its
// sign-in, models, requests and usage are the plugin's; all that is left
// here is reading the accounts signed in to the built-in, for the move onto
// the plugin (migrate_mimo.go). A fix belongs in the plugin
// (github.com/dieqiyun/uni-switch, packages/mimo), with the mover's min
// raised in internal/provider/migrate_mimo.go.

import (
	"encoding/json"
	"time"
)

// MiMoID is the MiMo subscription's id, as a provider and in logins.json.
const MiMoID = "mimo-app"

// mimoCreds is what a MiMo sign-in leaves: the Xiaomi account's id and
// passToken (which signs it on again), the device id they were issued to,
// the MiMo server it is served at and the session cookies that server set,
// and when those were set.
type mimoCreds struct {
	UserID    string            `json:"userId"`
	CUserID   string            `json:"cUserId,omitempty"`
	PassToken string            `json:"passToken"`
	DeviceID  string            `json:"deviceId"`
	Region    string            `json:"region,omitempty"`
	Base      string            `json:"base"`
	Cookies   map[string]string `json:"cookies,omitempty"`
	Issued    time.Time         `json:"issued"`
	Name      string            `json:"name,omitempty"`
}

func mimoSaved(l savedLogin) (mimoCreds, bool) {
	var c mimoCreds
	if json.Unmarshal(l.Auth, &c) != nil || c.UserID == "" || c.PassToken == "" || c.Base == "" {
		return mimoCreds{}, false
	}
	return c, true
}

type mimoLogin struct {
	Login
	creds mimoCreds
}

// mimoLogins is every MiMo account signed in, the one in use first.
func mimoLogins() []mimoLogin {
	var out []mimoLogin
	for _, l := range sideLogins(MiMoID, "", func(l savedLogin) bool {
		_, ok := mimoSaved(l)
		return ok
	}) {
		c, _ := mimoSaved(l.saved)
		a := mimoLogin{Login: l.Login, creds: c}
		a.Lapsed = l.saved.Lapsed
		out = append(out, a)
	}
	return out
}

package provider

// An account's name of the user's own (#1515, aindijrncom): a team's GLM
// seats are told apart by who holds each, while the vendor names a seat
// by a masked email (31****36@qq.com) or not at all. The name is kept in
// providers.json beside the account's other settings (AccountNames), by
// the account's name in lower case; the vendor's or plugin's sign-in is
// never written to. It is shown wherever the account is: the Usage page,
// the menu bar, uni-switch accounts, uni-switch quota and /v1/uni-switch/quotas
// (SubscriptionQuota.Alias, Quota.Alias), the Usage page's Accounts and
// the request log's Account filter, a gateway key's accounts, and the
// Routing page (which reads accountNames from /api/providers).

import (
	"cmp"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

// MaxAccountName bounds an account's name, in characters.
const MaxAccountName = 64

// cleanAccountName is a name as it is kept: spaces trimmed and run
// together, no control characters; "" for none.
func cleanAccountName(s string) string {
	s = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, s)
	return strings.Join(strings.Fields(s), " ")
}

// normalAccountNames is m as it is kept: keys in lower case, names
// cleaned, accounts with none left out; nil for none.
func normalAccountNames(m map[string]string) map[string]string {
	var out map[string]string
	for u, v := range m {
		u, v = accountKey(u), cleanAccountName(v)
		if u == "" || v == "" || utf8.RuneCountInString(v) > MaxAccountName {
			continue
		}
		if out == nil {
			out = map[string]string{}
		}
		out[u] = v
	}
	return out
}

// AccountName is the name the user gave the account user of p, "" when
// none. As with AccountCap, an account made afresh beside the agent's own
// reads it from providers.json.
func (p Provider) AccountName(user string) string {
	names := p.AccountNames
	if names == nil {
		if s, ok := storedPicks(p.ID); ok {
			names = s.AccountNames
		}
	}
	return names[accountKey(user)]
}

// AccountByName is the one of users, accounts of the subscription id, that
// the user named ref (AccountNameOf, compared without case), "" when none
// is: an alias picks an account wherever its email does (#4532). It is an
// error when the name is two accounts', or another account's email or
// name as uni-switch lists it, which would then pick one of them by guess.
func AccountByName(id string, users []string, ref string) (string, error) {
	ref = cleanAccountName(ref)
	if ref == "" {
		return "", nil
	}
	var named []string
	for _, u := range users {
		if strings.EqualFold(AccountNameOf(id, u), ref) {
			named = append(named, u)
		}
	}
	switch len(named) {
	case 0:
		return "", nil
	case 1:
	default:
		return "", fmt.Errorf("%q is the name of %d accounts, %q: name one by its email, or give them names apart (uni-switch accounts alias)", ref, len(named), named)
	}
	for _, u := range users {
		email, _, _ := strings.Cut(u, " · ")
		if !strings.EqualFold(u, named[0]) && (strings.EqualFold(u, ref) || strings.EqualFold(strings.TrimSpace(email), ref)) {
			return "", fmt.Errorf("%q is the name of %s and the email of %s: name it by its email, or give %s another name (uni-switch accounts alias)", ref, named[0], u, named[0])
		}
	}
	return named[0], nil
}

// AccountNameOf is the name the user gave the account user of the
// subscription id, "" when none.
func AccountNameOf(id, user string) string {
	if user == "" {
		return ""
	}
	if s, ok := storedPicks(id); ok {
		return s.AccountNames[accountKey(user)]
	}
	return ""
}

// AccountShown is how the account user of p is shown: by the name the
// user gave it, else by the name its plugin's usage gives it (PluginUserOf),
// else as its vendor names it. Records and lists keep the vendor's name,
// the key the user's name is kept by, and look this up as they are shown,
// so a rename reaches what came before it too
// (upstream plugins #310: a renamed Kiro account read by its old
// name on the requests after).
func (p Provider) AccountShown(user string) string {
	if n := p.AccountName(user); n != "" {
		return n
	}
	return cmp.Or(p.PluginUser(user), user)
}

// PluginUser is PluginUserOf the account user of p, "" for a provider no
// plugin gives.
func (p Provider) PluginUser(user string) string {
	if !p.IsPlugin() {
		return ""
	}
	return usageUser(p.Account.plugin.ID, user)
}

// AccountShownOf is AccountShown for the account user of the provider id.
func AccountShownOf(id, user string) string { return cmp.Or(AccountLabelOf(id, user), user) }

// AccountLabelOf is the name the account user of id goes by besides
// user itself: the user's own (AccountNameOf), else its plugin's
// (PluginUserOf); "" for none. id is a provider's or "plugin:<id>", as
// logins name a plugin's accounts.
func AccountLabelOf(id, user string) string {
	id = labelProvider(id)
	return cmp.Or(AccountNameOf(id, user), PluginUserOf(id, user))
}

// labelProvider is the provider id behind id, a provider's or
// "plugin:<id>" as logins name a plugin's accounts.
func labelProvider(id string) string {
	if p, ok := strings.CutPrefix(id, "plugin:"); ok {
		return PluginID(p)
	}
	return id
}

// PluginUserOf is the name a plugin's usage gives its account user
// (usage().user, a masked phone) where that isn't user itself, "" for
// none or one not read. A plugin's accountId is the account's key in
// uni-switch (its own name, proxy and gateway key are kept by it), so
// codearts keeps an IAM id there, iam::<domain>:user:<name>, and tells
// the phone only in usage (#4416). It is shown, never kept: what was read
// this run, else the last reading quotas.json has, so a read that fails
// or hasn't been made yet shows what was known, or user, never nothing.
func PluginUserOf(id, user string) string {
	pp, ok := pluginOfAgent(id)
	if !ok {
		return ""
	}
	return usageUser(pp.ID, user)
}

// usageUser is PluginUserOf of the plugin's provider pid.
func usageUser(pid, user string) string {
	key := strings.ToLower(strings.TrimSpace(user))
	if key == "" {
		return ""
	}
	other := func(u string) string {
		if u = strings.TrimSpace(u); strings.EqualFold(u, key) {
			return ""
		}
		return u
	}
	loginUsageCache.Lock()
	e := loginUsageCache.m["plugin:"+pid+"/"+key]
	loginUsageCache.Unlock()
	if u := other(e.q.User); u != "" {
		return u
	}
	c := &lastQuotas
	c.Lock()
	defer c.Unlock()
	c.load()
	return other(c.m[PluginID(pid)+"/"+key].Q.User)
}

// SetAccountName names the account ref of the subscription id; "" takes
// the name off.
func SetAccountName(id, ref, name string) error {
	name = cleanAccountName(name)
	if n := utf8.RuneCountInString(name); n > MaxAccountName {
		return fmt.Errorf("an account's name is at most %d characters, not %d", MaxAccountName, n)
	}
	p, err := Find(id)
	if err != nil {
		return err
	}
	if p.Account == nil {
		return fmt.Errorf("%s has keys, not signed-in accounts; a key is named in its own row", p.Name)
	}
	r, ok := p.accountRef(ref)
	if !ok {
		return fmt.Errorf("%s has no account %q", p.Name, ref)
	}
	k := accountKey(r)
	if p.AccountNames[k] == name {
		return nil
	}
	names := map[string]string{}
	for u, v := range p.AccountNames {
		names[u] = v
	}
	if name == "" {
		delete(names, k)
	} else {
		names[k] = name
	}
	p.AccountNames = names
	return Save(*p)
}

// withAccountNames is qs with each account's name of the user's own on
// its card, else the name of the key plan it stands for (Seat), as
// Alias; Named when Alias is the user's own name. qs is not changed: it
// is a cache's.
func withAccountNames(qs []SubscriptionQuota) []SubscriptionQuota {
	out := make([]SubscriptionQuota, len(qs))
	for i, q := range qs {
		if q.From == "" {
			own := AccountNameOf(labelProvider(q.Provider), q.User)
			q.Alias, q.Named = cmp.Or(own, AccountLabelOf(q.Provider, q.User), q.Seat), own != ""
		}
		out[i] = q
	}
	return out
}

package access

import (
	"errors"
	"slices"
	"strings"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// CleanAccounts is a key's account list as kept (#905): each entry
// "<provider>/<account>", an account named by its stable id — or by the
// name it is shown by, resolved to that id, so the command line reads
// while the list keeps what a rename doesn't move — and a key by its
// fingerprint; trimmed, told apart in lower case as accounts are, each
// once, in the order given. Every account the entry names must be one
// its provider has now, so a typo can't quietly hold a key to nothing;
// one gone later — a key rotated, an account signed out — is kept as it
// is, matching nothing.
func CleanAccounts(in []string) ([]string, error) {
	var out []string
	for _, a := range in {
		a = strings.TrimSpace(a)
		if a == "" {
			continue
		}
		pid, ref, ok := strings.Cut(a, "/")
		if !ok || pid == "" || ref == "" {
			return nil, errors.New("A key's account is <provider>/<account> or <provider>/<key id>, like codex/me@example.com: " + a)
		}
		if len(a) > 300 {
			return nil, errors.New("A key's account is at most 300 characters")
		}
		kept, err := provider.ResolveAccount(pid, ref)
		if err != nil {
			return nil, err
		}
		if !slices.ContainsFunc(out, func(o string) bool { return strings.EqualFold(o, kept) }) {
			out = append(out, kept)
		}
	}
	if len(out) > 500 {
		return nil, errors.New("A key can list at most 500 accounts")
	}
	return out, nil
}

// AllowsAccount says the provider pid's account or key ref is among the
// key's accounts — "<provider>/<ref>", told apart in lower case, as
// accounts are. The list holds a key to some accounts **of the providers
// it names**: a provider it names no account of, the key uses as it
// always did, its every account and key. Any, for a key that lists none.
func (who Identity) AllowsAccount(pid, ref string) bool {
	if len(who.Accounts) == 0 {
		return true
	}
	pid = strings.ToLower(strings.TrimSpace(pid))
	key := pid + "/" + strings.ToLower(strings.TrimSpace(ref))
	named := false
	for _, a := range who.Accounts {
		if a = strings.ToLower(strings.TrimSpace(a)); a == key {
			return true
		} else if p, _, ok := strings.Cut(a, "/"); ok && p == pid {
			named = true
		}
	}
	return !named
}

// heldToNone is the account an entry "<provider>/<heldToNone>" names: none
// a provider has, so the list names the provider and allows none of it.
const heldToNone = "\x00"

// HeldToGroup is who held to a routing group's accounts too (accounts,
// a Group's Accounts, in the key's form): a provider the group names is
// used only on the accounts the group names that the key may use as well,
// none when the two share none, never one either leaves out; a provider
// only the key names, on the key's; one neither names, on every account
// it has. Group is set to the group's name, for the error to say whose
// accounts held the request. A group naming no account is who as it is.
// Everything that holds a request to its key's accounts
// (AllowsAccount) holds it to the group's then, by the one rule.
func (who Identity) HeldToGroup(group string, accounts []string) Identity {
	named := map[string][]string{} // a provider the group names, in lower case: its entries as written
	var order []string
	for _, a := range accounts {
		a = strings.TrimSpace(a)
		pid, ref, ok := strings.Cut(a, "/")
		if pid = strings.ToLower(pid); !ok || pid == "" || ref == "" {
			continue
		}
		if _, seen := named[pid]; !seen {
			order = append(order, pid)
		}
		named[pid] = append(named[pid], a)
	}
	if len(order) == 0 {
		return who
	}
	var out []string
	for _, a := range who.Accounts {
		if pid, _, _ := strings.Cut(strings.TrimSpace(a), "/"); named[strings.ToLower(pid)] == nil {
			out = append(out, a)
		}
	}
	for _, pid := range order {
		some := false
		for _, a := range named[pid] {
			_, ref, _ := strings.Cut(a, "/")
			if who.AllowsAccount(pid, ref) && !slices.ContainsFunc(out, func(o string) bool { return strings.EqualFold(o, a) }) {
				out, some = append(out, a), true
			}
		}
		if !some {
			out = append(out, pid+"/"+heldToNone)
		}
	}
	who.Accounts, who.Group = out, group
	return who
}

// ShownAccounts is who's accounts as a message lists them: the entries
// HeldToGroup adds to name a provider with none of its accounts left out.
func (who Identity) ShownAccounts() []string {
	return slices.DeleteFunc(slices.Clone(who.Accounts), func(a string) bool { return strings.HasSuffix(a, "/"+heldToNone) })
}

// ReplaceAccount moves every key's account list from one of provider
// pid's keys to the fingerprint it has now (#1869: the key's value
// replaced in place), so a key held to it is held to it still. It
// returns how many keys it moved.
func ReplaceAccount(pid, was, now string) (int, error) {
	if was == now {
		return 0, nil
	}
	mu.Lock()
	defer mu.Unlock()
	keys, err := load()
	if err != nil {
		return 0, err
	}
	old := strings.ToLower(pid + "/" + was)
	moved := 0
	for i := range keys {
		for j, a := range keys[i].Accounts {
			if strings.ToLower(strings.TrimSpace(a)) == old {
				keys[i].Accounts[j] = pid + "/" + now
				moved++
			}
		}
	}
	if moved == 0 {
		return 0, nil
	}
	return moved, save(keys)
}

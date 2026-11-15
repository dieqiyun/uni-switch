package agent

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/dieqiyun/uni-switch/backend/internal/edit"
)

// Aside keeps one folder per account, ~/.aside/u/<id>, each with its own
// settings.json, models.json and credentials (#1499, fjrtdk). Which one is
// in use is ~/.aside/accounts.json's currentAccountId, which both the app
// and `aside account use <id>` read and write:
//
//	{"currentAccountId": 1, "accounts": [
//	  {"id": 1, "email": "...", "provider": "google", "mode": "cloud"},
//	  {"id": 0, "provider": "anonymous", "mode": "local"}, ...],
//	 "profileAccountBindings": {...}}
//
// uni-switch configures the account picked on uni-switch (the Aside row's account
// field, `uni-switch aside account u1`), else the one Aside has active, else u0:
// a missing or unreadable accounts.json says nothing about which account is
// active, so it is u0, as before there was a choice — never "no accounts".

type asideAccountInfo struct {
	ID       int
	Email    string
	Provider string
	Mode     string
}

// asideID reads an account id as Aside writes it (1), or as typed ("u1",
// "1"); false for anything else.
func asideID(raw json.RawMessage) (int, bool) {
	var n json.Number
	d := json.NewDecoder(strings.NewReader(string(raw)))
	d.UseNumber()
	var v any
	if d.Decode(&v) != nil {
		return 0, false
	}
	switch x := v.(type) {
	case json.Number:
		n = x
	case string:
		n = json.Number(strings.TrimPrefix(strings.TrimSpace(x), "u"))
	default:
		return 0, false
	}
	id, err := strconv.Atoi(string(n))
	if err != nil || id < 0 {
		return 0, false
	}
	return id, true
}

// asideAccounts is Aside's accounts.json: the active account and the list.
// ok is false when the file can't be read or parsed, or names no active
// account: unknown, not empty.
func asideAccounts(at place) (current int, list []asideAccountInfo, ok bool) {
	b, err := os.ReadFile(at.join(".aside", "accounts.json"))
	if err != nil {
		return 0, nil, false
	}
	var f struct {
		Current  json.RawMessage `json:"currentAccountId"`
		Accounts []struct {
			ID       json.RawMessage `json:"id"`
			Email    string          `json:"email"`
			Provider string          `json:"provider"`
			Mode     string          `json:"mode"`
		} `json:"accounts"`
	}
	if json.Unmarshal(b, &f) != nil {
		return 0, nil, false
	}
	for _, a := range f.Accounts {
		if id, valid := asideID(a.ID); valid {
			list = append(list, asideAccountInfo{ID: id, Email: a.Email, Provider: a.Provider, Mode: a.Mode})
		}
	}
	sort.Slice(list, func(i, j int) bool { return list[i].ID < list[j].ID })
	current, ok = asideID(f.Current)
	return current, list, ok
}

func asideAccountDir(at place, id int) string {
	return at.join(".aside", "u", strconv.Itoa(id))
}

func asideAccountName(id int) string { return "u" + strconv.Itoa(id) }

// asidePickPath is where uni-switch keeps the account picked on uni-switch: its
// own file, beside its record of Aside's settings.
func asidePickPath() string { return filepath.Join(filepath.Dir(stashPath()), "aside-account.json") }

// asidePicked is the account picked on uni-switch; false when none was (or the
// file can't be read, which then follows Aside rather than guessing).
func asidePicked() (int, bool) {
	b, err := os.ReadFile(asidePickPath())
	if err != nil {
		return 0, false
	}
	var f struct {
		Account json.RawMessage `json:"account"`
	}
	if json.Unmarshal(b, &f) != nil || len(f.Account) == 0 {
		return 0, false
	}
	return asideID(f.Account)
}

// asideChoice is the account uni-switch configures: the one picked on uni-switch,
// else the one Aside has active, else u0. An account whose folder isn't
// there is passed over, so uni-switch never makes one up.
func asideChoice(at place) int {
	if id, ok := asidePicked(); ok && at.isDir(asideAccountDir(at, id)) {
		return id
	}
	if id, _, ok := asideAccounts(at); ok && at.isDir(asideAccountDir(at, id)) {
		return id
	}
	return 0
}

// asideAccountOptions are Aside's accounts to pick from; none while there
// is only one (or the list can't be read) and none is picked, so the row
// shows no choice where there is none to make.
func asideAccountOptions(at place, cur string) []Option {
	active, list, ok := asideAccounts(at)
	if !ok || len(list) < 2 && cur == "" {
		return nil
	}
	var out []Option
	for _, a := range list {
		who := a.Email
		if who == "" {
			who = "Local Account"
		}
		note := who
		if a.ID == active {
			note += " · active in Aside"
		}
		out = append(out, Option{Value: asideAccountName(a.ID), Label: asideAccountName(a.ID), Note: note})
	}
	return out
}

// pickAccount keeps the account picked on uni-switch; "" follows Aside's own
// again. It changes nothing in Aside: each account's settings and what
// uni-switch remembers of them stay with that account.
func (c *asideConnection) pickAccount(v string) error {
	asideMu.Lock()
	defer asideMu.Unlock()
	v = strings.TrimSpace(v)
	if v == "" {
		if err := os.Remove(asidePickPath()); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}
	raw, _ := json.Marshal(v)
	id, ok := asideID(raw)
	if !ok {
		return fmt.Errorf("expected an Aside account such as u1, not %q", v)
	}
	if !c.at.isDir(asideAccountDir(c.at, id)) {
		return fmt.Errorf("Aside has no account %s (no %s)", asideAccountName(id), asideAccountDir(c.at, id))
	}
	b, _ := json.Marshal(map[string]string{"account": asideAccountName(id)})
	if err := os.MkdirAll(filepath.Dir(asidePickPath()), 0o700); err != nil {
		return err
	}
	return edit.WriteAtomic(asidePickPath(), append(b, '\n'))
}

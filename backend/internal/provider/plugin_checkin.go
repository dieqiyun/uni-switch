package provider

// A plugin's own daily check-in (Lemon on Discord: provider 每日签到的方法能
// 放到插件里面去做吗): a plugin whose auth hook has checkin presses its
// vendor's 签到 itself, and uni-switch runs it as it runs its own check-ins
// (checkin.go): each account in use once a Beijing day while the user has
// it on, a failure tried again later that day, what came of it on the
// account's Usage card and in Settings. A plugin that checks in takes over
// from uni-switch's own check-in through its fetch (WorkBuddy, Trae CN,
// MiniMax Code, Qoder), so an account is never checked in twice.
//
// What came of it is kept in plugin-checkin.json by provider and sign-in
// (pluginCheckinKey).

import (
	"context"
	"encoding/json"
	"log"
	"path/filepath"
	"slices"
	"sort"
	"sync"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/plugin"
	"github.com/dieqiyun/uni-switch/backend/internal/settings"
)

// pluginCheckinBy is the WorkBuddyCheckin.By of a plugin's check-in.
func pluginCheckinBy(id string) string { return "plugin:" + id }

func pluginCheckinPath() string { return filepath.Join(filepath.Dir(Path()), "plugin-checkin.json") }

var pluginCheckinMu sync.Mutex

// pluginCheckinAcct is a plugin's account checked in.
type pluginCheckinAcct struct {
	User string
	On   bool
	key  string // pluginCheckinKey
	// legacy is the account's entry before plugins#254,
	// pluginCheckinLegacyKey
	legacy string
	card   string
	do     func(context.Context) WorkBuddyCheckin
}

// pluginCheckinKey is a plugin sign-in's entry in plugin-checkin.json and
// settings.CheckinOff: the provider and the key plugin-auth.json keeps the
// sign-in under ("autoclaw", "autoclaw#x1y2z3"). Two sign-ins never share
// it, and it holds while the sign-in does: a refresh writes the same key,
// and signing in to an account already signed in goes back to its key
// (host.js settle). Its display name (AccountID) is not the account: two
// AutoClaw sign-ins share a default name the user can't change, and keyed
// on it only the first of them was ever checked in (plugins#254).
func pluginCheckinKey(id, login string) string { return id + "|" + login }

// pluginCheckinLegacyKey is what pluginCheckinKey was before plugins#254:
// the provider and the account's display name, else its sign-in key.
func pluginCheckinLegacyKey(id string, a plugin.Account) string {
	if a.AccountID != "" {
		return id + "|" + a.AccountID
	}
	return pluginCheckinKey(id, a.Key)
}

// pluginCheckinProvider is a plugin's provider that checks in, and its
// accounts.
type pluginCheckinProvider struct {
	ID, Name string
	accounts []pluginCheckinAcct
}

// pluginCheckinProviders are the plugins' providers whose plugin checks
// in, those signed in to.
var pluginCheckinProviders = func() []pluginCheckinProvider {
	var out []pluginCheckinProvider
	for _, pp := range heldPlugins() {
		if !pp.Checkin || movingNow(pp.ID) {
			continue
		}
		p := pluginCheckinProvider{ID: pp.ID, Name: pp.Name}
		card := PluginID(pp.ID)
		for _, l := range pluginLogins(pp) {
			acct := l.acct.Key
			id := pp.ID
			p.accounts = append(p.accounts, pluginCheckinAcct{User: l.User, On: l.On, key: pluginCheckinKey(id, acct), legacy: pluginCheckinLegacyKey(id, l.acct), card: card,
				do: func(ctx context.Context) WorkBuddyCheckin {
					c, err := plugin.AccountCheckin(ctx, id, acct)
					if err != nil {
						return wbCheckinFailed(err)
					}
					r := WorkBuddyCheckin{Outcome: c.Outcome, Credit: c.Credit, Streak: c.Streak, Msg: c.Message}
					// the vendor's own day turns then (Qoder's 10:00
					// UTC+8); a time that doesn't parse is not told
					if t, err := time.Parse(time.RFC3339, c.Until); err == nil {
						r.ValidUntil = t
					}
					return r
				}})
		}
		if len(p.accounts) > 0 {
			out = append(out, p)
		}
	}
	adoptLegacyPluginCheckins(out)
	return out
}

// pluginCheckinAdopted are the legacy entries (pluginCheckinLegacyKey),
// by the check-in file they were looked for in, that
// adoptLegacyPluginCheckins has carried over or found nothing kept under,
// so it reads the files once for each; under pluginCheckinMu.
var pluginCheckinAdopted = map[[2]string]bool{}

// adoptLegacyPluginCheckins carries what was kept under each account's
// entry before plugins#254 (its provider and display name) over to its
// entry now (its sign-in), so an upgrade neither checks an account in
// twice in a day nor loses the user's own Off. Kept under a name two
// sign-ins share, it is:
//
//   - a check-in, in plugin-checkin.json: the first of them in use, in
//     the order the check-in lists them, the one the old key's check-in
//     asked (it skipped the rest), and only that one: the others were
//     never asked, so they are asked now. The entry goes once carried.
//   - an account left out, in settings.CheckinOff: each of them. The user
//     switched that name off, and none of its sign-ins was checked in
//     since; one switched on only by the upgrade would be checked in
//     against the user's choice.
//
// A file that can't be read or parsed is left for later as it is, and the
// entries are carried again once it reads.
func adoptLegacyPluginCheckins(ps []pluginCheckinProvider) {
	pluginCheckinMu.Lock()
	defer pluginCheckinMu.Unlock()
	path := pluginCheckinPath()
	// an entry a sign-in keeps now is never taken for an old one
	now := map[string]bool{}
	for _, p := range ps {
		for _, a := range p.accounts {
			now[a.key] = true
		}
	}
	// each legacy entry's heirs, those in use first: the old check-in
	// skipped an account not in use before it took the key
	heirs := map[string][]string{}
	var order []string
	for _, inUse := range []bool{true, false} {
		for _, p := range ps {
			for _, a := range p.accounts {
				if a.On != inUse || a.legacy == "" || now[a.legacy] || pluginCheckinAdopted[[2]string{path, a.legacy}] {
					continue
				}
				if _, ok := heirs[a.legacy]; !ok {
					order = append(order, a.legacy)
				}
				heirs[a.legacy] = append(heirs[a.legacy], a.key)
			}
		}
	}
	if len(order) == 0 {
		return
	}
	checkins := adoptLegacyCheckinFile(path, order, heirs)
	off := adoptLegacyCheckinOff(heirs)
	if checkins && off {
		for _, l := range order {
			pluginCheckinAdopted[[2]string{path, l}] = true
		}
	}
}

// adoptLegacyCheckinFile gives each legacy entry of the check-in file at
// path to its first heir that has none of its own, and drops it; it says
// whether the file is as it should be now.
func adoptLegacyCheckinFile(path string, order []string, heirs map[string][]string) bool {
	st, err := readCheckins(path)
	if err != nil {
		return false
	}
	changed := false
	for _, l := range order {
		r, ok := st[l]
		if !ok {
			continue
		}
		if _, has := st[heirs[l][0]]; !has {
			st[heirs[l][0]] = r
		}
		delete(st, l)
		changed = true
	}
	if !changed {
		return true
	}
	out, err := json.MarshalIndent(st, "", "  ")
	if err == nil {
		err = writePrivate(path, append(out, '\n'))
	}
	if err == nil {
		wroteCheckins(path, st)
	}
	if err != nil {
		log.Printf("plugin check-in: %v", err)
		return false
	}
	return true
}

// adoptLegacyCheckinOff puts every heir of a legacy entry in
// settings.CheckinOff in its place; it says whether the settings are as
// they should be now.
func adoptLegacyCheckinOff(heirs map[string][]string) bool {
	s, err := settings.LoadChecked()
	if err != nil {
		return false
	}
	if !slices.ContainsFunc(s.CheckinOff, func(k string) bool { _, ok := heirs[k]; return ok }) {
		return true
	}
	err = settings.EditCheckinOff(func(off []string) []string {
		var out []string
		for _, k := range off {
			ks, ok := heirs[k]
			if !ok {
				ks = []string{k}
			}
			for _, k := range ks {
				if !slices.Contains(out, k) {
					out = append(out, k)
				}
			}
		}
		return out
	})
	if err != nil {
		log.Printf("plugin check-in: %v", err)
		return false
	}
	return true
}

// pluginChecksIn says whether pp's plugin presses the check-in itself:
// uni-switch's own check-in through its fetch leaves its accounts to it.
func pluginChecksIn(pp plugin.Provider) bool { return pp.Checkin }

// PluginCheckinOn says whether a plugin provider's accounts are checked in
// each day: as the user set it for the provider, else as the built-in
// check-in of its vendor is set, which the plugin took over.
func PluginCheckinOn(s settings.Settings, id string) bool {
	if on, ok := s.PluginCheckins[id]; ok {
		return on
	}
	switch id {
	case wbCN.id:
		return s.WorkBuddyCheckin
	case TraeCNID:
		return s.TraeCheckin
	case MiniMaxCodeID, MiniMaxCodeGlobalID:
		return s.MiniMaxCheckin
	case "qoder", QoderCNID:
		return s.QoderCheckin
	}
	return false
}

// pluginRechecks says whether a plugin provider's check-in is asked again
// within the Beijing day, as uni-switch's own Qoder check-in is (recheck):
// a campaign can open mid-day (Qoder CN's ran 10:00 to 09:59 the next
// day), so a done or inactive read before then said nothing about the
// day's own and was kept all day, a press too (#2696). ZCode's gift
// plans open as campaigns too, some for under a day (plugins#503). Every
// other plugin is settled for the day.
func pluginRechecks(id string) bool { return id == "qoder" || id == QoderCNID || id == "zcode" }

func (p pluginCheckinProvider) checkiner(now func() time.Time) checkiner {
	return checkiner{path: pluginCheckinPath(), now: now, mu: &pluginCheckinMu, by: pluginCheckinBy(p.ID), label: p.ID, recheck: pluginRechecks(p.ID), accounts: func() []checkinAcct {
		var out []checkinAcct
		for _, a := range p.accounts {
			out = append(out, checkinAcct{User: a.User, On: a.On, key: a.key, provider: a.card, do: a.do})
		}
		return out
	}}
}

// pluginCheckiner checks in the plugins' accounts: of those whose
// provider only says, all of them when only is nil.
type pluginCheckiner struct {
	now  func() time.Time
	only func(id string) bool
}

func (c pluginCheckiner) clock() time.Time { return c.now() }

func (c pluginCheckiner) checkinNow(ctx context.Context, soon bool) []WorkBuddyCheckin {
	if discoveryOff() {
		return nil
	}
	var out []WorkBuddyCheckin
	for _, p := range pluginCheckinProviders() {
		if c.only != nil && !c.only(p.ID) {
			continue
		}
		for _, r := range p.checkiner(c.now).checkinNow(ctx, soon) {
			r.Vendor = p.Name
			out = append(out, r)
		}
	}
	return out
}

// CheckInPlugins checks the accounts of each plugin that checks in in for
// today now, those not in yet; with ids, only those providers'. It says
// how each stands.
func CheckInPlugins(ctx context.Context, ids ...string) []WorkBuddyCheckin {
	c := pluginCheckiner{now: time.Now}
	if len(ids) > 0 {
		c.only = func(id string) bool {
			for _, x := range ids {
				if x == id {
					return true
				}
			}
			return false
		}
	}
	return c.checkinNow(ctx, true)
}

// PluginCheckin is a plugin's provider that checks in, for Settings: its
// switch and how each account's last check-in went.
type PluginCheckin struct {
	ID       string             `json:"id"`
	Name     string             `json:"name"`
	On       bool               `json:"on"`
	Checkins []WorkBuddyCheckin `json:"checkins,omitempty"`
}

// PluginCheckins are the plugins' providers that check in, each with its
// accounts' last check-ins as kept; asks nothing.
func PluginCheckins() []PluginCheckin {
	st := shownCheckins(pluginCheckinPath())
	s := settings.Load()
	var out []PluginCheckin
	for _, p := range pluginCheckinProviders() {
		pc := PluginCheckin{ID: p.ID, Name: p.Name, On: PluginCheckinOn(s, p.ID)}
		listed := map[string]bool{}
		for _, a := range p.accounts {
			if r, ok := st[a.key]; ok && a.On && !listed[a.key] {
				listed[a.key] = true
				r.User, r.By, r.Vendor = a.User, pluginCheckinBy(p.ID), p.Name
				pc.Checkins = append(pc.Checkins, r)
			}
		}
		sort.SliceStable(pc.Checkins, func(i, j int) bool { return pc.Checkins[i].User < pc.Checkins[j].User })
		out = append(out, pc)
	}
	return out
}

// HasPluginCheckin says whether a plugin that checks in is signed in.
func HasPluginCheckin() bool { return len(pluginCheckinProviders()) > 0 }

// pluginCheckinMarks is qs with the cards of the plugins' accounts that
// check in marked, as WithCheckins marks the built-ins'.
func pluginCheckinMarks(qs []SubscriptionQuota) []SubscriptionQuota {
	ps := pluginCheckinProviders()
	if len(ps) == 0 {
		return qs
	}
	st := shownCheckins(pluginCheckinPath())
	for _, p := range ps {
		var on []checkinCard
		for _, a := range p.accounts {
			if a.On {
				on = append(on, checkinCard{User: a.User, card: a.card, key: a.key})
			}
		}
		qs = markCheckins(qs, on, st, pluginCheckinBy(p.ID))
	}
	return qs
}

// KeepPluginsCheckedIn checks the plugins' accounts in each day, each
// provider's while settings say to, as KeepWorkBuddyCheckedIn does
// WorkBuddy's.
func KeepPluginsCheckedIn(ctx context.Context) {
	c := pluginCheckiner{now: time.Now, only: func(id string) bool { return PluginCheckinOn(settings.Load(), id) }}
	keepCheckedIn(ctx, "plugin", c, func() bool { return true })
}

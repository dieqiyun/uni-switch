package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/appdir"
	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// quotaRefreshGap is how soon uni-switch quota refresh asks a card's vendor
// again, as the gateway's refresh for another uni-switch keeps to
// (remoteRefreshGap): a script or a status line run in a loop shows what
// was read rather than starting Claude Code, or asking the vendor, each time.
const quotaRefreshGap = 30 * time.Second

// quotaRefreshCmd: uni-switch quota refresh <provider> [<account>] [--json] —
// the card's refresh button on the Usage page, from a terminal (#2104).
func quotaRefreshCmd(args []string) error {
	asJSON := false
	var names []string
	for _, a := range args {
		switch a {
		case "--json", "-j":
			asJSON = true
		case "help", "-h", "--help":
			fmt.Println(quotaUsage)
			return nil
		default:
			names = append(names, a)
		}
	}
	var id, user string
	switch len(names) {
	case 1:
		id = names[0]
		// <provider>/<account>, unless the whole of it is a provider (a
		// remote uni-switch's card, office/codex)
		if p, u, ok := strings.Cut(id, "/"); ok {
			if _, err := provider.Find(p); err == nil {
				id, user = p, u
			}
		}
	case 2:
		id, user = names[0], names[1]
	default:
		return fmt.Errorf("usage: uni-switch quota refresh <provider> [<account>] [--json]")
	}
	if p, err := provider.Find(id); err == nil {
		id = p.ID
	} else {
		id = strings.ToLower(id)
	}
	now := time.Now()
	var qs []provider.Quota
	if ago, recent := refreshedWithin(id, user, now); recent {
		fmt.Fprintf(os.Stderr, "read %s ago: not asked again before %s have passed, shown as it was read\n",
			ago.Round(time.Second), quotaRefreshGap)
		for _, q := range provider.KeptQuotaReport(now) {
			if refreshedCard(q, id, user) {
				qs = append(qs, q)
			}
		}
	} else {
		noteRefreshed(id, user, now)
		ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
		defer cancel()
		for _, q := range provider.RefreshReport(ctx, id, user, time.Now()) {
			if refreshedCard(q, id, user) {
				qs = append(qs, q)
			}
		}
	}
	if len(qs) == 0 {
		if user != "" {
			return fmt.Errorf("no card of %s · %s · uni-switch quota %s lists its accounts", id, user, id)
		}
		return fmt.Errorf("no subscription, plan or key balance of %s · uni-switch quota lists them", id)
	}
	if asJSON {
		b, _ := json.MarshalIndent(qs, "", "  ")
		fmt.Println(string(b))
		return nil
	}
	printQuotas(qs)
	return nil
}

// refreshedCard is whether q is the card uni-switch quota refresh asked for:
// the provider's (a remote uni-switch's every card when it is the remote), of
// the account named by its login or its alias.
func refreshedCard(q provider.Quota, id, user string) bool {
	if !strings.EqualFold(q.Provider, id) && !strings.HasPrefix(strings.ToLower(q.Provider), strings.ToLower(id)+"/") {
		return false
	}
	return user == "" || strings.EqualFold(q.User, user) || q.Alias != "" && strings.EqualFold(q.Alias, user)
}

// The times quota refresh last asked each card, kept beside uni-switch's
// other caches, so that one run after another keeps to quotaRefreshGap.
func refreshStampsPath() string { return filepath.Join(appdir.Cache(), "quota-refresh.json") }

func refreshStampKey(id, user string) string { return strings.ToLower(id + "|" + user) }

func readRefreshStamps() map[string]time.Time {
	m := map[string]time.Time{}
	if b, err := os.ReadFile(refreshStampsPath()); err == nil {
		_ = json.Unmarshal(b, &m) // unreadable: nothing asked recently, read now
	}
	return m
}

// refreshedWithin says how long ago the card was asked, when that is
// under quotaRefreshGap. An account's card counts as asked when every card
// of its provider was; the provider's cards only when all of them were.
func refreshedWithin(id, user string, now time.Time) (time.Duration, bool) {
	m := readRefreshStamps()
	best, ok := time.Duration(0), false
	for k, at := range m {
		kid, kuser, _ := strings.Cut(k, "|")
		if kid != strings.ToLower(id) || kuser != "" && kuser != strings.ToLower(user) {
			continue
		}
		if ago := now.Sub(at); ago >= 0 && ago < quotaRefreshGap && (!ok || ago < best) {
			best, ok = ago, true
		}
	}
	return best, ok
}

// noteRefreshed keeps that the card was asked at now, dropping stamps
// older than the gap.
func noteRefreshed(id, user string, now time.Time) {
	m := readRefreshStamps()
	for k, at := range m {
		if now.Sub(at) >= quotaRefreshGap {
			delete(m, k)
		}
	}
	m[refreshStampKey(id, user)] = now
	b, _ := json.Marshal(m)
	path := refreshStampsPath()
	if os.MkdirAll(filepath.Dir(path), 0o755) != nil {
		return
	}
	f, err := os.CreateTemp(filepath.Dir(path), "quota-refresh-*.tmp")
	if err != nil {
		return
	}
	_, err = f.Write(b)
	if f.Close() != nil || err != nil || os.Rename(f.Name(), path) != nil {
		os.Remove(f.Name())
	}
}

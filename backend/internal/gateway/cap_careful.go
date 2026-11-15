package gateway

// Careful caps (provider.Provider.CapCareful, plugins#349): a capped
// account's usage is read again before a request goes to it near its cap,
// and while its usage can't be told it is held, as one at its cap is:
// unknown is not spent. Off, the default, routing goes by the last
// reading, read again each minute in the background.

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// What careful means, as the thread pins no figures: an account within
// carefulNear points of a cap is read again when its reading is older than
// carefulFresh, any capped account when older than carefulOld, and a
// request waits at most carefulWait for the reading.
const (
	carefulNear  = 5.0
	carefulFresh = 15 * time.Second
	carefulOld   = 5 * time.Minute
	carefulWait  = 10 * time.Second
	// carefulRetry: an account whose reading failed is asked again at
	// most this often, held meanwhile, so a vendor's usage endpoint that
	// is down isn't asked at every request
	carefulRetry = 15 * time.Second
)

// usageRead and readUsageNow are provider's, stood in for by tests.
var (
	usageRead    = provider.UsageRead
	readUsageNow = provider.ReadUsageNow
)

// carefulDoubt says why the account of a careful provider, capped by caps,
// can't be sent model at now on what is known of its usage: never read
// well, its last read failed, its reading older than carefulOld, or older
// than carefulFresh within carefulNear points of a cap; "" when it can.
func carefulDoubt(a *provider.Account, al provider.Allowance, known bool, model string, caps provider.WindowCaps, now time.Time) string {
	at, failed, ok := usageRead(a.UsageAgent(), a.User)
	age := now.Sub(at)
	switch {
	case !ok:
		return "its usage hasn't been read yet"
	case failed:
		return "its usage couldn't be read"
	case at.IsZero() || !known:
		return "its usage hasn't been read yet"
	case age > carefulOld:
		return fmt.Sprintf("its usage was last read %s ago", age.Round(time.Second))
	case age > carefulFresh && al.NearCap(model, caps, carefulNear, now):
		return fmt.Sprintf("it is near its cap and its usage was last read %s ago", age.Round(time.Second))
	}
	return ""
}

// carefulTried: when each account's usage was last read again for a
// careful provider, by agent/user (carefulRetry).
var carefulTried struct {
	sync.Mutex
	at map[string]time.Time
}

// readCareful reads again, before a request is planned, the usage of each
// capped account of the careful providers among ps (with the model each
// is asked for) that carefulDoubt would hold, and waits for it at most
// carefulWait: a reading back in time lets the plan send to it.
func readCareful(ctx context.Context, ps []provider.Provider, models []string) {
	now := time.Now()
	users := map[string][]string{} // usage agent → accounts
	carefulTried.Lock()
	if carefulTried.at == nil {
		carefulTried.at = map[string]time.Time{}
	}
	for i, p := range ps {
		if !p.CapCareful || p.Account == nil {
			continue
		}
		for _, acct := range append([]provider.Provider{p}, p.AlsoOn()...) {
			a := acct.Account
			if a == nil {
				continue
			}
			caps := provider.HoldCaps(p, a.Agent, a.User)
			if !caps.Holds() {
				continue
			}
			al, known := allowances(a.UsageAgent())[a.User]
			if carefulDoubt(a, al, known, models[i], caps, now) == "" {
				continue
			}
			key := a.UsageAgent() + "/" + strings.ToLower(a.User)
			if now.Sub(carefulTried.at[key]) < carefulRetry {
				continue
			}
			carefulTried.at[key] = now
			users[a.UsageAgent()] = append(users[a.UsageAgent()], a.User)
		}
	}
	carefulTried.Unlock()
	if len(users) == 0 {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, carefulWait)
	defer cancel()
	var wg sync.WaitGroup
	for agent, us := range users {
		wg.Go(func() { readUsageNow(ctx, agent, us...) })
	}
	wg.Wait()
}

// carefulOf is the providers a request for model on p may go to, with the
// model each is asked: p, then its fallbacks, as plan takes them; a
// group's members.
func carefulOf(p provider.Provider, model string, ms []provider.Member) ([]provider.Provider, []string) {
	var ps []provider.Provider
	var models []string
	if ms != nil {
		for _, m := range ms {
			ps, models = append(ps, m.Provider), append(models, m.Model)
		}
		return ps, models
	}
	ps, models = append(ps, p), append(models, model)
	for _, m := range provider.FallbackModels(p) {
		ps, models = append(ps, m.Provider), append(models, m.Model)
	}
	return ps, models
}

// anyCareful says whether any of ps is careful, so a request to none of
// them reads nothing more.
func anyCareful(ps []provider.Provider) bool {
	for _, p := range ps {
		if p.CapCareful {
			return true
		}
	}
	return false
}

// capShare is the cap caps hold an account at, for saying so: its own,
// else the lowest a window of it has, else 100 (credits not spent).
func capShare(caps provider.WindowCaps) int {
	if caps.All >= provider.MinCap && caps.All <= provider.MaxCap {
		return caps.All
	}
	share := 100
	for _, v := range caps.Windows {
		if v >= provider.MinCap && v < share {
			share = v
		}
	}
	return share
}

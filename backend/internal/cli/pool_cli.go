package cli

// `uni-switch provider pool`: a subscription's accounts as one pool, the
// GUI's account list in one command (plugins#349) — which take part and in
// what order, each one's cap, whether a request ends at the caps or goes
// on to the fallbacks, and whether usage is read again near a cap.

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/dieqiyun/uni-switch/backend/internal/agent"
	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

const poolUsage = "uni-switch provider pool <id> [--accounts a,b,…] [--cap percent|off] [--stop on|off] [--careful on|off]"

// poolCmd shows a subscription's pool, after setting what the flags say:
// --accounts puts those accounts in use in that order, the rest out of
// it; --cap caps each account in use (the listed ones, with --accounts);
// --stop answers 429 while every account is at its cap rather than going
// on to the fallbacks (provider.Provider.CapStop); --careful reads usage
// again near a cap and holds an account whose usage can't be told
// (provider.Provider.CapCareful).
func poolCmd(rest []string) error {
	usage := fmt.Errorf("%s", poolUsage)
	var id string
	var accounts []string
	accountsSet, capSet := false, false
	capN := 0
	var stop, careful *bool
	onOff := func(v string) (*bool, error) {
		switch strings.ToLower(strings.TrimSpace(v)) {
		case "on", "true", "yes":
			b := true
			return &b, nil
		case "off", "false", "no":
			b := false
			return &b, nil
		}
		return nil, fmt.Errorf("%q: on or off\n%s", v, poolUsage)
	}
	for i := 0; i < len(rest); i++ {
		a := rest[i]
		name, val, hasVal := strings.Cut(a, "=")
		if !strings.HasPrefix(name, "--") {
			if id != "" {
				return usage
			}
			id = a
			continue
		}
		if !hasVal {
			if i+1 >= len(rest) {
				return usage
			}
			i++
			val = rest[i]
		}
		var err error
		switch name {
		case "--accounts":
			accountsSet = true
			for _, u := range strings.Split(val, ",") {
				if u = strings.TrimSpace(u); u != "" {
					accounts = append(accounts, u)
				}
			}
			if len(accounts) == 0 {
				return fmt.Errorf("--accounts names at least one account\n%s", poolUsage)
			}
		case "--cap":
			capSet = true
			if capN, err = provider.ParseCap(val); err != nil {
				return err
			}
		case "--stop":
			stop, err = onOff(val)
		case "--careful":
			careful, err = onOff(val)
		default:
			return usage
		}
		if err != nil {
			return err
		}
	}
	if id == "" {
		return usage
	}
	p, err := provider.Find(id)
	if err != nil {
		return err
	}
	if p.Account == nil {
		return fmt.Errorf("%s has keys, not subscription accounts with usage windows to pool", p.Name)
	}
	agentID := poolAgent(*p)
	ls := provider.Logins(agentID)
	if accountsSet {
		// every one named is known before anything changes
		var order, users []string
		for _, l := range ls {
			users = append(users, l.User)
		}
		for _, u := range accounts {
			// an account by its name or the name the user gave it (#4532)
			if n, err := provider.AccountByName(p.ID, users, u); err != nil {
				return err
			} else if n != "" {
				u = n
			}
			i := slices.IndexFunc(ls, func(l provider.Login) bool { return strings.EqualFold(l.User, u) })
			if i < 0 {
				return fmt.Errorf("%s has no account %q", p.Name, u)
			}
			order = append(order, ls[i].User)
		}
		for _, l := range ls {
			if !slices.ContainsFunc(order, func(u string) bool { return strings.EqualFold(u, l.User) }) {
				order = append(order, l.User)
			}
		}
		if err := provider.SetAccountOrder(p.ID, order); err != nil {
			return err
		}
		moved, err := agent.Reseat(func() error {
			// on first, so the one the agent is signed in to can be
			// paused once another is on
			for _, u := range order[:len(accounts)] {
				if err := provider.SetLoginOn(agentID, u, true); err != nil {
					return err
				}
			}
			for _, u := range order[len(accounts):] {
				if err := provider.SetLoginOn(agentID, u, false); err != nil {
					return err
				}
			}
			return nil
		})
		if err != nil {
			return err
		}
		printMoved(moved)
		agent.SyncCatalogSoon()
		ls = provider.Logins(agentID)
	}
	if capSet {
		var users []string
		if accountsSet {
			users = accounts
		} else {
			for _, l := range ls {
				if poolOn(l) {
					users = append(users, l.User)
				}
			}
		}
		for _, u := range users {
			if err := provider.SetAccountCap(p.ID, u, capN); err != nil {
				return err
			}
		}
	}
	if stop != nil {
		if err := provider.SetCapStop(p.ID, *stop); err != nil {
			return err
		}
	}
	if careful != nil {
		if err := provider.SetCapCareful(p.ID, *careful); err != nil {
			return err
		}
	}
	if accountsSet || capSet || stop != nil || careful != nil {
		fmt.Println(green.Render("✓"), "saved", p.Name, muted.Render("("+p.ID+")"))
		if p, err = provider.Find(p.ID); err != nil {
			return err
		}
	}
	printPool(*p, ls)
	return nil
}

// poolAgent is the agent p's accounts are listed and switched under: a
// plugin's by its provider's id.
func poolAgent(p provider.Provider) string {
	if p.IsPlugin() {
		return p.ID
	}
	return p.Account.Agent
}

// poolOn says whether l takes part in the pool: on, the one signed in
// unless paused.
func poolOn(l provider.Login) bool {
	return l.On && !l.Paused || l.Active && !l.Paused
}

// printPool shows p's pool: each account in order, whether it is in use
// and its cap, then how the pool ends at its caps.
func printPool(p provider.Provider, ls []provider.Login) {
	if len(ls) == 0 {
		fmt.Println(muted.Render(p.Name + " has no account"))
		return
	}
	uncapped := 0
	for i, l := range ls {
		mark := muted.Render("off")
		if poolOn(l) {
			mark = green.Render("on ")
		}
		caps := p.CapsOf(l.User)
		capText := muted.Render("no cap: used to 100%")
		switch {
		case caps.All > 0:
			capText = "capped at " + strconv.Itoa(caps.All) + "%"
		case len(caps.Windows) > 0:
			capText = "capped on some windows"
		default:
			if poolOn(l) {
				uncapped++
			}
		}
		fmt.Printf("%d. %s %s · %s\n", i+1, mark, l.User, capText)
	}
	stop := "on: a request is answered 429 while every account in use is at its cap"
	if !p.CapStop {
		stop = "off: at the caps, a request goes on to the fallbacks"
		if len(p.Fallback) == 0 {
			stop = "off: at the caps, a request goes on to the fallbacks (none set, so it is answered 429)"
		}
	}
	careful := "on: near a cap (within 5 points) usage is read again if older than 15s, any reading older than 5m too, and an account whose usage can't be read is held"
	if !p.CapCareful {
		careful = "off: routing goes by the last reading, read again each minute"
	}
	fmt.Println("stop at caps ·", stop)
	fmt.Println("check usage near the cap ·", careful)
	if uncapped > 0 {
		fmt.Println(muted.Render(fmt.Sprintf("%d account(s) in use have no cap, so nothing holds them before the vendor does: uni-switch provider pool %s --cap 90", uncapped, p.ID)))
	}
}

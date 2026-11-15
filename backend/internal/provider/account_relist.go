package provider

// A signed-in account's model list is fetched once by FetchNew, when uni-switch
// has none for it yet, and after that only when Refresh is pressed. A model
// the vendor adds to the plan later never reached a uni-switch left running, nor
// one restarted, as the list it had was not new: OpenAI served GPT-6.1 Terra
// to ChatGPT accounts on 10-26, and Codex routed through uni-switch still listed
// what the ChatGPT backend had on 10-12 (cliff_26651 on Discord). The uni-switch
// serving the gateway now asks each account's vendor again once the list it
// has is accountListAge old, as KeepPluginsListed does for a plugin's.

import (
	"context"
	"log"
	"time"
)

// accountRelistEvery is how often the gateway's uni-switch looks for an
// account's list that is due; accountRelistFirst is how long after it
// starts it first does (FetchNew lists the new accounts meanwhile);
// accountListAge is how old a list is before it is asked for again.
var (
	accountRelistEvery = time.Hour
	accountRelistFirst = 2 * time.Minute
	accountListAge     = 45 * time.Minute
)

// KeepAccountsListed fetches each signed-in account's list again once it
// is accountListAge old, in the uni-switch serving the gateway.
func KeepAccountsListed(ctx context.Context) {
	t := time.NewTimer(accountRelistFirst)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		relistAccounts(ctx, time.Now())
		t.Reset(accountRelistEvery)
	}
}

// relistAccounts fetches the list of each account whose list is older than
// accountListAge at now, and answers the ids it asked. An account with no
// list yet is FetchNew's; a plugin's is listed by KeepPluginsListed; one
// switched off isn't asked. A fetch that fails keeps the list there was
// (the fetch saves only what it got), and is tried at the next look.
func relistAccounts(ctx context.Context, now time.Time) []string {
	var asked []string
	for _, p := range All() {
		if p.Off || p.Account == nil || p.Account.fetch == nil || p.IsPlugin() || !p.Ready() {
			continue
		}
		at, ok := p.Fetched()
		if !ok || now.Sub(at) < accountListAge {
			continue
		}
		if ctx.Err() != nil {
			return asked
		}
		asked = append(asked, p.ID)
		fctx, cancel := context.WithTimeout(ctx, time.Minute)
		ms, err := p.Fetch(fctx)
		cancel()
		if err != nil {
			log.Printf("%s: fetching its list again failed: %s (the last list stays)", p.ID, err)
			continue
		}
		log.Printf("%s: fetched its list again, %d models", p.ID, len(ms))
	}
	return asked
}

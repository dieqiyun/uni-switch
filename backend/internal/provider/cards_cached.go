package provider

import (
	"slices"
	"time"
)

func CachedCards(now time.Time) []SubscriptionQuota {
	c := &subscriptionUsageCache
	c.Lock()
	data := c.data
	c.Unlock()
	subs := withDailyCredits(visibleQuotas(withAccountReadings(data)), now)
	p := &planQuotaCache
	p.Lock()
	plans := slices.Clone(p.data)
	p.Unlock()
	b := &keyBalanceCache
	b.Lock()
	balances := slices.Clone(b.data)
	b.Unlock()
	ps := keyCardProviders()
	if plans == nil || balances == nil {
		keptPlans, keptBalances := keptKeyCards(ps)
		if plans == nil {
			plans = keptPlans
		}
		if balances == nil {
			balances = keptBalances
		}
	}
	plans, balances = withKeyReadings(plans, ps, "plan"), withKeyReadings(balances, ps, "balance")
	plans, subs = seatCards(plans, subs)
	subs = withAccountNames(subs)
	out := []SubscriptionQuota{}
	for _, g := range []struct {
		kind string
		qs   []SubscriptionQuota
	}{{"subscription", subs}, {"plan", plans}, {"balance", balances}} {
		for _, q := range g.qs {
			if q.From == "" {
				q.Kind = g.kind
				out = append(out, q)
			}
		}
	}
	return out
}

func ForgetKeptCardsForTest() {
	c := &lastQuotas
	c.Lock()
	c.m, c.loaded = nil, false
	c.Unlock()
}

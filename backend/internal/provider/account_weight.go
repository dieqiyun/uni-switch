package provider

// An account's weight (rubbzha on X): a subscription routed by weight
// spreads its requests over its accounts as a provider's keys are (#841),
// each by the weight set beside it — a Max account allowing several times
// what a Pro one does takes that many requests for each the Pro one takes,
// so both are used to the end rather than the first run out mid-task. It
// is kept per account by its name in lower case (AccountWeights), as its
// other settings are; one with none weighs 1. Nothing sets one for the
// user, whatever the plan: the plans' allowances aren't told in requests.

import (
	"fmt"
	"strconv"
	"strings"
)

// AccountWeight is the weight of the account user of p under Weighted
// routing, 1 when it has none. The accounts All and AlsoOn give carry it,
// as the gateway routes them; it is read from p alone, with no file read,
// as it is asked for every account of every request weighed.
func (p Provider) AccountWeight(user string) int {
	return max(p.AccountWeights[accountKey(user)], 1)
}

// SetAccountWeight gives the account ref of the subscription id its weight
// under Weighted routing, 1 to MaxKeyWeight; 0 or 1 is the default, kept
// as none.
func SetAccountWeight(id, ref string, weight int) error {
	if weight < 0 || weight > MaxKeyWeight {
		return fmt.Errorf("an account's weight is 1 to %d, not %d", MaxKeyWeight, weight)
	}
	p, err := Find(id)
	if err != nil {
		return err
	}
	if p.Account == nil {
		return fmt.Errorf("%s has keys, not subscription accounts: a key's weight is set on the key", p.Name)
	}
	r, ok := p.accountRef(ref)
	if !ok {
		return fmt.Errorf("%s has no account %q", p.Name, ref)
	}
	k := accountKey(r)
	if max(p.AccountWeights[k], 1) == max(weight, 1) {
		return nil
	}
	m := map[string]int{}
	for u, w := range p.AccountWeights {
		m[u] = w
	}
	if weight <= 1 {
		delete(m, k)
	} else {
		m[k] = weight
	}
	p.AccountWeights = m
	return Save(*p)
}

// ParseWeight reads a weight as the CLI takes it: a whole number from 1
// to MaxKeyWeight, or default (-, 0) for 1.
func ParseWeight(s string) (int, error) {
	s = strings.ToLower(strings.TrimSpace(s))
	switch s {
	case "default", "-", "0":
		return 1, nil
	}
	n, err := strconv.Atoi(s)
	if err != nil || n < 1 || n > MaxKeyWeight {
		return 0, fmt.Errorf("a weight is a whole number from 1 to %d, or default, not %q", MaxKeyWeight, s)
	}
	return n, nil
}

// normalAccountWeights is m as it is kept: keys in lower case, weights
// within 2–MaxKeyWeight, the default 1 left out; nil for none.
func normalAccountWeights(m map[string]int) map[string]int {
	var out map[string]int
	for u, w := range m {
		u = accountKey(u)
		if u == "" || w <= 1 {
			continue
		}
		if out == nil {
			out = map[string]int{}
		}
		out[u] = min(w, MaxKeyWeight)
	}
	return out
}

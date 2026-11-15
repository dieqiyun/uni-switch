package provider

// Ollama Cloud tells a key its account's money at ollama.com/api/balance
// (#1888): what is left of the plan's included allowance this period, and
// the credits bought on top of it. The allowance is a plan card with one
// window (PlanQuotas), the bought credits a balance card (KeyBalances).

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// ollamaBalanceURL is a var for tests to point at a server of their own.
var ollamaBalanceURL = "https://ollama.com/api/balance"

// ollamaBalance is the reply:
//
//	{"included":{"balance_usd":72.5,"allowance_usd":100,
//	   "period":{"from":"2026-09-15T09:30:00Z","until":"2026-10-15T09:30:00Z"}},
//	 "purchased":{"balance_usd":25.0}}
//
// A legacy (early) plan has no allowance in dollars but a session and a
// weekly limit, each as what is left of it (#1888, yangmingyuan380):
//
//	{"included":{"session":{"remaining_percent":75,"resets_at":"2026-10-01T07:00:00Z"},
//	   "weekly":{"remaining_percent":40,"resets_at":"2026-10-05T00:00:00Z"}},
//	 "purchased":{"balance_usd":25.0}}
//
// Each amount is kept as it came, so one left out (a plan with no included
// allowance, an account that never bought credits) is unknown, not $0.
type ollamaBalance struct {
	Included *struct {
		Balance   any `json:"balance_usd"`
		Allowance any `json:"allowance_usd"`
		Period    *struct {
			From  string `json:"from"`
			Until string `json:"until"`
		} `json:"period"`
		Session *ollamaLimit `json:"session"`
		Weekly  *ollamaLimit `json:"weekly"`
	} `json:"included"`
	Purchased *struct {
		Balance any `json:"balance_usd"`
	} `json:"purchased"`
}

// ollamaLimit is a legacy plan's session or weekly limit.
type ollamaLimit struct {
	Remaining any    `json:"remaining_percent"`
	ResetsAt  string `json:"resets_at"`
}

// window is the limit as a window, used = 100 - remaining; a limit that
// doesn't say what is left is no window.
func (l *ollamaLimit) window(name string, span time.Duration) (QuotaWindow, bool) {
	if l == nil {
		return QuotaWindow{}, false
	}
	left, ok := number(l.Remaining)
	if !ok {
		return QuotaWindow{}, false
	}
	w := QuotaWindow{Name: name, Used: min(100, max(0, 100-left)), Span: span}
	if at, err := time.Parse(time.RFC3339, l.ResetsAt); err == nil {
		w.ResetsAt = &at
	}
	return w, true
}

// errNoBalance is a reply that tells no balance for this card to show: not
// a failure to say on one, so the card is left out.
var errNoBalance = errors.New("no balance in the reply")

func parseOllamaBalance(b []byte) (ollamaBalance, error) {
	var r ollamaBalance
	if err := json.Unmarshal(b, &r); err != nil {
		return r, err
	}
	return r, nil
}

// readOllamaPlan is the included allowance as a window: what the period
// has spent of it, in USD, renewing at the period's end. A reply without
// an allowance or without what is left of it has no window, and no card.
// Spending past the allowance draws on the bought credits, so a spent
// allowance with credits left doesn't stop the key (Aside).
func readOllamaPlan(b []byte) (string, []QuotaWindow, error) {
	r, err := parseOllamaBalance(b)
	if err != nil {
		return "", nil, err
	}
	out := []QuotaWindow{}
	in := r.Included
	if in == nil {
		return "", out, nil
	}
	bought := false
	if r.Purchased != nil {
		if v, ok := number(r.Purchased.Balance); ok && v > 0 {
			bought = true
		}
	}
	// A legacy plan's limits. How long Ollama's session is the reply
	// doesn't say, so it has no span; a week is a week.
	if w, ok := in.Session.window("Session limit", 0); ok {
		w.Aside = bought
		out = append(out, w)
	}
	if w, ok := in.Weekly.window("Weekly", 7*24*time.Hour); ok {
		w.Aside = bought
		out = append(out, w)
	}
	allowance, ok1 := number(in.Allowance)
	left, ok2 := number(in.Balance)
	if !ok1 || !ok2 || allowance <= 0 {
		return "", out, nil
	}
	used := min(allowance, max(0, allowance-left))
	w := QuotaWindow{Name: "Allowance", Amount: used, Limit: allowance, Unit: "USD",
		Used: min(100, max(0, used/allowance*100))}
	if p := in.Period; p != nil {
		until, uerr := time.Parse(time.RFC3339, p.Until)
		if uerr == nil {
			w.ResetsAt = &until
		}
		if from, ferr := time.Parse(time.RFC3339, p.From); ferr == nil && uerr == nil && until.After(from) {
			w.Span = until.Sub(from)
			if d := w.Span.Hours() / 24; d >= 27 && d <= 32 {
				w.Name = "Month"
			}
		}
	}
	w.Aside = bought
	return "", append(out, w), nil
}

// readOllamaCredits is the credits bought on top of the plan, in dollars.
// A reply that names none has no balance card: the allowance is the plan
// card's.
func readOllamaCredits(b []byte) (string, error) {
	r, err := parseOllamaBalance(b)
	if err != nil {
		return "", err
	}
	if r.Purchased == nil {
		if r.Included == nil {
			return "", fmt.Errorf("Ollama's reply has no included or purchased balance: %s", shapeOf(b))
		}
		return "", errNoBalance
	}
	v, ok := number(r.Purchased.Balance)
	if !ok {
		return "", errNoBalance
	}
	return money("$", v), nil
}

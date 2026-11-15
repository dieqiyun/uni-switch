package gui

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/dieqiyun/uni-switch/backend/internal/provider"
	"github.com/dieqiyun/uni-switch/backend/internal/usage"
	"net/http"
	"time"
)

// Account allowances remain available to routing, account controls and the tray.
func accountAllowanceRoutes(mux *http.ServeMux) {
	accountAutomationRoutes(mux)
	mux.HandleFunc("GET /api/accounts/quotas", func(rw http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		if q := r.URL.Query(); q.Get("asked") != "" {
			if q.Get("all") != "" {
				provider.AskEveryClaudeUsage()
			} else {
				provider.AskClaudeUsage()
			}
			ctx = provider.Asked(ctx)
		}
		ctx, cancel := context.WithTimeout(ctx, 12*time.Second)
		defer cancel()
		// a WorkBuddy (China) account's card says how its daily check-in
		// went (#694)
		qs := provider.InProviderOrder(provider.WithCheckins(provider.Quotas(ctx)))
		// and what each window holds whole, by what uni-switch routed in it
		qs = usage.WithWindowHolds(qs, usage.Clock())
		// and when each began, for its Pace number (fflake33)
		qs = provider.WithWindowStarts(qs, usage.Clock())
		// and which an account can't spend for another window used up,
		// for the accounts' sum (#4791)
		qs = provider.WithHeldWindows(qs, usage.Clock())
		// and which the menu bar can show, by kind, for Settings to pick
		// among (settings.TrayWindows)
		qs = withTrayKinds(qs)
		// an account's card may be the stale copy a read under way will
		// replace: the page asks again until it has landed (#959)
		if provider.SubscriptionUsageReading() {
			rw.Header().Set("X-Uni-Switch-Reading", "1")
		}
		writeJSON(rw, qs)
	})
	// One card read again, from its refresh button (#840): ?provider= and,
	// of a card with several accounts, &user=; the others are left as they
	// were read. The answer is every card, as GET's.
	mux.HandleFunc("POST /api/accounts/quotas/refresh", func(rw http.ResponseWriter, r *http.Request) {
		id := r.URL.Query().Get("provider")
		if id == "" {
			http.Error(rw, "provider is required", http.StatusBadRequest)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
		defer cancel()
		provider.RefreshUsage(ctx, id, r.URL.Query().Get("user"))
		writeJSON(rw, withTrayKinds(provider.WithHeldWindows(provider.WithWindowStarts(usage.WithWindowHolds(provider.InProviderOrder(provider.WithCheckins(provider.Quotas(ctx))), usage.Clock()), usage.Clock()), usage.Clock())))
	})
	// WorkBuddy's daily check-in pressed now, from the Usage card, for
	// each account not in yet today, as `uni-switch accounts checkin` does; the
	// card is read again after
	mux.HandleFunc("POST /api/accounts/workbuddy-checkin", func(rw http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), time.Minute)
		defer cancel()
		rs := provider.CheckInWorkBuddy(ctx)
		if rs == nil {
			rs = []provider.WorkBuddyCheckin{}
		}
		writeJSON(rw, rs)
	})
	// and Trae CN's, for each Trae CN account (#694)
	mux.HandleFunc("POST /api/accounts/trae-checkin", func(rw http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), time.Minute)
		defer cancel()
		rs := provider.CheckInTrae(ctx)
		if rs == nil {
			rs = []provider.WorkBuddyCheckin{}
		}
		writeJSON(rw, rs)
	})
	// and MiniMax Code's, for each MiniMax Code (China) account (#811)
	mux.HandleFunc("POST /api/accounts/minimax-checkin", func(rw http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), time.Minute)
		defer cancel()
		rs := provider.CheckInMiniMax(ctx)
		if rs == nil {
			rs = []provider.WorkBuddyCheckin{}
		}
		writeJSON(rw, rs)
	})
	// and Qoder's daily credits, for each Qoder and Qoder CN account
	mux.HandleFunc("POST /api/accounts/qoder-checkin", func(rw http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), time.Minute)
		defer cancel()
		rs := provider.CheckInQoder(ctx)
		if rs == nil {
			rs = []provider.WorkBuddyCheckin{}
		}
		writeJSON(rw, rs)
	})
	// and a plugin's own, for each account of the provider it names

	// and one account's, from Settings' Check in now (xiaowu_95660), by
	// its vendor and key as /api/settings lists them: asked once now,
	// whatever its switch says, which it leaves as it is
	mux.HandleFunc("POST /api/accounts/checkin-account", func(rw http.ResponseWriter, r *http.Request) {
		var in struct{ By, Key string }
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil || in.Key == "" {
			fail(rw, errors.New("no account"))
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), time.Minute)
		defer cancel()
		rs := provider.CheckInAccount(ctx, in.By, in.Key)
		if len(rs) == 0 {
			fail(rw, errors.New("No daily check-in reaches this account"))
			return
		}
		writeJSON(rw, rs[0])
	})
	// what was left of each window over time, for the quota cards' curves
	// (#651); ?days= back
	mux.HandleFunc("GET /api/accounts/quotas/history", func(rw http.ResponseWriter, r *http.Request) {
		days := r.URL.Query().Get("days")
		hs := provider.QuotaHistories(provider.QuotaHistorySince(days, time.Now()), "", "")
		// and a remote uni-switch's, for its cards (office/codex)
		writeJSON(rw, hs)
	})
	// Historical whole-window estimates come only from this computer's
	// readings and ledger. Selecting a cycle never asks a vendor.
	mux.HandleFunc("GET /api/accounts/quotas/history/holds", func(rw http.ResponseWriter, r *http.Request) {
		args := r.URL.Query()
		from, e1 := time.Parse(time.RFC3339Nano, args.Get("from"))
		to, e2 := time.Parse(time.RFC3339Nano, args.Get("to"))
		now := usage.Clock()
		if e1 != nil || e2 != nil || !from.Before(to) || to.After(now) || args.Get("provider") == "" || args.Get("user") == "" {
			http.Error(rw, "provider, user and a completed time range are required", http.StatusBadRequest)
			return
		}
		hist := provider.QuotaHistories(now.Add(-45*24*time.Hour), args.Get("provider"), args.Get("user"))
		writeJSON(rw, usage.HistoricalWindowHolds(provider.CachedCards(now), hist, from, to))
	})
	// spends one of a Codex account's rate-limit resets, which the page
	// has asked the user about first: the one they chose (Credit, an id
	// of its resets.each), else the one that runs out first; what it did
	// comes back. A card from a remote uni-switch (Provider "office/codex")
	// has that uni-switch spend it, on its account (#3850)
	mux.HandleFunc("POST /api/accounts/codex-reset", func(rw http.ResponseWriter, r *http.Request) {
		var in struct{ Provider, User, Credit string }
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			fail(rw, err)
			return
		}

		ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
		defer cancel()
		out, err := provider.SpendCodexReset(ctx, in.User, in.Credit)
		if err != nil {
			fail(rw, err)
			return
		}
		writeJSON(rw, out)
	})
}

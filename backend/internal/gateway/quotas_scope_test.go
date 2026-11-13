package gateway

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/access"
	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// #2216 (Kenoia): a key held to models or accounts on a shared uni-switch was
// told no quota at all (#1981), so the remote uni-switch it was set up on
// showed none of the providers it may use. A key whose owner turns its
// quotas on is told the cards of the providers whose every model it may
// use, of the accounts and keys it may use, and nothing else: not another
// provider's, not a provider's it may use only some models of, not an
// account's it may not use.
func TestHeldKeyToldItsOwnQuotas(t *testing.T) {
	fresh(t)
	t.Setenv("UNI_SWITCH_ADDR", "")
	seedKeptBalances(t)

	ref := func(id, user string) string {
		t.Helper()
		p, err := provider.Find(id)
		if err != nil {
			t.Fatal(err)
		}
		for _, a := range p.AccountIDs() {
			if a.User == user {
				return id + "/" + a.ID
			}
		}
		t.Fatalf("%s has no key %q", id, user)
		return ""
	}
	keys, secrets := newCaller(t, "Owner", "AlphaAll", "AlphaSome", "AlphaTwo", "BetaKey", "Budget", "Nothing")
	set := func(action string, c access.Change) {
		t.Helper()
		if _, err := access.Update(action, c); err != nil {
			t.Fatal(action, err)
		}
	}
	set("models-key", access.Change{Key: keys[1].ID, Models: []string{"alpha/*"}})
	set("models-key", access.Change{Key: keys[2].ID, Models: []string{"alpha/m1", "beta/*"}})
	set("models-key", access.Change{Key: keys[3].ID, Models: []string{"alpha/*"}})
	set("accounts-key", access.Change{Key: keys[3].ID, Accounts: []string{ref("alpha", "Two")}})
	set("accounts-key", access.Change{Key: keys[4].ID, Accounts: []string{ref("beta", provider.Mask("beta-key"))}})
	set("limit-key", access.Change{Key: keys[5].ID, Limit: &access.Limit{Period: "day", Tokens: 1_000_000}})
	set("models-key", access.Change{Key: keys[6].ID, Models: []string{"alpha/m2"}})
	for _, k := range append(keys[1:5:5], keys[6]) {
		set("quotas-key", access.Change{Key: k.ID, Quotas: true})
	}

	h := localGuard(New().Handler())
	do := func(method, path, secret string) (int, string) {
		r := httptest.NewRequest(method, path, nil)
		r.RemoteAddr = "192.168.1.9:5000"
		r.Header.Set("Authorization", "Bearer "+secret)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w.Code, w.Body.String()
	}
	told := func(path, b string) []string {
		t.Helper()
		var out struct {
			Data []struct{ Provider, User string }
		}
		if err := json.Unmarshal([]byte(b), &out); err != nil {
			t.Fatal(path, err, b)
		}
		var cards []string
		for _, q := range out.Data {
			if q.Provider == "alpha" || q.Provider == "beta" || q.Provider == "" {
				cards = append(cards, q.Provider+":"+strings.ToLower(q.User))
				continue
			}
			// another provider's card (an account another test signed in)
			cards = append(cards, q.Provider+":?")
		}
		slices.Sort(cards)
		return cards
	}
	every := []string{"alpha:one", "alpha:two", "beta:"}
	want := map[string][]string{
		"AlphaAll":  {"alpha:one", "alpha:two"},
		"AlphaSome": {"beta:"}, // none of alpha's: m2 spends its quota too
		"AlphaTwo":  {"alpha:two"},
		// beta's key alone and no model: beta's models alone are the
		// key's (Linka on Discord), so alpha's cards are not its
		"BetaKey": {"beta:"},
	}
	for _, path := range []string{"/v1/uni-switch/quotas?kept=1"} {
		for i, s := range secrets {
			name := keys[i].Name
			c, b := do("GET", path, s)
			switch name {
			case "Owner":
				if c != 200 {
					t.Fatalf("%s %s: %d %s", name, path, c, b)
				}
				got := told(path, b)
				for _, w := range every {
					if !slices.Contains(got, w) {
						t.Fatalf("%s %s: no %s card in %v", name, path, w, got)
					}
				}
			case "Nothing":
				// told no card here: the other uni-switch shows its own limit,
				// not "nothing read on that uni-switch yet"
				if c != 200 || len(told(path, b)) != 0 {
					t.Errorf("%s %s: %d %s", name, path, c, b)
				}
			case "Budget":
				if c != http.StatusForbidden || !strings.Contains(b, "gateway_key_held") {
					t.Errorf("%s %s, quotas off: %d %s", name, path, c, b)
				}
			default:
				if c != 200 {
					t.Fatalf("%s %s: %d %s", name, path, c, b)
				}
				if got := told(path, b); !slices.Equal(got, want[name]) {
					t.Errorf("%s %s: told %v, want %v", name, path, got, want[name])
				}
			}
		}
	}
	// the history and the report read afresh go by the same cards
	for _, path := range []string{"/v1/uni-switch/quotas", "/v1/uni-switch/quotas/history"} {
		for i, s := range secrets[1:5] {
			name := keys[i+1].Name
			c, b := do("GET", path, s)
			if c != 200 {
				t.Fatalf("%s %s: %d %s", name, path, c, b)
			}
			for _, card := range told(path, b) {
				if !slices.Contains(want[name], card) {
					t.Errorf("%s %s: told %s, may be told only %v", name, path, card, want[name])
				}
			}
		}
	}
}

// seedKeptBalances saves provider alpha with two keys, One and Two (models
// m1 and m2), and beta with one key (o1), each key's balance kept on disk
// (quotas.json) as last read.
func seedKeptBalances(t *testing.T) {
	t.Helper()
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(up.Close)
	if err := provider.Save(provider.Provider{ID: "alpha", Name: "Alpha", Key: "alpha-one", KeyName: "One",
		Keys: []provider.KeyAccount{{Name: "Two", Key: "alpha-two"}}, Chat: up.URL + "/v1", Models: []string{"m1", "m2"}}); err != nil {
		t.Fatal(err)
	}
	if err := provider.Save(provider.Provider{ID: "beta", Name: "Beta", Key: "beta-key", Chat: up.URL + "/v1", Models: []string{"o1"}}); err != nil {
		t.Fatal(err)
	}
	// each key's balance as last read, kept on disk (quotas.json)
	tag := func(key string) string {
		sum := sha256.Sum256([]byte(key))
		return "balance " + hex.EncodeToString(sum[:8])
	}
	at := time.Now().Add(-time.Minute)
	kept := map[string]any{}
	for _, k := range []struct{ id, key, user string }{{"alpha", "alpha-one", "One"}, {"alpha", "alpha-two", "Two"}, {"beta", "beta-key", ""}} {
		kept[k.id+"/"+tag(k.key)] = map[string]any{"at": at, "quota": provider.SubscriptionQuota{Provider: k.id, User: k.user, Balance: "$5.00", Windows: []provider.QuotaWindow{}}}
	}
	if err := os.WriteFile(filepath.Join(filepath.Dir(provider.Path()), "quotas.json"), mustJSON(kept), 0o600); err != nil {
		t.Fatal(err)
	}
	provider.ForgetKeptCardsForTest()
	provider.ForgetBalances()
	provider.ForgetPlanQuotas()

}

// #2216 from the other side: a uni-switch with that uni-switch as its provider,
// through a key let see its quotas, shows the cards of what the key may
// use beside the key's own; a key told none is answered as held, so it
// shows its own card and the remote's saying why (yi_time), not "nothing
// read on that uni-switch yet", which a refresh wouldn't change.

// the key's own

// #2333: a window's whole is reckoned from every call routed through the
// account, so a held key told the card isn't told it, while this machine's
// owner (no scope) is; the caller's windows are left as they are.
func TestHeldKeyToldNoWindowHolds(t *testing.T) {
	qs := []provider.Quota{{Provider: "alpha", User: "a@example.com", Windows: []provider.QuotaSpan{
		{Name: "5 hours", Used: 40, Holds: &provider.WindowHolds{Tokens: 900000, Priced: true}},
		{Name: "7 days", Used: 2, HoldsWhy: provider.HoldsLow},
	}}}
	if got := (*quotaScope)(nil).quotas(qs); got[0].Windows[0].Holds == nil || got[0].Windows[1].HoldsWhy == "" {
		t.Fatalf("the owner wasn't told the whole: %+v", got[0].Windows)
	}
	s := &quotaScope{every: map[string]bool{"alpha": true}, refs: map[string][]provider.AccountRef{}}
	got := s.quotas(qs)
	if len(got) != 1 {
		t.Fatalf("the key wasn't told its card: %+v", got)
	}
	for _, w := range got[0].Windows {
		if w.Holds != nil || w.HoldsWhy != "" {
			t.Errorf("a held key was told %s's whole: %+v", w.Name, w)
		}
	}
	if qs[0].Windows[0].Holds == nil || qs[0].Windows[1].HoldsWhy == "" {
		t.Errorf("the caller's windows were written to: %+v", qs[0].Windows)
	}
}

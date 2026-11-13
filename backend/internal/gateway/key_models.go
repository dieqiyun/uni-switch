package gateway

import (
	"context"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"strings"

	"github.com/dieqiyun/uni-switch/backend/internal/access"
	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// A gateway key may be held to some models (#882, access.Key.Models): it
// is shown only those in the model lists, and a request of it for another
// is refused before any provider is asked. A model is held to by its
// provider's id and its own, "<provider>/<model>", whatever the agent
// called it. A routing group passes when the key names it, "group/<id>"
// (or "group/*", every group), a group uni-switch found by its auto/<model>
// too, either id passing for the other (allows), and then every member of
// it does, through it. A group of the user's own the key doesn't name is
// never the key's, however few models it has and all of them the key's
// (kautism on Discord: a key given gemini 3.8 was shown, and could use, a
// group the user made of gemini 3.8 alone, a group never handed out); a
// group uni-switch found, one model several providers serve, passes when
// every model in it does, as it is that model.

// uniSwitchChoseKey marks, in a request's context, a call uni-switch makes for a
// model the user picked in its settings rather than the caller (a web
// search's, an image description's, a Codex title's): the key's models
// don't hold it.
type uniSwitchChoseKey struct{}

func uniSwitchChose(ctx context.Context) context.Context {
	return context.WithValue(ctx, uniSwitchChoseKey{}, true)
}

// keyHolds is the calling key's models, when they hold this request: the
// models it lists, and those a limit of some models it has spent counts
// (access.Identity.Spent, zola_xynb on X), which hold uni-switch's own calls
// for the key too, as their spend counts in it.
func keyHolds(r *http.Request) (access.Identity, bool) {
	who := access.Caller(r.Context())
	if chose, _ := r.Context().Value(uniSwitchChoseKey{}).(bool); chose {
		if len(who.Spent) == 0 {
			return who, false
		}
		who.Models, who.AccountModels = nil, false
		return who, true
	}
	return who, who.Restricted() || len(who.Spent) > 0
}

// keyListHolds is keyHolds for a list of models shown to the key: a
// spent limit leaves its models listed, as a spent limit of the whole key
// does, and a request for one is told why it is refused.
func keyListHolds(r *http.Request) (access.Identity, bool) {
	who := access.Caller(r.Context())
	who.Spent = nil
	if !who.Restricted() {
		return who, false
	}
	if chose, _ := r.Context().Value(uniSwitchChoseKey{}).(bool); chose {
		return who, false
	}
	return who, true
}

// spentFor is the key's spent limit that counts provider p's model, by
// any id the model goes by, the one that resets last when several do.
func spentFor(who access.Identity, p provider.Provider, model string) (access.SpentScope, bool) {
	if len(who.Spent) == 0 {
		return access.SpentScope{}, false
	}
	ids := []string{p.ID + "/" + model}
	if own := p.OwnModelID(model); own != model {
		ids = append(ids, p.ID+"/"+own)
	}
	if local := p.ListedModelID(model); local != model {
		ids = append(ids, p.ID+"/"+local)
	}
	var out access.SpentScope
	found := false
	for _, id := range ids {
		if s, ok := who.SpentOn(id); ok && (!found || s.Reset.After(out.Reset)) {
			out, found = s, true
		}
	}
	return out, found
}

// spentRef is the spent limit a request for ref is refused by: a model's
// that counts it, or for a routing group the key may use, the one of its
// members' that resets last, when each member is spent or not the key's.
func spentRef(who access.Identity, ref string) (access.SpentScope, bool) {
	if len(who.Spent) == 0 {
		return access.SpentScope{}, false
	}

	p, model, ok := provider.Resolve(ref)
	if !ok {
		return access.SpentScope{}, false
	}
	return spentFor(who, p, model)
}

// keyModelRefusal is the status and message a request for ref its key may not
// use is answered with: a 429 saying the limit and when it resets, with
// Retry-After, when a limit of the key's spent on it is why, else
// keyModelError's 403. It names no reset in remoteKeyLimitHeader: a
// uni-switch using this one as a remote rests only the model on it, as the
// key's other models are still served.
func keyModelRefusal(w http.ResponseWriter, who access.Identity, ref string) (int, string) {
	if s, ok := spentRef(who, ref); ok {
		return spentRefusal(w, s)
	}
	return http.StatusForbidden, keyModelError(who, ref)
}

// spentRefusal is keyModelRefusal's 429 for the spent limit s.
func spentRefusal(w http.ResponseWriter, s access.SpentScope) (int, string) {
	w.Header().Set("Retry-After", strconv.Itoa(max(1, int(s.Reset.Sub(limitClock()).Seconds()+0.999))))
	w.Header().Set("X-Should-Retry", "false")
	return http.StatusTooManyRequests, s.Why
}

// spentAmong is the spent limit of the key's that counts one of cs, the
// one that resets last when several do.
func spentAmong(who access.Identity, cs []candidate) (access.SpentScope, bool) {
	var out access.SpentScope
	found := false
	for _, c := range cs {
		if s, ok := spentFor(who, c.p, c.model); ok && (!found || s.Reset.After(out.Reset)) {
			out, found = s, true
		}
	}
	return out, found
}

// accountHolds is the calling key's accounts, when they hold this
// request: uni-switch's own calls for the key — a web search, a picture
// described, a Codex title — are of the models the user picked, so the
// models don't hold them, but their spend lands on an account or key all
// the same, and the accounts hold them too.
func accountHolds(r *http.Request) (access.Identity, bool) {
	who := access.Caller(r.Context())
	return who, len(who.Accounts) > 0
}

// allows is who.Allows, a found group named by either of its ids: a key
// held to auto/<model> (as the model lists give it, any spelling of the
// model) may use group/auto-<slug>, the id the gateway routes it by, and
// one held to group/auto-<slug> or group/* the auto/<model> an agent asks
// for, which the gateway takes back to that id (validateModel). A pattern
// of auto/ ids ("auto/*") holds to the groups' auto/<model>.
func allows(who access.Identity, ids ...string) bool {
	if who.Allows(ids...) {
		return true
	}
	for _, id := range ids {
		if !strings.HasPrefix(id, provider.GroupPrefix+"auto-") {
			continue
		}
		if pub := provider.PublicRef(id); pub != id && who.Allows(pub) {
			return true
		}
		for _, p := range who.Models {
			if !strings.Contains(p, "*") && strings.HasPrefix(p, provider.AutoPrefix) && strings.EqualFold(provider.CanonicalRef(p), id) {
				return true
			}
		}
	}
	return false
}

// modelAllowed says the key may use provider p's model. A model the user lists by an id of their own (provider.SetModelID) is
// the one model by either id, so a key held to one may use the other:
// one written before the id was given, or an agent's old ref to the
// provider's own id.
func modelAllowed(who access.Identity, p provider.Provider, model string) bool {
	if _, spent := spentFor(who, p, model); spent {
		return false // a limit of the key's that counts it is spent
	}
	return modelListed(who, p, model)
}

// modelListed is modelAllowed by the key's models alone.
func modelListed(who access.Identity, p provider.Provider, model string) bool {
	if allows(who, p.ID+"/"+model) {
		return true
	}
	if own := p.OwnModelID(model); own != model {
		return allows(who, p.ID+"/"+own)
	}
	if local := p.ListedModelID(model); local != model {
		return allows(who, p.ID+"/"+local)
	}
	return false
}

// keyList weighs a model list's entries for one request's key (#1762):
// the routing groups are read once for the list, not once an entry —
// each read is every provider's models, its files stat'ed again, and a
// list of 1,000 models with 500 groups took seconds — and each
// provider's accounts and keys once, not once a model. Nothing is kept
// past the request, so the next sees a group, account or key's models
// changed, as before.
type keyList struct {
	who  access.Identity
	refs map[string][]provider.AccountRef
}

func newKeyList(who access.Identity) *keyList {
	return &keyList{who: who, refs: map[string][]provider.AccountRef{}}
}

// entryAllowed says the key is shown a model of the catalog — and some
// account or key it may use serves it (#905): one the provider's
// accounts are set not to serve (#474) is none of the key's.
func (k *keyList) entryAllowed(e provider.Entry) bool {
	who := k.who
	if !who.Restricted() {
		return true
	}

	if !modelAllowed(who, e.Provider, e.Model) {
		return false
	}
	return k.accountServes(e.Provider, e.Model)
}

// accountServes is accountServes with p's accounts and keys read once
// for the list.
func (k *keyList) accountServes(p provider.Provider, model string) bool {
	if len(k.who.Accounts) == 0 {
		return true
	}
	refs, ok := k.refs[p.ID]
	if !ok {
		refs = p.AccountIDs()
		k.refs[p.ID] = refs
	}
	return accountServesOn(k.who, p, model, refs)
}

// accountServes says some account or key the key may use serves the provider's
// model: an account of several set to serve other models only (#474) is
// one the key may use, and none of the key's.
func accountServes(who access.Identity, p provider.Provider, model string) bool {
	if len(who.Accounts) == 0 {
		return true
	}
	return accountServesOn(who, p, model, p.AccountIDs())
}

// accountServesOn is accountServes over refs, p's accounts and keys.
func accountServesOn(who access.Identity, p provider.Provider, model string, refs []provider.AccountRef) bool {
	if len(refs) == 0 {
		// a provider with no account and no key (a local ollama): the
		// list holds a key to some accounts of the providers it names
		// (AllowsAccount's rule), so one it names none of serves as it
		// always did; one it names — an entry kept after its last account
		// or key went — serves none, holding the key closer
		return who.AllowsAccount(p.ID, "")
	}
	for _, r := range refs {
		if !who.AllowsAccount(p.ID, r.ID) {
			continue
		}
		ref := r.User
		if r.Key {
			ref = r.ID // a key's own list is keyed by its fingerprint
		}
		if p.AccountServes(ref, model) {
			return true
		}
	}
	return false
}

// keyAllowed filters the catalog to what the calling key may use.
func keyAllowed(r *http.Request, es []provider.Entry) []provider.Entry {
	who, held := keyListHolds(r)
	if !held {
		return es
	}
	k := newKeyList(who)
	out := make([]provider.Entry, 0, len(es))
	for _, e := range es {
		if k.entryAllowed(e) {
			out = append(out, e)
		}
	}
	return out
}

func keyListed(r *http.Request, shown []provider.Entry, off func(string) bool) []provider.Entry {
	return keyAllowed(r, shown)
}

// allowedCandidates leaves out of a plan the providers' models the key
// may not use — a model's fallbacks are other models — and the accounts
// and keys it may not either, through a named group too: naming a group
// is not naming its accounts. The plan's order keeps step with what is
// left, and an account the key may not use is said on the plan's left,
// held. members are the group's the key may go to through it
// (groupKeeps), nil for a model. held says every candidate was an
// account or key the key may not use.
func allowedCandidates(who access.Identity, cs []candidate, pl planned, members map[string]bool) ([]candidate, planned, bool) {
	out := cs[:0:0]
	order := pl.order[:0:0]
	held := false
	for i, c := range cs {
		if !members[c.p.ID+"/"+c.model] && !modelAllowed(who, c.p, c.model) {
			continue // a model the key may not use: skipped, as before
		}
		if !accountAllowed(who, c) {
			held = true
			if i < len(pl.order) {
				w := pl.order[i]
				w.Held, w.HeldBy = true, who.Group
				pl.left = append(pl.left, w)
			}
			continue
		}
		out = append(out, c)
		if i < len(pl.order) {
			order = append(order, pl.order[i])
		}
	}
	pl.order = order
	// nor is one held for its credits there for a reset to bring back, or
	// one past its cap for a conversation kept strictly on it
	gone := func(c candidate) bool {
		return !members[c.p.ID+"/"+c.model] && !modelAllowed(who, c.p, c.model) || !accountAllowed(who, c)
	}
	pl.held = slices.DeleteFunc(slices.Clone(pl.held), gone)
	pl.capped = slices.DeleteFunc(slices.Clone(pl.capped), gone)
	// nor one whose own list lacks the model, asked once those that list
	// it refused the model (unlistedNext)
	var unlisted []candidate
	var unlistedW []Weighed
	for i, c := range pl.unlisted {
		if !gone(c) {
			unlisted, unlistedW = append(unlisted, c), append(unlistedW, pl.unlistedW[i])
		}
	}
	pl.unlisted, pl.unlistedW = unlisted, unlistedW
	return out, pl, held && len(out) == 0
}

// accountAllowed says the key may use a candidate's account or key: a
// signed-in account by its stable id, kept through renames, a key by its
// fingerprint — "<provider>/<ref>", told apart in lower case, as
// accounts are. A key that lists none uses every account, as keys
// always did.
func accountAllowed(who access.Identity, c candidate) bool {
	if len(who.Accounts) == 0 {
		return true
	}
	return who.AllowsAccount(c.p.ID, c.p.AccountID())
}

// allowedKey is p sent on the first of its keys in use the calling key
// may use (#905), for the paths that send on one key of the provider
// rather than over its candidates — a Grok or Volcengine video, which is
// asked after on the first key again — in place of the first, which may
// be one the key may not: a key held to a later one is sent on it, and
// refused only when it may use none. A provider of an account keeps its
// one account, checked as it is; one with no account and no key (a local
// ollama) has nothing to hold. A key's own list of models (#474) is not
// weighed here; the paths that can go over the keys do (onKeys, a
// drawing's, the decision path's).
func allowedKey(who access.Identity, p provider.Provider, model string) (provider.Provider, bool) {
	if p.Key == "" {
		return p, p.Account == nil || accountAllowed(who, candidate{p: p, model: model})
	}
	for _, k := range p.KeysOn() {
		if q := p.WithKey(k); accountAllowed(who, candidate{p: q, model: model}) {
			return q, true
		}
	}
	return p, false
}

// onKeys is the keys or accounts of p a request for model that isn't a
// chat goes over (a drawing, a video, an embedding), as a chat's go over
// them (#1657): in its routing's order, a key or account whose own list
// of models (#474) leaves the model out never, one the vendor lists the
// model to another only after those that list it, and those the calling
// key may not use (#905) left out. p's fallback models are not among
// them. With none left it gives the status and what the caller is told:
// 403 when every one is set not to serve the model, or is one the calling
// key may not use. A provider with no key and no account (a local ollama)
// is itself.
func (s *Server) onKeys(r *http.Request, p provider.Provider, model, asked string) ([]candidate, int, string) {
	if p.Account == nil && p.Key == "" {
		return []candidate{{p: p, model: model, rest: p.ID}}, 0, ""
	}
	q := p
	q.Fallback = nil
	cs, pl := s.plan(q, model, provider.Chat)
	if len(cs) == 0 && len(pl.left) > 0 && !slices.ContainsFunc(pl.left, func(w Weighed) bool { return !w.Barred }) {
		return nil, http.StatusForbidden, barredError(asked, pl.left)
	}
	if keyWho, held := accountHolds(r); held {
		cs = slices.DeleteFunc(cs, func(c candidate) bool { return !accountAllowed(keyWho, c) })
		if len(cs) == 0 {
			return nil, http.StatusForbidden, keyAccountsError(keyWho, asked)
		}
	}
	if len(cs) == 0 {
		cs = []candidate{{p: p, model: model, rest: p.ID}}
	}
	return cs, 0, ""
}

// keyModelError is what a request for a model its key may not use is told.
func keyModelError(who access.Identity, model string) string {
	return fmt.Sprintf("The gateway key %q may not use %s; it may use %s. Change the key's models in uni-switch's Gateway page, or use a model it has.",
		who.KeyName, model, strings.Join(who.Models, ", "))
}

// keyAccountsError is what a request every candidate of which was an
// account or key its key may not use is told — or its routing group,
// held to some accounts (who.Group).
func keyAccountsError(who access.Identity, model string) string {

	return fmt.Sprintf("The gateway key %q is not allowed to use the accounts behind %s: no account or key behind it is one the key may use; it may use %s. Change the key's accounts in uni-switch's Gateway page, or use an account it has.",
		who.KeyName, model, keyAccountNames(who))
}

// groupsHeldError is what a request is told when every account or key
// left was one a routing group it went through — a group in the group, a
// group a fallback reached — is held away from (Weighed.HeldBy).
func groupsHeldError(model string, left []Weighed) string {
	var by []string
	for _, w := range left {
		if w.HeldBy != "" && !slices.Contains(by, w.HeldBy) {
			by = append(by, w.HeldBy)
		}
	}
	return fmt.Sprintf("Every account or key behind %s is one the routing group %s is held away from: it is held to other accounts. Change the group's accounts in uni-switch's Routing page, or use an account it has.",
		model, strings.Join(by, ", "))
}

// keyAccountNames is the key's accounts as an error message shows them:
// named as the accounts list names them, "<provider>/<user>" where a
// provider has the account now, the id alone otherwise.
func keyAccountNames(who access.Identity) string {
	names := provider.AccountNames()
	accounts := who.ShownAccounts()
	if len(accounts) == 0 {
		return "none"
	}
	shown := make([]string, len(accounts))
	for i, a := range accounts {
		if n, ok := names[a]; ok {
			shown[i] = n
		} else {
			shown[i] = a
		}
	}
	return strings.Join(shown, ", ")
}

// countHeld answers the 403 a gateway key held to some models (#882) gets
// for counting tokens on one it may not use, as it would be refused
// serving it: Anthropic's count_tokens and Gemini's :countTokens, id the
// model as each asks it, resolved — a bare group's name, an auto group's
// stand-in — and a group judged as a request to it is (groupAllowed).
// proto is the API the error is answered on.
func countHeld(w http.ResponseWriter, r *http.Request, proto provider.Protocol, id string) bool {
	keyWho, held := keyHolds(r)
	if !held {
		return false
	}

	p, model, ok := provider.Resolve(id)
	if !ok {
		return false // counted nowhere: a local estimate, as before
	}
	if !modelAllowed(keyWho, p, model) {
		st, msg := keyModelRefusal(w, keyWho, id)
		writeError(w, proto, st, msg)
		return true
	}
	return false
}

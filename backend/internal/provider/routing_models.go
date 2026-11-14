package provider

import (
	"strings"

	"github.com/dieqiyun/uni-switch/backend/internal/catalog"
)

// MergeName is the name the groups uni-switch finds merge a model by, when
// the user said nothing of it: its id as vendors agree on it (sameModel).
func MergeName(model string) string { return sameModel(model) }

// sameModel is a model's name as vendors agree on it: catalog.SameModel,
// which says what it keeps and what it folds.
func sameModel(id string) string { return catalog.SameModel(id) }

func isDigit(c byte) bool { return c >= '0' && c <= '9' }

// FallbackModels are the models p's fallbacks reach, in order: an entry
// naming a model, that model as routing reaches it (ResolveRoute); one
// naming a routing group, the group's members. An entry that reaches
// nothing is left out, as the gateway passes it over, and a model met
// again is listed where it was met first.
func FallbackModels(p Provider) []Member {
	var out []Member
	seen := map[string]bool{}
	add := func(m Member) {
		if k := m.Provider.ID + "/" + m.Model; !seen[k] {
			seen[k] = true
			out = append(out, m)
		}
	}
	for _, id := range p.Fallback {

		if fp, fm, ok := ResolveRoute(id); ok {
			add(Member{ID: id, Path: []string{id}, Provider: fp, Model: fm})
		}
	}
	return out
}

// unpicked reports whether p's model m is one the user unchecked in p's
// model list since a group took it (timonwong, #3911): p has picks and
// its served models (entries, as find has them) lack m. resolveIn takes
// any "provider/model" of a provider that is on, so that a request can
// name a model its list lacks; a group's member is the user's pick of a
// model they now said not to serve, and the Routing page strikes it.
// A provider with no picks still routes a model its list lacks (#2642),
// and a decision model, never in the catalog, is still a decision
// group's.
func unpicked(find entryOf, p Provider, m string) bool {
	return len(p.Models) > 0 && find(p.ID, m) < 0 && !p.DecidesModel(m)
}

// ResolveRoute is Resolve for a model a route names, a routing group's
// member or a provider's fallback, as routing reaches it: not one the
// user unchecked (unpicked). A request naming the model itself goes by
// Resolve, which still takes it.
func ResolveRoute(id string) (Provider, string, bool) {
	entries := providerEntries()
	p, m, ok := resolveIn(entries, strings.TrimSuffix(strings.TrimSpace(id), "[1m]"))

	find := func(pid, model string) int {
		for i := range entries {
			if entries[i].Provider.ID == pid && entries[i].Model == model {
				return i
			}
		}
		return -1
	}
	if !ok || unpicked(find, p, m) {
		return Provider{}, "", false
	}
	return p, m, true
}

// entryOf is where in entries the last entry of provider pid's model is,
// -1 when none is.
type entryOf func(pid, model string) int

// levelsUnknown reports whether nothing uni-switch reads says which reasoning
// levels the model of x takes, or that it takes none: it has none in the
// catalog, no account's list gives it, its vendor and its maker don't list
// it, and models.dev lists no model of its id at all.
func levelsUnknown(x Entry) bool {
	if len(x.Efforts) > 0 || x.Provider.Account != nil && x.Provider.Account.models != nil {
		return false
	}
	if _, ok := catalog.ListedBy(x.Provider.Catalogs(), x.Model); ok {
		return false
	}
	if _, ok := catalog.ListedBy(makerCatalogs(), x.Model); ok {
		return false
	}
	return !catalog.Knows(x.Model)
}

// Quiet reports whether the model is known not to think: uni-switch has a
// word on it — its vendor's or its maker's list, or its provider's own
// account list — and that word says no reasoning. A model nothing speaks
// for isn't quiet: the gateway sends it the effort the agent asked for,
// as #597 leaves it. Nor is one that takes levels — a model that thinks,
// or one the user gave levels of their own, which the gateway fits an
// effort to. A group's member that is quiet is sent no reasoning ask,
// which some vendors turn away with a 400 on a model that can't think,
// but for one the user fixed at an effort (Group's member syntax, #189):
// the gateway leaves that alone, the user's word over uni-switch's.
func (e Entry) Quiet() bool {
	return e.Reasonless || !e.Reasoning && len(e.Efforts) == 0 && !levelsUnknown(e)
}

// ModelTarget is a single provider model used by media calls and fallbacks.
type Member struct {
	ID       string
	Path     []string
	Provider Provider
	Model    string
	Effort   string
	Levels   []string
}

const GroupPrefix = "group/" // Reserved for retired route references.
const (
	AffinitySession = "session"
	AffinityTurn    = "turn"
	AffinityOff     = "off"
	AffinityStrict  = "strict"
)

var Affinities = []string{"", AffinitySession, AffinityTurn, AffinityOff, AffinityStrict}

const (
	PatienceOff     = -1
	PatienceDefault = 60
	PatienceLongest = 240
)

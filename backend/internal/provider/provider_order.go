package provider

import (
	"fmt"
	"slices"
	"strings"
)

// ordered is ps in the order the user put them in (#499): those order
// names first, as it has them, then the rest as they came (the order they
// were added). The order is more than the list's: the agents' pickers list
// the models in it, a bare model id goes to the first provider serving it,
// and a group uni-switch finds of a model more than one provider serves tries
// them in it.
func ordered(ps []Provider, order []string) []Provider {
	if len(order) == 0 {
		return ps
	}
	at := map[string]int{}
	for i, id := range order {
		if _, dup := at[id]; !dup {
			at[id] = i
		}
	}
	here := map[string]bool{}
	for _, p := range ps {
		here[p.ID] = true
	}
	rank := func(p Provider) int {
		i, ok := at[p.ID]
		if !ok {
			i = len(order)
		}
		// the same subscription under the other computer's id, when it
		// isn't a provider of its own here: an order synced in names
		// it there, and sync put this computer's own id after the rest
		// (MirrorOrder), so the earlier of the two is where it was put
		if c := samePlace(p.ID); c != "" && !here[c] {
			if j, ok := at[c]; ok && j < i {
				i = j
			}
		}
		return i
	}
	slices.SortStableFunc(ps, func(a, b Provider) int { return rank(a) - rank(b) })
	return ps
}

// samePlace is the id the same subscription has on another computer: a
// built-in's plugin is id-plugin where the built-in isn't moved onto it
// and id where it is (PluginID), and whether it is moved is each
// computer's own (migrations.json isn't synced). An order synced from
// the other computer names it by the other id, and it keeps its place by
// that one (#1940, Sun1090: WorkBuddy and WorkBuddy AI, put first on one
// computer, weren't on the other). Sync then names it by both, this
// computer's own id last of all (MirrorOrder keeps the ids named here
// alone after the other's), and ordered takes the earlier.
func samePlace(id string) string {
	if base, ok := strings.CutSuffix(id, "-plugin"); ok && slices.Contains(accountIDs, base) {
		return base
	}
	if slices.Contains(accountIDs, id) {
		return id + "-plugin"
	}
	return ""
}

// SetOrder saves the order the providers are listed and tried in, by id.
// Every id must be a provider's, once; the ones left out follow those
// named, as they are now, so a provider added later goes after them all.
func SetOrder(ids []string) error {
	f, err := read()
	if err != nil {
		return err
	}
	all := All()
	seen := map[string]bool{}
	for _, id := range ids {
		if seen[id] {
			return fmt.Errorf("%s is in the order twice", id)
		}
		seen[id] = true
		if _, ok := find(all, id); !ok {
			return fmt.Errorf("no provider %q", id)
		}
	}
	order := slices.Clone(ids)
	for _, p := range all {
		if !seen[p.ID] {
			order = append(order, p.ID)
		}
	}
	f.Order = order
	return store(f)
}

// StoredOrder is the order the user put the providers in, as saved; as
// with Stored, a file that can't be read is an error.
func StoredOrder() ([]string, error) {
	f, err := read()
	return f.Order, err
}

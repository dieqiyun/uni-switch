package cli

import (
	"fmt"
	"slices"

	"strings"
	"unicode"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// Routing groups from the terminal: what the Routing view does, through
// the same provider.SaveGroup, DeleteGroup and ShowGroup.

func parseRouting(v string) (string, error) {
	v = strings.TrimSpace(v)
	if provider.PluginRouting(v) { // a gateway plugin's
		return v, nil
	}
	v = strings.ToLower(v)
	var names []string
	for _, r := range routingNames {
		if v == r.value || v == r.name || slices.Contains(r.also, v) {
			return r.value, nil
		}
		names = append(names, r.name)
	}
	return "", fmt.Errorf("routing %q is not one uni-switch has: %s, or <plugin>/<name> for a gateway plugin's", v, strings.Join(names, ", "))
}

func splitList(v string) []string {
	return strings.FieldsFunc(v, func(r rune) bool { return r == ',' || r == ' ' })
}

// splitIDs splits a list of model ids at commas, each trimmed: an id can
// hold a space ("Opus 5.5" on a relay, #1976), which splitList would cut in
// two. An item with a space in it that whole doesn't take (no model the
// group can have) is split at its spaces, as such a list was before; with
// whole nil, never.
func splitIDs(v string, whole func(string) bool) []string {
	var out []string
	for _, id := range strings.Split(v, ",") {
		if id = strings.TrimSpace(id); id == "" {
			continue
		}
		if whole != nil && strings.ContainsFunc(id, unicode.IsSpace) && !whole(id) {
			out = append(out, strings.Fields(id)...)
			continue
		}
		out = append(out, id)
	}
	return out
}

func parseTokens(v string) (int, error) { return provider.ParseTokens(v) }

var routingNames = []struct {
	value, name string
	also        []string
}{
	{"", "smart", []string{"default", "auto"}},
	{provider.Ordered, "order", []string{"ordered", "in-order"}},
	{provider.Rotate, "rotate", []string{"in-turn", "round-robin"}},
	{provider.LeastUsed, "usage", []string{"least-used"}},
	{provider.Pace, "pace", []string{"weekly-pace"}},
	{provider.Weighted, "weight", []string{"weighted", "by-weight", "weighted-round-robin", "wrr"}},
	{provider.FreeFirst, "free", []string{"free-first", "freest", "no-cost"}},
	{provider.Fastest, "fast", []string{"fastest", "speed", "speed-first"}},
	{provider.Cheapest, "cheap", []string{"cheapest", "cheapest-first", "price", "cost"}},
	{provider.Drain, "drain", []string{"until-used-up", "use-up", "sticky"}},
	{provider.FreeExpiring, "expiring", []string{"free-expiring", "free-expiry", "free-soonest", "soonest-free"}},
}

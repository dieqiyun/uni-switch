package provider

import (
	"fmt"
	"slices"
	"strings"

	"github.com/dieqiyun/uni-switch/backend/internal/catalog"
	"github.com/dieqiyun/uni-switch/backend/internal/settings"
)

// KeyGroup reports a model selection pinned to one API key. Ordinary provider
// selections retain their existing rotation and fallback behavior.
func (p Provider) KeyGroup() bool { return p.routeKey != "" }

// KeyGroupName never puts a secret or its masked value into client model lists.
func (p Provider) KeyGroupName() string {
	for i, k := range p.KeyList() {
		if k.ID == p.routeKey {
			if name := strings.TrimSpace(k.Name); name != "" {
				return name
			}
			return fmt.Sprintf("Key %d", i+1)
		}
	}
	return ""
}

func keyGroupEntries(p Provider, m catalog.Model, s settings.Settings) []Entry {
	if !p.KeyGroups || p.Account != nil || p.Key == "" {
		return []Entry{entryFor(p, m, s)}
	}
	var out []Entry
	serves := p.Serving(m.ID)
	planHas := p.PlanServing(m.ID)
	for _, k := range p.KeysOn() {
		kid := keyID(k.Key)
		if (len(m.Keys) > 0 && !slices.Contains(m.Keys, kid)) || !p.AccountServes(kid, m.ID) || !serves(k) || !planHas(k) || len(p.WithKey(k).Speaks()) == 0 {
			continue
		}
		e := entryFor(p, m, s)
		e.ID = p.ID + "/~key/" + kid + "/" + m.ID
		e.Provider.routeKey = kid
		out = append(out, e)
	}
	return out
}

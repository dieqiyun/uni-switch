package gateway

import (
	"github.com/dieqiyun/uni-switch/backend/internal/catalog"
	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// saveWindowFixture explicitly configures the windows used by adapter tests.
// Production uses DefaultContextWindow unless a user supplies an override;
// these fixtures exercise propagation of the distinct windows they declare.
func saveWindowFixture(p provider.Provider) error {
	if p.Contexts == nil {
		p.Contexts = map[string]int{}
	}
	for _, m := range p.Available() {
		if n := catalog.Window(m.Context); n > 0 && p.Contexts[m.ID] == 0 {
			p.Contexts[m.ID] = n
		}
	}
	for _, id := range p.Models {
		if n := catalog.ContextOf(id); n > 0 && p.Contexts[id] == 0 {
			p.Contexts[id] = n
		}
	}
	return provider.Save(p)
}

func saveWindowCatalog(id, base string, models []catalog.Model) error {
	if err := catalog.SaveLive(id, base, models); err != nil {
		return err
	}
	p, err := provider.Find(id)
	if err != nil {
		return nil
	} // the provider can be saved after its catalog
	return saveWindowFixture(*p)
}

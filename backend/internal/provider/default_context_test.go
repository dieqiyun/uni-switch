package provider

import (
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/catalog"
)

func TestDefaultContextWindowAcrossModelMetadata(t *testing.T) {
	for _, m := range []catalog.Model{
		{ID: "unknown"},
		{ID: "small", Context: 32000},
		{ID: "large", Context: 1000000, MaxContext: 2000000},
	} {
		p := Provider{ID: "relay"}
		if got := p.WindowOf(m); got != 256000 {
			t.Errorf("%s: default = %d, want 256000", m.ID, got)
		}
		if p.ContextOf(m.ID) != 0 || p.Contexts != nil {
			t.Fatal("a default must not be persisted as a user override")
		}
	}
}

func TestDefaultContextWindowKeepsManualOverrides(t *testing.T) {
	p := Provider{Contexts: map[string]int{"*": 128000, "custom": 512000}}
	for id, want := range map[string]int{"ordinary": 128000, "custom": 512000} {
		if got := p.WindowOf(catalog.Model{ID: id}); got != want {
			t.Errorf("%s: window = %d, want %d", id, got, want)
		}
	}
	delete(p.Contexts, "*")
	if got := p.WindowOf(catalog.Model{ID: "ordinary"}); got != 256000 {
		t.Fatalf("cleared override = %d, want default 256000", got)
	}
}

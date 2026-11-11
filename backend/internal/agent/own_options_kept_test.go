package agent

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/catalog"
)

// A provider's options an agent reaches on its own are kept while
// nothing changed: Goose's model field asked them each poll, for the
// picker and again for Drift, each provider's models made into options
// anew (20 MB of forty polls with real accounts). Each caller still has
// a list of its own, and a catalog read since is offered.
func TestOwnOptionsKept(t *testing.T) {
	syncHome(t)
	writeCatalog := func(extra string) {
		var ms []string
		for i := range 200 {
			ms = append(ms, fmt.Sprintf(`"m%d": {"id":"m%d","name":"M%d"}`, i, i, i))
		}
		if extra != "" {
			ms = append(ms, fmt.Sprintf(`"%s": {"id":"%s","name":"%s"}`, extra, extra, extra))
		}
		if err := os.WriteFile(catalog.CachePath(), []byte(`{"openai": {"name":"OpenAI","models": {`+strings.Join(ms, ",")+`}}}`), 0o644); err != nil {
			t.Fatal(err)
		}
		catalog.Reset()
	}
	writeCatalog("")
	values := func() map[string]bool {
		out := map[string]bool{}
		for _, o := range ownOptions("", "", "openai") {
			out[o.Value] = true
		}
		return out
	}
	if got := values(); !got["openai/m7"] || len(got) < 200 {
		t.Fatalf("openai's models not offered: %d", len(got))
	}
	built := testing.AllocsPerRun(5, func() { ownProvider(nil, "openai") })
	kept := testing.AllocsPerRun(20, func() { ownOptions("", "", "openai") })
	if kept*5 > built {
		t.Fatalf("a provider's options already built took %.0f allocations, building them %.0f", kept, built)
	}
	mine := ownOptions("", "", "openai")
	for i := range mine {
		mine[i].Value = "x"
	}
	if got := values(); !got["openai/m7"] {
		t.Fatal("a caller's write reached the options kept")
	}
	writeCatalog("fresh")
	if !values()["openai/fresh"] {
		t.Fatal("a model the catalog lists since is not offered")
	}
}

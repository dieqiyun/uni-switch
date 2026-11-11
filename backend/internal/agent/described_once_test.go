package agent

import (
	"strings"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// Every agent's list asks whether uni-switch describes images to a model that
// can't see them; the gateway answers by ranking every provider's models
// that see, so it is asked once for all the lists built while nothing
// changed, and again once something did.
func TestListsAskVisionOnce(t *testing.T) {
	syncHome(t)
	var asked int
	was := provider.Described
	provider.Described = func() bool { asked++; return true }
	t.Cleanup(func() { provider.Described = was })
	if err := provider.Save(provider.Provider{ID: "oa", Name: "OA", Key: "k", Chat: "http://127.0.0.1:1/v1", Models: []string{"gpt-5.5"}}); err != nil {
		t.Fatal(err)
	}
	agents := []string{"pi", "claude", "codex", "opencode", "droid", "dsh"}
	for _, a := range agents {
		if ms := sharedModels(a); len(ms) == 0 || !ms[len(ms)-1].Images {
			t.Fatalf("%s: %+v, want its models taking images", a, ms)
		}
	}
	if n := asked; n != 1 {
		t.Errorf("%d agents' lists asked Vision %d times, want once", len(agents), n)
	}
	provider.Described = func() bool { asked++; return false }
	if err := provider.Save(provider.Provider{ID: "oa", Name: "OA", Key: "k", Chat: "http://127.0.0.1:1/v1", Models: []string{"gpt-5.5", "gpt-5.4"}}); err != nil {
		t.Fatal(err)
	}
	ms, ids := sharedModels("pi"), ""
	for _, m := range ms {
		ids += " " + m.ID
		if m.Images {
			t.Errorf("after a change: %s takes images, want none to", m.ID)
		}
	}
	if !strings.Contains(ids, "oa/gpt-5.4") {
		t.Errorf("after a change:%s, want oa/gpt-5.4 among them", ids)
	}
	if asked != 2 {
		t.Errorf("Vision asked %d times in all, want twice", asked)
	}
}

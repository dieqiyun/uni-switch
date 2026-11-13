package gateway

import (
	"context"
	"slices"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// A search goes to the search APIs in the order the user arranged them,
// and never to one switched off, whose key stays (#2286, blackrion).
func TestSearchAPIOrderAndOff(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	f := newSearchAPIs(t)
	for _, a := range []provider.SearchAPI{f.api("tavily", "tvly-k"), f.api("exa", "exa-k"), f.api("brave", "brave-k")} {
		if err := provider.SetSearchAPI(a); err != nil {
			t.Fatal(err)
		}
	}
	s := New()
	ask := func() []string {
		t.Helper()
		f.mu.Lock()
		f.asked = nil
		f.mu.Unlock()
		if _, _, err := s.apiSearch(context.Background(), "latest go"); err != nil {
			t.Fatal(err)
		}
		f.mu.Lock()
		defer f.mu.Unlock()
		return slices.Clone(f.asked)
	}
	if got := ask(); !slices.Equal(got, []string{"tavily: latest go"}) {
		t.Fatalf("as added: %v", got)
	}
	if err := provider.ArrangeSearchAPIs([]string{"brave", "tavily"}); err != nil {
		t.Fatal(err)
	}
	if got := ask(); !slices.Equal(got, []string{"brave: latest go"}) {
		t.Fatalf("brave moved first, asked %v", got)
	}
	if err := provider.SwitchSearchAPI("brave", false); err != nil {
		t.Fatal(err)
	}
	if got := ask(); !slices.Equal(got, []string{"tavily: latest go"}) {
		t.Fatalf("brave off, asked %v", got)
	}
	// off and the next one failing: the one after it, never the one off
	f.mu.Lock()
	f.broken = true
	f.mu.Unlock()
	if got := ask(); !slices.Equal(got, []string{"tavily: latest go", "exa: latest go"}) {
		t.Fatalf("tavily down with brave off, asked %v", got)
	}
	if err := provider.SwitchSearchAPI("tavily", false); err != nil {
		t.Fatal(err)
	}
	if err := provider.SwitchSearchAPI("exa", false); err != nil {
		t.Fatal(err)
	}
	if len(provider.SearchAPIs()) != 0 {
		t.Fatal("every API off, and one still to be asked")
	}
	var order []string
	for _, a := range provider.StoredSearchAPIs() {
		order = append(order, a.Vendor)
		if a.Key == "" {
			t.Errorf("%s lost its key", a.Vendor)
		}
	}
	if !slices.Equal(order, []string{"brave", "tavily", "exa"}) {
		t.Fatalf("stored %v", order)
	}
	for _, bad := range [][]string{{"brave", "brave"}, {"google"}} {
		if err := provider.ArrangeSearchAPIs(bad); err == nil {
			t.Errorf("%v arranged", bad)
		}
	}
}

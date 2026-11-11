package library

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// The MCP Registry's search answers in 13–37 s often enough (9 of 33 tries
// over 13 s, 6 over 20 s, the slowest 37.3 s — measured in #3043) that the
// market's 20 s client timed out and the search came back empty, "context
// deadline exceeded", with nothing to retry but the same box. The registry
// is asked with a budget of its own, long enough for its slow tail; the
// other market requests (skills.sh, icons) keep the short one.

// slowRegistry is the registry with a controlled answer delay.
func slowRegistry(t *testing.T, delay time.Duration) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(delay)
		w.Write([]byte(`{"servers": [
			{"server": {"name": "io.github.blwfish/freecad-mcp", "title": "FreeCAD MCP", "remotes": [{"type": "streamable-http", "url": "https://freecad.example/mcp"}]}}
		]}`))
	}))
	t.Cleanup(srv.Close)
	was := registryURL
	registryURL = srv.URL
	t.Cleanup(func() { registryURL = was })
	return srv
}

// TestRegistrySlowTailStillAnswers: a registry that answers past the market
// client's 20 s is searched all the same (#3043). The shared client's
// timeout stands in for the real 20 s here, at a scale a test can wait for.
func TestRegistrySlowTailStillAnswers(t *testing.T) {
	sandbox(t)
	slowRegistry(t, 120*time.Millisecond)
	was := client
	client = &http.Client{Timeout: 50 * time.Millisecond} // the 20 s of the report, shrunk
	t.Cleanup(func() { client = was })

	list, err := MarketServers("freecad")
	if err != nil {
		t.Fatalf("the registry's slow tail emptied the search: %v", err)
	}
	if len(list) == 0 || list[0].ID != "io.github.blwfish/freecad-mcp" {
		t.Fatalf("found: %+v", list)
	}
}

// TestRegistryTimeoutStillSaysSo: past even the registry's own budget the
// search fails with what it is, and what the market has of its own is
// still listed beside it (#1589) — not an empty page.
func TestRegistryTimeoutStillSaysSo(t *testing.T) {
	sandbox(t)
	slowRegistry(t, 200*time.Millisecond)
	was := registryClient
	registryClient = &http.Client{Timeout: 30 * time.Millisecond}
	t.Cleanup(func() { registryClient = was })

	list, err := MarketServers("freecad")
	if err == nil {
		t.Fatalf("a registry past its own budget answered: %+v", list)
	}
	var se *SearchError
	if !(errors.As(err, &se) && se.Source == "the MCP Registry") {
		t.Fatalf("error %v, want a search error naming the registry", err)
	}
}

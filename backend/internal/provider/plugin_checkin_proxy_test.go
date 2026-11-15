package provider

import (
	"net/http"
	"net/http/httptest"

	"slices"
	"sync"
	"testing"
)

// recordingProxy is an HTTP proxy that answers each request itself, as the
// vendor would, and tells what it was asked.
func recordingProxy(t *testing.T) (*httptest.Server, func() []string) {
	t.Helper()
	var mu sync.Mutex
	var seen []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		seen = append(seen, r.URL.String())
		mu.Unlock()
		w.Write([]byte("{}"))
	}))
	t.Cleanup(srv.Close)
	return srv, func() []string {
		mu.Lock()
		defer mu.Unlock()
		return slices.Clone(seen)
	}
}

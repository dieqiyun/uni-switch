package gateway

import (
	"io"
	"net/http"
	"net/http/httptest"

	"sync"
	"testing"
)

// busyVendor answers every request 429, out of quota, and counts them.
func busyVendor(t *testing.T) (string, func() []string) {
	t.Helper()
	var mu sync.Mutex
	var asked []string
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		asked = append(asked, r.Method+" "+r.URL.Path)
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(429)
		io.WriteString(w, `{"error":{"message":"quota exceeded"}}`)
	}))
	t.Cleanup(up.Close)
	return up.URL, func() []string { mu.Lock(); defer mu.Unlock(); return append([]string(nil), asked...) }
}

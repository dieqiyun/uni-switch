package gateway

import (
	"net/http/httptest"
	"testing"
)

// Empryo lists a provider with no key of its own only when its baseURL
// answers a GET (2xx, 401 or 403).
func TestBaseURLAnswers(t *testing.T) {
	for _, p := range []string{"/v1", "/v1/"} {
		rec := httptest.NewRecorder()
		New().Handler().ServeHTTP(rec, httptest.NewRequest("GET", p, nil))
		if rec.Code != 200 {
			t.Fatalf("GET %s: %d %s", p, rec.Code, rec.Body)
		}
	}
}

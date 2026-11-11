package provider

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"

	"testing"
)

// factorySite is a fake Factory API at the root, its EU region under /eu.
func factorySite(t *testing.T, h http.HandlerFunc) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	t.Cleanup(FactoryBaseForTest(srv.URL, srv.URL+"/eu"))
	return srv
}

func factoryJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

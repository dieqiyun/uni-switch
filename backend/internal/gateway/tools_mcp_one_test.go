package gateway

import (
	"net/http"
	"net/http/httptest"
	"strings"
)

func toolsPostTo(h http.Handler, path, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	r.RemoteAddr, r.Host = "127.0.0.1:50000", "127.0.0.1:3425"
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

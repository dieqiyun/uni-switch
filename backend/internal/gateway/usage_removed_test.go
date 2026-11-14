package gateway

import (
	"github.com/dieqiyun/uni-switch/backend/internal/access"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestUsageReportEndpointsRemoved(t *testing.T) {
	fresh(t)
	secret, err := access.Update("add-key", access.Change{Name: "QA local caller"})
	if err != nil {
		t.Fatal(err)
	}
	front := New().Front()
	for _, path := range []string{"/v1/uni-switch/usage", "/v1/uni-switch/usage/summary", "/v1/uni-switch/usage/requests"} {
		for _, method := range []string{"GET", "POST"} {
			r := httptest.NewRequest(method, "http://localhost"+path, strings.NewReader(`{}`))
			r.RemoteAddr = "127.0.0.1:4567"
			r.Header.Set("Authorization", "Bearer "+secret)
			w := httptest.NewRecorder()
			front.ServeHTTP(w, r)
			if w.Code != http.StatusNotFound {
				t.Errorf("%s %s = %d: %s", method, path, w.Code, w.Body)
			}
		}
	}
}

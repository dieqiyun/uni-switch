package gateway

import (
	"net/http/httptest"
	"strings"
	"testing"
)

// bareGLM asks for glm-5.3 by its bare id, as MOMO's client did, and says
// which provider the reply named and what it said. Each say is a
// conversation of its own: one answered stays where it was answered.
func bareGLM(t *testing.T, s *Server, say string) (code int, from, body string) {
	t.Helper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(`{"model":"glm-5.3","messages":[{"role":"user","content":"`+say+`"}]}`))
	s.Handler().ServeHTTP(rec, req)
	return rec.Code, rec.Header().Get(providerHeader), rec.Body.String()
}

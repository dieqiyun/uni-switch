package gateway

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestInvalidRequestParsers(t *testing.T) {
	for _, parse := range []struct {
		name string
		read func([]byte) (*Request, error)
	}{
		{"chat", parseChat}, {"responses", parseResponses},
		{"anthropic", parseAnthropic}, {"gemini", parseGemini},
	} {
		for _, body := range []string{"", " \r\n\t", "null", "[]", `{"model":`, `{"stream":true,"model":`, `{"model":"secret-value"} trailing`, `{"tools":[{"name":"echo"}]} trailing`, `{"generation_config":{}} trailing`, `{"model":42}`} {
			t.Run(parse.name+"/"+body, func(t *testing.T) {
				_, err := parse.read([]byte(body))
				if err == nil || !strings.HasPrefix(err.Error(), "invalid request: ") {
					t.Fatalf("invalid envelope accepted: %v", err)
				}
				if strings.Contains(err.Error(), "secret-value") || strings.Contains(err.Error(), "cannot unmarshal") || strings.Contains(err.Error(), "unexpected end") {
					t.Fatalf("raw parser details escaped: %v", err)
				}
				if strings.TrimSpace(body) == "" && !strings.Contains(err.Error(), "empty") {
					t.Fatalf("empty body needs an actionable message: %v", err)
				}
			})
		}
	}
}

func TestInvalidRequestHTTPShapes(t *testing.T) {
	fresh(t)
	s := New()
	for _, path := range []string{"/v1/chat/completions", "/chat/completions", "/v1/responses", "/responses", "/v1/messages", "/messages", "/v1/messages/count_tokens", "/v1beta/models/qa:generateContent", "/v1beta/models/qa:streamGenerateContent?alt=sse", "/v1beta/models/qa:countTokens"} {
		for _, body := range []string{"", " \n", "null", "[]", `{"model":`, `{"stream":true,"model":`, `{"model":"qa","generation_config":{}} trailing`, `{"model":42}`} {
			// Gemini takes its model from the URL and replaces the body model.
			if strings.Contains(path, "v1beta") && body == `{"model":42}` {
				continue
			}
			t.Run(path+"/"+body, func(t *testing.T) {
				r := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
				r.Header.Set("Authorization", "Bearer "+Token)
				r.Header.Set("Content-Type", "application/json")
				w := httptest.NewRecorder()
				s.Handler().ServeHTTP(w, r)
				var reply struct {
					Error struct {
						Code    any    `json:"code"`
						Type    string `json:"type"`
						Message string `json:"message"`
						Status  string `json:"status"`
					} `json:"error"`
				}
				if w.Code != 400 || !strings.Contains(w.Header().Get("Content-Type"), "application/json") || json.Unmarshal(w.Body.Bytes(), &reply) != nil {
					t.Fatalf("want JSON 400: %d %s", w.Code, w.Body)
				}
				if reply.Error.Type != "invalid_request_error" || !strings.HasPrefix(reply.Error.Message, "invalid request: ") || reply.Error.Code == nil {
					t.Fatalf("missing structured error fields: %s", w.Body)
				}
				if strings.Contains(path, "v1beta") {
					if reply.Error.Code != float64(400) || reply.Error.Status != "INVALID_ARGUMENT" {
						t.Fatalf("Gemini dialect lost: %s", w.Body)
					}
				} else if reply.Error.Code != "invalid_request" {
					t.Fatalf("want stable invalid_request code: %s", w.Body)
				}
			})
		}
	}
}

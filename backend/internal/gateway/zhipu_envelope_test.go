package gateway

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// zhipuResponses plays the GLM Coding Plan's Responses API at /api/v1: a
// key it refuses is answered 200 in the platform's envelope, as
// open.bigmodel.cn answered one on 2026-10-15.
type zhipuResponses struct{}

func (zhipuResponses) RoundTrip(r *http.Request) (*http.Response, error) {
	if r.Body != nil {
		io.Copy(io.Discard, r.Body)
	}
	body := `{"code":1000,"msg":"身份验证失败。","success":false}`
	if r.Header.Get("Authorization") == "Bearer good" {
		body = `{"id":"resp_1","object":"response","status":"completed","model":"glm-5.1","output":[{"type":"message","id":"m1","role":"assistant","content":[{"type":"output_text","text":"pong"}]}],"usage":{"input_tokens":3,"output_tokens":1,"total_tokens":4}}`
	}
	return &http.Response{StatusCode: 200, Status: "200 OK", Header: http.Header{"Content-Type": {"application/json"}},
		Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
}

// Codex on a GLM Coding Plan whose key Zhipu refuses (@xanahopper on X)
// is told the key was refused, 401 with Zhipu's words, not handed the
// envelope as a 200 reply; a key Zhipu takes is answered as before.
func TestZhipuResponsesRefusalIsRefusal(t *testing.T) {
	for _, tc := range []struct{ preset, host string }{
		{"zhipu", "open.bigmodel.cn"},
		{"zai", "api.z.ai"},
	} {
		t.Run(tc.preset, func(t *testing.T) {
			t.Setenv("XDG_CONFIG_HOME", t.TempDir())
			t.Setenv("XDG_CACHE_HOME", t.TempDir())
			for _, id := range []string{"bad", "good"} {
				p, err := provider.FromPreset(tc.preset)
				if err != nil {
					t.Fatal(err)
				}
				p.ID, p.Name, p.Key, p.Models = tc.preset+"-"+id, id, id, []string{"glm-5.1"}
				p.Chat, p.Anthropic, p.Responses = "", "", "https://"+tc.host+"/api/v1"
				if err := provider.Save(p); err != nil {
					t.Fatal(err)
				}
			}
			s := New()
			s.client = &http.Client{Transport: zhipuResponses{}}
			codex := func(id string) *httptest.ResponseRecorder {
				rec := httptest.NewRecorder()
				s.Handler().ServeHTTP(rec, httptest.NewRequest("POST", "/v1/responses",
					strings.NewReader(`{"model":"`+tc.preset+"-"+id+`/glm-5.1","input":"Say pong","stream":false}`)))
				return rec
			}
			if rec := codex("bad"); rec.Code != http.StatusUnauthorized || !strings.Contains(rec.Body.String(), "身份验证失败") {
				t.Errorf("refused key: %d %s", rec.Code, rec.Body)
			}
			if rec := codex("good"); rec.Code != 200 || !strings.Contains(rec.Body.String(), "pong") {
				t.Errorf("a key Zhipu takes: %d %s", rec.Code, rec.Body)
			}
		})
	}
}

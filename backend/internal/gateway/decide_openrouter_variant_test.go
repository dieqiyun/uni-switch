package gateway

import (
	"encoding/json"
	"io"
	"net/http"

	"slices"
	"strings"
	"sync"
)

// openRouterUp stands in for openrouter.ai, in process: /api/v1/systemone
// answers Mercury Decide's way (the wrapper as OpenRouter served it on
// 2026-11-01), and anything else gets the 400 coeo91 saw. Requests for any
// other host go to the test's own servers.
type openRouterUp struct {
	mu    sync.Mutex
	asked []string // path and model of each request to openrouter.ai
	next  http.RoundTripper
}

func (u *openRouterUp) RoundTrip(r *http.Request) (*http.Response, error) {
	if r.URL.Hostname() != "openrouter.ai" {
		return u.next.RoundTrip(r)
	}
	b, _ := io.ReadAll(r.Body)
	var q struct{ Model string }
	json.Unmarshal(b, &q)
	u.mu.Lock()
	u.asked = append(u.asked, r.URL.Path+" "+q.Model)
	u.mu.Unlock()
	code, body := 400, `{"error":{"message":"`+q.Model+` is a decisions model and cannot be used with the chat/completions endpoint. Use the /api/alpha/decisions endpoint instead.","code":400}}`
	if r.URL.Path == "/api/v1/systemone" {
		code, body = 200, `{"model":"inception/mercury-decide-20260930","answers":{"intent":{"type":"choice","choice":"debugging","confidence":0.9}},"usage":{"input_tokens":72,"output_tokens":1,"cost":0.00000144},"id":"gen-dec-1793467694-KCls8tvRNbusIJmB9FhD","provider":"Inception"}`
	}
	return &http.Response{StatusCode: code, Header: http.Header{"Content-Type": {"application/json"}},
		Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
}

func (u *openRouterUp) got() []string {
	u.mu.Lock()
	defer u.mu.Unlock()
	return slices.Clone(u.asked)
}

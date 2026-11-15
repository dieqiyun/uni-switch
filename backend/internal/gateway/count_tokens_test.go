package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/catalog"
	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

func TestCountTokensUpstreamErrors(t *testing.T) {
	cases := []struct {
		name, message, typ, retryAfter string
		status                         int
	}{
		{"authentication", "invalid API key", "authentication_error", "", 401},
		{"permission", "access denied", "permission_error", "", 403},
		{"prompt_too_long", "prompt is too long", "invalid_request_error", "", 400},
		{"unsupported_image", "unsupported image format", "invalid_request_error", "", 400},
		{"unsupported_tool", "count_tokens: unsupported tool type", "invalid_request_error", "", 400},
		{"unsupported_image_in_count", "count_tokens unsupported image format", "invalid_request_error", "", 400},
		{"unsupported_model", "model is not supported", "invalid_request_error", "", 400},
		{"unsupported_api_key", "unsupported API key type", "invalid_request_error", "", 400},
		{"unsupported_method_parameter", "unsupported method parameter in tool schema", "invalid_request_error", "", 400},
		{"unsupported_tool_operation", "tool schema does not support operation foo", "invalid_request_error", "", 400},
		{"unsupported_image_zh", "count_tokens 不支持该图片格式", "invalid_request_error", "", 400},
		{"unsupported_tool_zh", "该接口不支持工具参数", "invalid_request_error", "", 400},
		{"unsupported_model_zh", "模型不支持此请求", "invalid_request_error", "", 400},
		{"prompt_too_long_zh", "输入内容超过模型最大上下文长度", "invalid_request_error", "", 400},
		{"rate_limit", "slow down", "rate_limit_error", "7", 429},
		{"unavailable", "temporarily unavailable", "api_error", "", 503},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("XDG_CONFIG_HOME", t.TempDir())
			t.Setenv("XDG_CACHE_HOME", t.TempDir())
			f := &fake{code: tc.status, ctype: "application/json", reply: `{"error":{"message":"` + tc.message + `"}}`}
			up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if tc.retryAfter != "" {
					w.Header().Set("Retry-After", tc.retryAfter)
				}
				f.ServeHTTP(w, r)
			}))
			t.Cleanup(up.Close)
			p := provider.Provider{ID: "fake", Name: "Fake", Key: "k", Models: []string{"m1"}, Anthropic: up.URL}
			if tc.status != 429 {
				p.Keys = []provider.KeyAccount{{Key: "spare"}} // real errors must not try another key
			}
			if err := provider.Save(p); err != nil {
				t.Fatal(err)
			}
			// Exercise errors through the response wrapper as well as the
			// count endpoint: it must finish the error body for the client.
			rec := httptest.NewRecorder()
			req := httptest.NewRequest("POST", "/v1/messages/count_tokens", strings.NewReader(
				`{"model":"fake/m1","messages":[{"role":"user","content":"13812345678"}]}`))
			New().Handler().ServeHTTP(rec, req)
			var result struct {
				Type  string
				Error struct{ Type, Message string }
			}
			message := "Fake: " + tc.message
			if tc.name == "prompt_too_long_zh" {
				message = "prompt is too long: " + message
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil || rec.Code != tc.status || result.Type != "error" ||
				result.Error.Type != tc.typ || result.Error.Message != message {
				t.Fatalf("upstream error: %d %s (decode: %v)", rec.Code, rec.Body.String(), err)
			}
			if rec.Header().Get("Retry-After") != tc.retryAfter {
				t.Fatalf("retry hint: %v, want %q", rec.Header(), tc.retryAfter)
			}
			if f.calls != 1 || f.path != "/v1/messages/count_tokens" {
				t.Fatalf("upstream: %d calls to %s", f.calls, f.path)
			}
		})
	}
}

func TestCountTokensUnsupported(t *testing.T) {
	for _, tc := range []struct {
		name, reply string
		status      int
	}{
		{"missing", "404 page not found", 404},
		{"method", "method not allowed", 405},
		{"unimplemented", "not implemented", 501},
		{"plain_unsupported", "unsupported", 400},
		{"unsupported", `{"error":{"message":"Unsupported"}}`, 400},
		{"unsupported_endpoint", `{"error":{"message":"Unsupported endpoint: /v1/messages/count_tokens"}}`, 400},
		{"not_supported", `{"error":{"message":"count_tokens is not supported for this model"}}`, 400},
		{"not_implemented", `{"error":{"message":"This endpoint is not implemented"}}`, 400},
		{"token_counting", `{"error":{"message":"This provider does not support token counting"}}`, 400},
		{"endpoint_zh", `{"error":{"message":"该端点不支持 count_tokens"}}`, 400},
		{"token_counting_zh", `{"error":{"message":"不支持 token 计数"}}`, 400},
		{"count_tokens_zh", `{"error":{"message":"count_tokens 接口暂不支持"}}`, 400},
		{"unimplemented_zh", `{"error":{"message":"该接口尚未实现"}}`, 400},
		{"tokens_zh", "暂不支持令牌计数", 400},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := &fake{ctype: "application/json", code: tc.status, reply: tc.reply}
			setup(t, provider.Anthropic, f)
			code, body := post(t, "/v1/messages/count_tokens", `{"model":"fake/m1","messages":[{"role":"user","content":"13812345678"}]}`)
			var got struct {
				Tokens int `json:"input_tokens"`
			}
			if err := json.Unmarshal([]byte(body), &got); err != nil || code != 200 || got.Tokens != 2 {
				t.Fatalf("original estimate: %d %s (decode: %v)", code, body, err)
			}
			if f.calls != 1 || f.path != "/v1/messages/count_tokens" || !strings.Contains(string(f.got), "13812345678") {
				t.Fatalf("upstream: %d calls to %s, body %s", f.calls, f.path, f.got)
			}
		})
	}
}

func TestCountTokensKeyFailover(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		reply  string
	}{
		{"next_key_counts", 200, `{"input_tokens":17}`},
		{"all_limited", 429, `{"error":{"message":"second key limited"}}`},
		{"next_key_unsupported", 404, `{"error":{"message":"not found"}}`},
		{"next_key_bad_prompt", 400, `{"error":{"message":"unsupported image format"}}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("XDG_CONFIG_HOME", t.TempDir())
			t.Setenv("XDG_CACHE_HOME", t.TempDir())
			restingUntil.Lock()
			restingUntil.m = map[string]time.Time{}
			restingUntil.Unlock()
			var seen, bodies []string
			up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				key := r.Header.Get("x-api-key")
				b, _ := io.ReadAll(r.Body)
				seen, bodies = append(seen, key), append(bodies, string(b))
				if r.URL.Path != "/v1/messages/count_tokens" {
					t.Errorf("wrong endpoint: %s", r.URL.Path)
				}
				w.Header().Set("Content-Type", "application/json")
				if key == "k-first" {
					w.Header().Set("Retry-After", "60")
					w.WriteHeader(429)
					io.WriteString(w, `{"error":{"message":"first key limited"}}`)
					return
				}
				if key != "k-next" {
					t.Errorf("used disabled, unlisted or incompatible key: %q", key)
				}
				if tc.status == 429 {
					w.Header().Set("Retry-After", "7")
				}
				w.WriteHeader(tc.status)
				io.WriteString(w, tc.reply)
			}))
			t.Cleanup(up.Close)
			p := provider.Provider{ID: "count", Name: "Count", Key: "k-first", Anthropic: up.URL, Chat: up.URL + "/v1", Models: []string{"m1"},
				Keys:     []provider.KeyAccount{{Key: "k-off", Off: true}, {Key: "k-unlisted"}, {Key: "k-chat", Protocol: provider.Chat}, {Key: "k-next"}},
				Fallback: []string{"count/other-model"}}
			if err := provider.Save(p); err != nil {
				t.Fatal(err)
			}
			// k-unlisted's own list answered, without m1 (a key no list
			// marks is unknown and serves, #1598)
			if err := catalog.SaveLive(p.ID, up.URL, []catalog.Model{{ID: "m1", Keys: []string{provider.KeyID("k-first"), provider.KeyID("k-chat"), provider.KeyID("k-next")}},
				{ID: "m2", Keys: []string{provider.KeyID("k-unlisted")}}}); err != nil {
				t.Fatal(err)
			}
			s := New()
			send := func() *httptest.ResponseRecorder {
				rec := httptest.NewRecorder()
				s.Handler().ServeHTTP(rec, httptest.NewRequest("POST", "/v1/messages/count_tokens", strings.NewReader(
					`{"model":"count/m1","messages":[{"role":"user","content":"13812345678"}]}`)))
				return rec
			}
			rec := send()
			want := tc.status
			if want == 404 {
				want = 200
			}
			if rec.Code != want || strings.Join(seen, ",") != "k-first,k-next" {
				t.Fatalf("reply: %d %s; tried %v", rec.Code, rec.Body, seen)
			}
			if bodies[0] != bodies[1] || modelOf([]byte(bodies[0])) != "m1" || !strings.Contains(bodies[0], "13812345678") {
				t.Fatalf("retried a different prompt: %v", bodies)
			}
			retry := ""
			if tc.status == 429 {
				retry = "7"
			}
			if rec.Header().Get("Retry-After") != retry {
				t.Fatalf("retry hint: %v, want %q", rec.Header(), retry)
			}
			switch tc.status {
			case 200:
				if rec.Body.String() != tc.reply {
					t.Fatalf("count reply: %s", rec.Body)
				}
				seen = nil
				if rec = send(); rec.Code != 200 || strings.Join(seen, ",") != "k-next" {
					t.Fatalf("limited key did not rest: %d %s, tried %v", rec.Code, rec.Body, seen)
				}
			case 404:
				if !strings.Contains(rec.Body.String(), `"input_tokens":2`) {
					t.Fatalf("original estimate: %s", rec.Body)
				}
			default:
				if !strings.Contains(rec.Body.String(), "Count: "+provider.APIError([]byte(tc.reply), "")) {
					t.Fatalf("last error was lost: %s", rec.Body)
				}
			}
		})
	}
}

func TestCountTokensTransportError(t *testing.T) {
	up := setup(t, provider.Anthropic, &fake{})
	up.Close()
	code, body := post(t, "/v1/messages/count_tokens", `{"model":"fake/m1","messages":[{"role":"user","content":"hello"}]}`)
	var result struct {
		Type  string
		Error struct{ Type, Message string }
	}
	if err := json.Unmarshal([]byte(body), &result); err != nil || code != 502 || result.Type != "error" ||
		result.Error.Type != "api_error" || !strings.HasPrefix(result.Error.Message, "Fake: ") {
		t.Fatalf("transport error: %d %s (decode: %v)", code, body, err)
	}
}

type countTransport func(*http.Request) (*http.Response, error)

func (f countTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestCountTokensTransportFailover(t *testing.T) {
	for _, outcome := range []string{"success", "all-failed", "canceled"} {
		t.Run(outcome, func(t *testing.T) {
			t.Setenv("XDG_CONFIG_HOME", t.TempDir())
			t.Setenv("XDG_CACHE_HOME", t.TempDir())
			p := provider.Provider{ID: "transport-" + outcome, Name: "Transport", Key: "first", Anthropic: "http://count.test", Models: []string{"m1"},
				Keys: []provider.KeyAccount{{Key: "off", Off: true}, {Key: "next"}}}
			t.Cleanup(func() {
				for _, k := range p.KeysOn() {
					id := p.ID + "#" + provider.KeyID(k.Key)
					restingUntil.Lock()
					delete(restingUntil.m, id)
					delete(restingUntil.note, id)
					restingUntil.Unlock()
					routed.Lock()
					delete(routed.failures, id)
					routed.Unlock()
				}
			})
			if err := provider.Save(p); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			var seen, bodies []string
			s := New()
			s.client = &http.Client{Transport: countTransport(func(r *http.Request) (*http.Response, error) {
				defer r.Body.Close()
				body, err := io.ReadAll(r.Body)
				if err != nil {
					t.Fatal(err)
				}
				key := r.Header.Get("x-api-key")
				seen, bodies = append(seen, key), append(bodies, string(body))
				if r.URL.Path != "/v1/messages/count_tokens" || modelOf(body) != "m1" || !strings.Contains(string(body), "13812345678") {
					t.Fatalf("upstream: %s %s", r.URL, body)
				}
				if key == "first" {
					if outcome == "canceled" {
						cancel()
						return nil, context.Canceled
					}
					return nil, errors.New("first connection broke")
				}
				if key != "next" {
					t.Fatalf("unexpected key: %s", key)
				}
				if outcome == "all-failed" {
					return nil, errors.New("last connection broke")
				}
				return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}},
					Body: io.NopCloser(strings.NewReader(`{"input_tokens":17}`))}, nil
			})}
			send := func() *httptest.ResponseRecorder {
				rec := httptest.NewRecorder()
				s.Handler().ServeHTTP(rec, httptest.NewRequest("POST", "/v1/messages/count_tokens", strings.NewReader(
					`{"model":"`+p.ID+`/m1","messages":[{"role":"user","content":"13812345678"}]}`)).WithContext(ctx))
				return rec
			}
			rec := send()
			want := "first,next"
			if outcome == "canceled" {
				want = "first"
			}
			if strings.Join(seen, ",") != want {
				t.Fatalf("tried %v, want %s; reply: %d %s", seen, want, rec.Code, rec.Body)
			}
			if len(bodies) == 2 && bodies[0] != bodies[1] {
				t.Fatalf("retry changed prompt: %v", bodies)
			}
			if outcome == "success" {
				if rec.Code != 200 || rec.Body.String() != `{"input_tokens":17}` {
					t.Fatalf("count reply: %d %s", rec.Code, rec.Body)
				}
				seen = nil
				if rec = send(); rec.Code != 200 || strings.Join(seen, ",") != "next" {
					t.Fatalf("failed key did not rest: %d %s, tried %v", rec.Code, rec.Body, seen)
				}
			} else {
				var result struct {
					Error struct{ Type, Message string }
				}
				message := "last connection broke"
				if outcome == "canceled" {
					message = "context canceled"
					if s.resting(p.ID + "#" + provider.KeyID("first")) {
						t.Fatal("client cancellation rested a key")
					}
				}
				if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil || rec.Code != 502 || result.Error.Type != "api_error" || !strings.Contains(result.Error.Message, message) {
					t.Fatalf("transport error: %d %s (decode: %v)", rec.Code, rec.Body, err)
				}
			}
		})
	}
}

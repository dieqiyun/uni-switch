package provider

import (
	"context"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
)

// Zhipu's replies to a key it refuses, byte for byte as open.bigmodel.cn
// answered them on 2026-10-15: 401 at the OpenAI and Anthropic endpoints,
// 200 in the platform's envelope at /api/v1 (the Responses API) and at
// /api/monitor (the plan's quota).
const (
	zhipuChat401      = `{"error":{"code":"1000","message":"身份验证失败。"}}`
	zhipuAnthropic401 = `{"error":{"message":"身份验证失败。","type":"1000"}}`
	zhipuEnvelope1000 = `{"code":1000,"msg":"身份验证失败。","success":false}`
)

// zhipuRefuses plays the GLM Coding Plan's endpoints: "good" is served on
// each, any other key refused as Zhipu refuses it. asked is each path.
type zhipuRefuses struct {
	mu    sync.Mutex
	asked []string
}

func (z *zhipuRefuses) RoundTrip(r *http.Request) (*http.Response, error) {
	if r.Body != nil {
		io.Copy(io.Discard, r.Body)
	}
	z.mu.Lock()
	z.asked = append(z.asked, r.URL.Host+r.URL.Path)
	z.mu.Unlock()
	reply := func(status int, body string) (*http.Response, error) {
		return &http.Response{StatusCode: status, Status: http.StatusText(status), Header: http.Header{"Content-Type": {"application/json"}},
			Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
	}
	key := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	if key == "" {
		key = r.Header.Get("x-api-key")
	}
	good := key == "good"
	switch {
	case strings.HasSuffix(r.URL.Path, "/models"):
		return reply(200, `{"data":[{"id":"glm-5.1"}]}`)
	case r.URL.Path == "/api/v1/responses":
		if !good {
			return reply(200, zhipuEnvelope1000)
		}
		return reply(200, `{"id":"resp_1","object":"response","status":"completed","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"hi"}]}]}`)
	case r.URL.Path == "/api/coding/paas/v4/chat/completions":
		if !good {
			return reply(401, zhipuChat401)
		}
		return reply(200, `{"id":"c","choices":[{"index":0,"message":{"role":"assistant","content":"hi"}}]}`)
	case r.URL.Path == "/api/anthropic/v1/messages":
		if !good {
			return reply(401, zhipuAnthropic401)
		}
		return reply(200, `{"id":"m","type":"message","role":"assistant","content":[{"type":"text","text":"hi"}]}`)
	case strings.HasPrefix(r.URL.Path, "/api/monitor/usage/quota/limit"):
		if !good {
			return reply(200, zhipuEnvelope1000)
		}
		return reply(200, `{"success":true,"data":{"level":"pro","limits":[{"type":"TOKENS_LIMIT","unit":3,"number":5,"percentage":12}]}}`)
	}
	return reply(200, `{"code":500,"msg":"404 NOT_FOUND","success":false}`)
}

// zhipuCodingPlan is an explicitly configured saved Coding Plan endpoint.
func zhipuCodingPlan(t *testing.T, key string) Provider {
	t.Helper()
	p, err := legacyVendor("zhipu")
	if err != nil {
		t.Fatal(err)
	}
	p.Chat, p.Responses = "https://open.bigmodel.cn/api/coding/paas/v4", "https://open.bigmodel.cn/api/v1"
	if p.Responses != "https://open.bigmodel.cn/api/v1" {
		t.Fatalf("coding plan's Responses: %q", p.Responses)
	}
	p.ID, p.Name, p.Key, p.Models = "glm", "GLM", key, []string{"glm-5.1"}
	return p
}

// @xanahopper on X: the editor's Test on a GLM Coding Plan showed OpenAI and
// Anthropic "401 · 令牌已过期或验证不正确" beside a green Responses "25 ms".
// Zhipu's /api/v1 answers a refused key 200 in its envelope; that is the
// refusal it is, on every path that tests an endpoint, and a key Zhipu
// takes is still green there.
func TestZhipuResponsesRefusalNotGreen(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	fake := &zhipuRefuses{}
	was := http.DefaultClient.Transport
	http.DefaultClient.Transport = fake
	t.Cleanup(func() { http.DefaultClient.Transport = was })

	bad := zhipuCodingPlan(t, "expired")
	res := bad.Test(context.Background())
	seen := map[Protocol]bool{}
	for _, r := range res {
		seen[r.Protocol] = true
		if r.OK || r.Status != http.StatusUnauthorized || !strings.Contains(r.Error, "身份验证失败") {
			t.Errorf("refused key on %s: %+v", r.Protocol, r)
		}
	}
	if !seen[Responses] || !seen[Chat] || !seen[Anthropic] {
		t.Fatalf("endpoints tested: %+v", res)
	}
	// a model's own test, and Detect APIs, on the Responses API
	if r := (Provider{ID: "glm-r", Responses: bad.Responses, Key: "expired", Models: []string{"glm-5.1"}}).TestModels(context.Background(), []string{"glm-5.1"}); r[0].OK || r[0].Status != 401 {
		t.Errorf("model test on the Responses API: %+v", r[0])
	}
	if r := probe(context.Background(), bad, Responses, bad.Responses+"/responses", []byte(`{"model":"glm-5.1","input":"hi"}`), "glm-5.1", testWait); r.OK {
		t.Errorf("detect's probe: %+v", r)
	}

	good := zhipuCodingPlan(t, "good")
	for _, r := range good.Test(context.Background()) {
		if !r.OK {
			t.Errorf("a key Zhipu takes on %s: %+v", r.Protocol, r)
		}
	}
}

// What ZhipuEnveloped turns into a refusal, and what it leaves alone.
func TestZhipuEnveloped(t *testing.T) {
	mk := func(host, ct, body string) (*http.Request, *http.Response) {
		req, _ := http.NewRequest("POST", "https://"+host+"/api/v1/responses", nil)
		return req, &http.Response{StatusCode: 200, Status: "200 OK", Header: http.Header{"Content-Type": {ct}}, Body: io.NopCloser(strings.NewReader(body))}
	}
	read := func(res *http.Response) string {
		b, _ := io.ReadAll(res.Body)
		return string(b)
	}
	for _, c := range []struct {
		body   string
		status int
	}{
		{zhipuEnvelope1000, 401},
		{`{"code":1001,"msg":"Header中未收到Authorization参数，无法进行身份验证。","success":false}`, 401},
		{`{"code":1003,"msg":"Authentication Token 已过期，请重新生成/获取","success":false}`, 401},
		{`{"code":1113,"msg":"余额不足","success":false}`, 429},
		{`{"code":1310,"msg":"已达到每周使用上限","success":false}`, 429},
		{`{"code":1211,"msg":"模型不存在","success":false}`, 400},
		{`{"code":1220,"msg":"无权访问","success":false}`, 403},
		{`{"code":500,"msg":"404 NOT_FOUND","success":false}`, 502},
		{`{"code":"1000","msg":"身份验证失败。","success":false}`, 401},
	} {
		for _, host := range []string{"open.bigmodel.cn", "api.z.ai"} {
			req, res := mk(host, "application/json", c.body)
			res = ZhipuEnveloped(req, res)
			if b := read(res); res.StatusCode != c.status || !strings.Contains(b, `"error":{`) || !strings.Contains(b, `"message"`) {
				t.Errorf("%s %s: %d %s", host, c.body, res.StatusCode, b)
			}
		}
	}
	// a reply, another host's envelope, a stream and a long body pass as
	// they came
	long := `{"id":"r","output":[{"text":"` + strings.Repeat("x", 70<<10) + `"}],"success":false}`
	for _, c := range []struct{ host, ct, body string }{
		{"open.bigmodel.cn", "application/json", `{"id":"resp_1","object":"response","output":[]}`},
		{"open.bigmodel.cn", "application/json", `{"success":true,"data":{}}`},
		{"api.example.com", "application/json", zhipuEnvelope1000},
		{"open.bigmodel.cn", "text/event-stream", "data: " + zhipuEnvelope1000 + "\n\n"},
		{"open.bigmodel.cn", "application/json", long},
	} {
		req, res := mk(c.host, c.ct, c.body)
		res = ZhipuEnveloped(req, res)
		if b := read(res); res.StatusCode != 200 || b != c.body {
			t.Errorf("%s %s: changed to %d %.80s", c.host, c.ct, res.StatusCode, b)
		}
	}
}

// The plan's card for a key Zhipu refuses says the key was refused, and
// what to do, with Zhipu's words; it isn't asked again as a team's key.
func TestZhipuRefusedKeyCard(t *testing.T) {
	fake := &zhipuRefuses{}
	was := http.DefaultClient.Transport
	http.DefaultClient.Transport = fake
	t.Cleanup(func() { http.DefaultClient.Transport = was })
	src, ok := planQuotaSourceOf(zhipuCodingPlan(t, "expired"))
	if !ok || !src.sure {
		t.Fatalf("coding plan's quota source: %+v %v", src, ok)
	}
	_, _, _, err := planKeyWindows(context.Background(), Provider{ID: "glm"}, src, "expired")
	if err == nil || !strings.HasPrefix(err.Error(), "Zhipu didn't take this key (身份验证失败。)") || !strings.Contains(err.Error(), "open.bigmodel.cn/usercenter/proj-mgmt/apikeys") {
		t.Errorf("refused key's card: %v", err)
	}
	if len(fake.asked) != 1 {
		t.Errorf("asked %v, want the plan's quota alone", fake.asked)
	}
	zai := planQuotaSource{url: "https://api.z.ai/api/monitor/usage/quota/limit", read: readZhipuPlan, sure: true}
	if _, _, err := planWindows(context.Background(), zai, "expired"); err == nil || !strings.HasPrefix(err.Error(), "Z.ai didn't take this key") {
		t.Errorf("Z.ai's refused key: %v", err)
	}
	if _, ws, err := planWindows(context.Background(), src, "good"); err != nil || len(ws) != 1 {
		t.Errorf("a key Zhipu takes: %v %+v", err, ws)
	}
}

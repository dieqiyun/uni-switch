package provider

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

// The pay-as-you-go OpenAI bases of Zhipu and Z.ai have the Coding Plan's
// beside them; nothing else does, the plan's own base included.
func TestZhipuCodingBase(t *testing.T) {
	for base, want := range map[string]string{
		"https://open.bigmodel.cn/api/paas/v4":         "https://open.bigmodel.cn/api/coding/paas/v4",
		"https://open.bigmodel.cn/api/paas/v4/":        "https://open.bigmodel.cn/api/coding/paas/v4",
		"https://api.z.ai/api/paas/v4":                 "https://api.z.ai/api/coding/paas/v4",
		"https://open.bigmodel.cn/api/coding/paas/v4":  "",
		"https://open.bigmodel.cn/api/anthropic":       "",
		"https://api.z.ai/api/v1":                      "",
		"https://relay.example.com/api/paas/v4":        "",
		"https://open.bigmodel.cn.example/api/paas/v4": "",
	} {
		got, ok := ZhipuCodingBase(base)
		if got != want || ok != (want != "") {
			t.Errorf("%s: %q %v, want %q", base, got, ok, want)
		}
	}
}

// zhipuFake answers as Zhipu's OpenAI APIs do: plan keys at the Coding
// Plan's endpoint, pay-as-you-go keys at /api/paas/v4, anything else 429
// 1113 (its Anthropic endpoint too). asked is each chat completions path
// it was asked.
type zhipuFake struct {
	payg, plan map[string]bool
	asked      []string
}

func (z *zhipuFake) RoundTrip(r *http.Request) (*http.Response, error) {
	if r.Body != nil {
		io.Copy(io.Discard, r.Body)
	}
	if strings.HasSuffix(r.URL.Path, "/models") {
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}},
			Body: io.NopCloser(strings.NewReader(`{"data":[{"id":"glm-5.1"}]}`)), Request: r}, nil
	}
	if strings.HasSuffix(r.URL.Path, "/chat/completions") {
		z.asked = append(z.asked, r.URL.Host+r.URL.Path)
	}
	key := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	served := strings.HasPrefix(r.URL.Path, "/api/paas/v4/") && z.payg[key] || strings.HasPrefix(r.URL.Path, "/api/coding/paas/v4/") && z.plan[key]
	if !served {
		return &http.Response{StatusCode: 429, Status: "429 Too Many Requests", Header: http.Header{"Content-Type": {"application/json"}},
			Body: io.NopCloser(strings.NewReader(`{"error":{"code":"1113","message":"余额不足或无可用资源包,请充值。"}}`)), Request: r}, nil
	}
	return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}},
		Body: io.NopCloser(strings.NewReader(`{"id":"c","choices":[{"index":0,"message":{"role":"assistant","content":"hi"}}]}`)), Request: r}, nil
}

// The editor's endpoint check (@cleverpigo's screenshot: OpenAI "429 ·
// 余额不足或无可用资源包,请充值。" beside a green Anthropic): a GLM Coding
// Plan's key at the pay-as-you-go endpoint is answered at the plan's, and
// a saved provider with that one key moves there. A provider with other
// keys, one the user moved since, a pay-as-you-go key and a key with
// neither stay where they are, the last told what to change.
func TestZhipuCodingPlanKeyTested(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	fake := &zhipuFake{payg: map[string]bool{"payg": true}, plan: map[string]bool{"plan": true, "plan2": true}}
	was := http.DefaultClient.Transport
	http.DefaultClient.Transport = fake
	t.Cleanup(func() { http.DefaultClient.Transport = was })

	openai := func(p Provider) Result {
		t.Helper()
		for _, r := range p.Test(context.Background()) {
			if r.Protocol == Chat {
				return r
			}
		}
		t.Fatalf("%s: no OpenAI result", p.ID)
		return Result{}
	}
	mk := func(id, key string) Provider {
		p, err := legacyVendor("zhipu")
		if err != nil {
			t.Fatal(err)
		}
		p.ID, p.Name, p.Key, p.Models = id, id, key, []string{"glm-5.1"}
		return p
	}
	stored := func(id string) Provider {
		t.Helper()
		p, err := Find(id)
		if err != nil || p == nil {
			t.Fatal(id, err)
		}
		return *p
	}

	// a draft not yet saved: answered, nothing to move
	fake.asked = nil
	if r := openai(mk("draft", "plan")); !r.OK {
		t.Errorf("draft with a plan key: %+v", r)
	}
	if len(fake.asked) != 2 || fake.asked[1] != "open.bigmodel.cn/api/coding/paas/v4/chat/completions" {
		t.Errorf("draft asked %v", fake.asked)
	}

	// saved, the preset at Pay as you go: moved to the Coding Plan
	plan := mk("glm", "plan")
	if err := Save(plan); err != nil {
		t.Fatal(err)
	}
	if r := openai(plan); !r.OK {
		t.Errorf("plan key: %+v", r)
	}
	if p := stored("glm"); p.Chat != "https://open.bigmodel.cn/api/coding/paas/v4" || p.Responses != "https://open.bigmodel.cn/api/v1" {
		t.Errorf("plan provider at chat %s responses %s, want the Coding Plan's", p.Chat, p.Responses)
	}

	// pasted by hand, the Anthropic URL and pay as you go's: the OpenAI URL
	// alone moves, nothing added
	custom := Provider{ID: "mine", Name: "mine", Key: "plan", Models: []string{"glm-5.1"},
		Chat: "https://open.bigmodel.cn/api/paas/v4/", Anthropic: "https://open.bigmodel.cn/api/anthropic"}
	if err := Save(custom); err != nil {
		t.Fatal(err)
	}
	if r := openai(custom); !r.OK {
		t.Errorf("custom plan key: %+v", r)
	}
	if p := stored("mine"); p.Chat != "https://open.bigmodel.cn/api/coding/paas/v4" || p.Responses != "" || p.Anthropic != "https://open.bigmodel.cn/api/anthropic" {
		t.Errorf("custom provider at chat %s responses %s anthropic %s", p.Chat, p.Responses, p.Anthropic)
	}

	// a provider with other keys, which may be pay as you go's: answered,
	// not moved
	multi := mk("multi", "plan")
	multi.Keys = []KeyAccount{{Name: "other", Key: "payg"}}
	if err := Save(multi); err != nil {
		t.Fatal(err)
	}
	if r := openai(stored("multi")); !r.OK {
		t.Errorf("multi plan key: %+v", r)
	}
	if p := stored("multi"); p.Chat != "https://open.bigmodel.cn/api/paas/v4" {
		t.Errorf("provider with other keys moved to %s", p.Chat)
	}

	// a pay-as-you-go key: never sent to the plan's endpoint
	payg := mk("payg", "payg")
	if err := Save(payg); err != nil {
		t.Fatal(err)
	}
	fake.asked = nil
	if r := openai(payg); !r.OK {
		t.Errorf("payg key: %+v", r)
	}
	if len(fake.asked) != 1 || fake.asked[0] != "open.bigmodel.cn/api/paas/v4/chat/completions" {
		t.Errorf("payg key asked %v", fake.asked)
	}
	if p := stored("payg"); p.Chat != "https://open.bigmodel.cn/api/paas/v4" {
		t.Errorf("payg provider moved to %s", p.Chat)
	}

	// neither: refused, saying both URLs and what to change
	broke := mk("broke", "broke")
	if err := Save(broke); err != nil {
		t.Fatal(err)
	}
	r := openai(broke)
	if r.OK || r.Status != 429 {
		t.Fatalf("broke key: %+v", r)
	}
	for _, want := range []string{"余额不足", "https://open.bigmodel.cn/api/paas/v4", "Plan to Coding Plan", "https://open.bigmodel.cn/api/coding/paas/v4", "top up"} {
		if !strings.Contains(r.Error, want) {
			t.Errorf("broke key's error %q doesn't say %q", r.Error, want)
		}
	}
	if p := stored("broke"); p.Chat != "https://open.bigmodel.cn/api/paas/v4" {
		t.Errorf("broke provider moved to %s", p.Chat)
	}

	// moved by the user since the request went out: theirs
	LearnZhipuCoding(Provider{ID: "payg", Key: "other-key"}, "https://open.bigmodel.cn/api/paas/v4")
	if p := stored("payg"); p.Chat != "https://open.bigmodel.cn/api/paas/v4" {
		t.Errorf("provider with another key now moved to %s", p.Chat)
	}
}

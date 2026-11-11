package gateway

import (
	"net/http"
	"testing"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// Vendors' 429s for a limit per minute — tokens or requests — as they
// write them. A body that also says "quota", in its words or in a link,
// read as a plan used up and rested the key a quarter of an hour with
// balance left (monater.114514 on Discord). Those that say a longer
// window, or money spent, still rest as a plan or credit used up.
var minuteLimitBodies = map[string]string{
	// Gemini API (ai.google.dev/gemini-api/docs/rate-limits): a quota per
	// minute, its quotaId ending PerMinute
	`{"error":{"code":429,"message":"You exceeded your current quota, please check your plan and billing details. For more information on this error, head to: https://ai.google.dev/gemini-api/docs/rate-limits.\n* Quota exceeded for metric: generativelanguage.googleapis.com/generate_content_paid_tier_input_token_count, limit: 2000000, model: gemini-2.5-pro\nPlease retry in 33.517s.","status":"RESOURCE_EXHAUSTED","details":[{"@type":"type.googleapis.com/google.rpc.QuotaFailure","violations":[{"quotaMetric":"generativelanguage.googleapis.com/generate_content_paid_tier_input_token_count","quotaId":"GenerateContentPaidTierInputTokensPerModelPerMinute","quotaDimensions":{"location":"global","model":"gemini-2.5-pro"},"quotaValue":"2000000"}]},{"@type":"type.googleapis.com/google.rpc.RetryInfo","retryDelay":"33s"}]}}`: failRate,
	// Vertex AI
	`{"error":{"code":429,"message":"Quota exceeded for aiplatform.googleapis.com/generate_content_requests_per_minute_per_project_per_base_model with base model: gemini-2.5-pro. Please submit a quota increase request. https://cloud.google.com/vertex-ai/docs/generative-ai/quotas-genai.","status":"RESOURCE_EXHAUSTED"}}`: failRate,
	// Azure OpenAI, its link to aka.ms/oai/quotaincrease
	`{"error":{"code":"429","message":"Requests to the ChatCompletions_Create Operation under Azure OpenAI API version 2024-10-21 have exceeded token rate limit of your current AIServices S0 pricing tier. Please go here: https://aka.ms/oai/quotaincrease if you would like to further increase the default rate limit. For Free Account customers, upgrade to Pay as you Go here: https://aka.ms/429TrialUpgrade."}}`: failRate,
	`{"error":{"code":"RateLimitReached","message":"Your requests to gpt-4o for gpt-4o in East US have exceeded the call rate limit for your current AIServices S0 pricing tier. This request was for ChatCompletions_Create under Azure OpenAI API version 2024-10-21. Please retry after 41 seconds. To increase your default rate limit, visit: https://aka.ms/oai/quotaincrease."}}`:                                   failRate,
	// a rate limit pointing to a page about quotas, with nothing of when
	`{"error":{"message":"Rate limit exceeded. See https://example.com/docs/quota","type":"rate_limit_error"}}`: failRate,
	// already rate limits: they stay so
	`{"error":{"message":"Rate limit reached for gpt-4o in organization org-abc on tokens per min (TPM): Limit 30000, Used 28543, Requested 2391. Visit https://platform.openai.com/account/rate-limits to learn more.","type":"tokens","param":null,"code":"rate_limit_exceeded"}}`: failRate,
	`{"type":"error","error":{"type":"rate_limit_error","message":"This request would exceed the rate limit for your organization (abc) of 30,000 input tokens per minute. For details, refer to: https://docs.claude.com/en/api/rate-limits."}}`:                                    failRate,
	`{"error":{"code":"1302","message":"您的账户已达到速率限制，请您控制请求频率"}}`:                                                                                                                                    failRate,
	`{"code":50603,"message":"Request was rejected due to rate limiting. Details: TPM limit reached.","data":null}`:                                                                                 failRate,
	`{"error":{"code":"RateLimitExceeded.EndpointTPMExceeded","message":"The request failed because you have exceeded the TPM limit for the Endpoint. Request id: 0217","type":"TooManyRequests"}}`: failRate,

	// a longer window, or money spent: as before
	`{"error":{"message":"You exceeded your current quota, please check your plan and billing details.","type":"insufficient_quota","param":null,"code":"insufficient_quota"}}`:                                                                                                                        failCredit,
	`{"error":{"code":"1113","message":"余额不足或无可用资源包,请充值。"}}`:                                                                                                                                                                                                                                           failCredit,
	`{"error":{"code":429,"message":"Quota exceeded for metric: generativelanguage.googleapis.com/generate_content_free_tier_requests, limit: 50, model: gemini-2.5-pro","status":"RESOURCE_EXHAUSTED","details":[{"violations":[{"quotaId":"GenerateRequestsPerDayPerProjectPerModel-FreeTier"}]}]}}`: failQuota,
	`{"error":{"message":"Rate limit reached for model x in organization y on tokens per day (TPD): Limit 100000, Used 99000, Requested 3000.","type":"tokens","code":"rate_limit_exceeded"}}`:                                                                                                         failQuota,
	`{"error":{"message":"Rate limit exceeded: free-models-per-day. Add 10 credits to unlock 1000 free model requests per day"}}`:                                                                                                                                                                      failQuota,
	`{"error":{"message":"Resource has been exhausted (e.g. check quota).","status":"RESOURCE_EXHAUSTED"}}`:                                                                                                                                                                                            failQuota,
}

func TestMinuteLimitIsNotQuota(t *testing.T) {
	for body, want := range minuteLimitBodies {
		if got := failure(429, []byte(body)); got != want {
			t.Errorf("%s: %s, want %s", body, got, want)
		}
	}
}

// A key refused for a limit per minute rests as a rate limit: a minute,
// or as long as the vendor's Retry-After or x-ratelimit-reset asks —
// never the quarter of an hour of a quota.
func TestMinuteLimitRestsAMinute(t *testing.T) {
	old := allowances
	defer func() { allowances = old }()
	allowances = func(string) map[string]provider.Allowance { return nil }
	clineRestsCleared(t)
	s := &Server{}
	body := []byte(`{"error":{"code":429,"message":"Quota exceeded for aiplatform.googleapis.com/generate_content_requests_per_minute_per_project_per_base_model with base model: gemini-2.5-pro. Please submit a quota increase request.","status":"RESOURCE_EXHAUSTED"}}`)
	for i, h := range []http.Header{{}, {"Retry-After": {"20"}}, {"X-Ratelimit-Reset-Tokens": {"45s"}}} {
		clineRestsCleared(t)
		p := provider.Provider{ID: "vertex", Chat: "https://aiplatform.example/v1", Key: "k"}
		c := candidate{p: p, model: "gemini-2.5-pro", rest: p.ID}
		r := s.restAfter(c, 429, h, body)
		left := time.Until(r.Until)
		if r.Why != failRate || left > 2*time.Minute || left <= 0 {
			t.Errorf("header %d: rests %v as %s by %s, want a rate limit's minute or its header's wait", i, left, r.Why, r.By)
		}
	}
}

// DashScope words its tokens-per-minute limit as a quota, in OpenAI's
// shape insufficient_quota too (help.aliyun.com/zh/model-studio/
// error-code: Throttling.AllocationQuota / insufficient_quota, "超出
// TPM/TPS限制"): from DashScope it is a rate limit. Its free allowance used
// up stays out of quota, and OpenAI's insufficient_quota out of credit.
func TestDashScopeAllocationQuotaIsRate(t *testing.T) {
	dash := candidate{p: provider.Provider{ID: "bailian", Chat: "https://dashscope.aliyuncs.com/compatible-mode/v1", Key: "k"}, model: "qwen3-max"}
	oai := candidate{p: provider.Provider{ID: "oai", Chat: "https://api.openai.com/v1", Key: "k"}, model: "gpt-5"}
	alloc := `{"error":{"message":"Allocated quota exceeded, please increase your quota limit.","type":"limit_requests","param":null,"code":"limit_requests"},"request_id":"x"}`
	native := `{"code":"Throttling.AllocationQuota","message":"Allocated quota exceeded, please increase your quota limit.","request_id":"x"}`
	insufficient := `{"error":{"message":"You exceeded your current quota, please check your plan and billing details.","type":"insufficient_quota","param":null,"code":"insufficient_quota"}}`
	free := `{"code":"Throttling.AllocationQuota","message":"Free allocated quota exceeded.","request_id":"x"}`
	hour := `{"code":"Throttling.AllocationQuota","message":"hour allocated quota exceeded.","request_id":"x"}`
	for _, tc := range []struct {
		c    candidate
		body string
		want string
	}{
		{dash, alloc, failRate},
		{dash, native, failRate},
		{dash, insufficient, failRate},
		{dash, free, failQuota},
		{dash, hour, failQuota},
		{oai, insufficient, failCredit},
		{oai, alloc, failQuota},
	} {
		if got := failureOf(tc.c, 429, []byte(tc.body)); got != tc.want {
			t.Errorf("%s %s: %s, want %s", tc.c.p.ID, tc.body, got, tc.want)
		}
	}
}

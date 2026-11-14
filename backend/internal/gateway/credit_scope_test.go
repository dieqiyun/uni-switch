package gateway

import (
	"testing"
)

// wbExhausted is WorkBuddy's 429 for credits used up, as the WorkBuddy
// plugin's tests quote it (plugins #25), and as the plugin passes it on
// (explained): insufficient_quota, the hint after the message.
const (
	wbExhausted          = `{"error":{"data":{"code":14018,"msg":"Credits exhausted. Please visit the link below to purchase add-on packs and get more credits: https://www.codebuddy.ai/profile/usage ","requestId":"test-req"}}}`
	wbExhaustedExplained = `{"error":{"message":"Credits exhausted. Please visit the link below to purchase add-on packs and get more credits: https://www.codebuddy.ai/profile/usage  — WorkBuddy requires an active credit package even for 0-rate free models; check your account package balance","type":"insufficient_quota","code":14018}}`
)

// WorkBuddy's "Credits exhausted" is out of credit, as the built-in gets it
// and as the plugin passes it on: read as a rate limit, the built-in's
// account was benched a minute, then longer each time.
func TestCreditsExhaustedIsOutOfCredit(t *testing.T) {
	for _, body := range []string{wbExhausted, wbExhaustedExplained} {
		if got := failure(429, []byte(body)); got != failCredit {
			t.Errorf("%s: %s, want %s", body, got, failCredit)
		}
	}
	// a rate limit that names credits in passing is still one
	if got := failure(429, []byte(`{"error":{"message":"Rate limit reached for requests; credits are not affected"}}`)); got != failRate {
		t.Errorf("rate limit naming credits: %s", got)
	}
}

// OpenAI answers an organization whose prepaid API credits are spent with
// 429 and the code credit_balance_exhausted (hey_boyce on X, 11-06). The
// body here carries OpenAI's documented code alone, not a captured reply:
// it is out of credit, so the next account takes the request, not a rate
// limit that asks the same key again in a minute.
func TestOpenAICreditBalanceExhaustedIsCredit(t *testing.T) {
	for _, body := range []string{
		`{"error":{"code":"credit_balance_exhausted"}}`,
		`{"error":{"code":"credit_balance_exhausted","type":"rate_limit_exceeded"}}`,
	} {
		if got := failure(429, []byte(body)); got != failCredit {
			t.Errorf("%s: %s, want %s", body, got, failCredit)
		}
		if minuteRate([]byte(body)) {
			t.Errorf("%s: read as a minute's rate limit", body)
		}
	}
}

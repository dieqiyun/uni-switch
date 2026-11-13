package gateway

import (
	"strings"
	"testing"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// What monater's OpenAI-format provider answered for gpt-6.1-sol, with
// HTTP 200 (Discord, #general): word for word.
const carriedTPM = "Rate limit reached for gpt-6.1-sol in organization org-xxx on tokens per min (TPM): Limit 500000000, Used 499954050, Requested 101782. Please try again in 6ms. Visit https://platform.openai.com/account/rate-limits to learn more."

var carriedTPMBody = `{"error":{"message":"` + carriedTPM + `","type":"tokens","param":null,"code":"rate_limit_exceeded"}}`

const carriedQuotaBody = `{"error":{"message":"You exceeded your current quota, please check your plan and billing details.","type":"insufficient_quota","param":null,"code":"insufficient_quota"}}`

// An error a reply carried under a 200 fails as the same error with its
// real status does: the minute limit is a rate limit, not a plan used up,
// and insufficient_quota stays out of credit.
func TestCarriedErrorFailsAsItsStatus(t *testing.T) {
	c := candidate{p: provider.Provider{ID: "custom", Chat: "https://relay.example/v1", Key: "k"}, model: "gpt-6.1-sol"}
	for _, body := range []string{carriedTPM, carriedTPMBody, carriedQuotaBody} {
		want := failure(429, []byte(body))
		if got := failure(200, []byte(body)); got != want {
			t.Errorf("failure(200, %s) = %s, want %s as at 429", body, got, want)
		}
		if got := failureOf(c, 200, []byte(body)); got != want {
			t.Errorf("failureOf(200, %s) = %s, want %s as at 429", body, got, want)
		}
	}
	if got := failure(200, []byte(carriedTPM)); got != failRate {
		t.Errorf("the TPM limit at 200 is %s, want %s", got, failRate)
	}
	if got := failure(200, []byte(carriedQuotaBody)); got != failCredit {
		t.Errorf("insufficient_quota at 200 is %s, want %s", got, failCredit)
	}
	// minute limits that name themselves a rate limit fail the same at 200
	for body, want := range minuteLimitBodies {
		if streamStatus(string(wordsOf([]byte(body)))) != 429 {
			continue
		}
		if got := failure(200, []byte(body)); got != want {
			t.Errorf("failure(200, %s) = %s, want %s", body, got, want)
		}
	}
}

// The rest a 200-carried minute limit leaves is a rate limit's, shown
// with the 429 it stands for, not "200 quota used up".
func TestCarriedMinuteLimitRestsAMinute(t *testing.T) {
	old := allowances
	defer func() { allowances = old }()
	allowances = func(string) map[string]provider.Allowance { return nil }
	clineRestsCleared(t)
	s := &Server{}
	p := provider.Provider{ID: "custom", Chat: "https://relay.example/v1", Key: "k"}
	c := candidate{p: p, model: "gpt-6.1-sol", rest: p.ID}
	r := s.restAfter(c, 200, nil, []byte(carriedTPM))
	if left := time.Until(r.Until); r.Why != failRate || r.Status != 429 || left > 2*time.Minute || left <= 0 {
		t.Errorf("rests %v as %s (%d, by %s), want a rate limit's minute at 429", left, r.Why, r.Status, r.By)
	}
}

// errorReply knows a whole 200 that is only an error from a reply.
func TestErrorReply(t *testing.T) {
	for body, want := range map[string]int{
		carriedTPMBody:   429,
		carriedQuotaBody: 402,
		`{"type":"error","error":{"type":"rate_limit_error","message":"Number of request tokens has exceeded your per-minute rate limit"}}`:   429,
		`{"error":"Rate limit reached, please try again later"}`:                                                                              429,
		`{"id":"r1","object":"response","status":"failed","output":[],"error":{"code":"rate_limit_exceeded","message":"` + carriedTPM + `"}}`: 429,
	} {
		status, msg, ok := errorReply([]byte(body))
		if !ok || status != want || msg == "" {
			t.Errorf("errorReply(%s) = %d %q %v, want %d", body, status, msg, ok, want)
		}
	}
	for _, body := range []string{
		chatOK,
		`{"id":"r1","object":"response","status":"completed","output":[{"type":"message"}],"error":null}`,
		`{"error":null,"choices":[{"message":{"content":"hi"}}]}`,
		`{"error":""}`,
		`{"error":{}}`,
		`not json`,
	} {
		if _, _, ok := errorReply([]byte(body)); ok {
			t.Errorf("errorReply(%s) took a reply for an error", body)
		}
	}
}

// A whole 200 that is only an error is booked failed, with its words.
func TestSnifferBooksA200Error(t *testing.T) {
	sn := newSniffer(provider.Chat, "application/json")
	sn.write([]byte(carriedTPMBody))
	sn.settleWhole()
	if !strings.Contains(sn.failed, "Rate limit reached for gpt-6.1-sol") {
		t.Errorf("failed = %q", sn.failed)
	}
	ok := newSniffer(provider.Chat, "application/json")
	ok.write([]byte(chatOK))
	ok.settleWhole()
	if ok.failed != "" {
		t.Errorf("an answer booked failed: %q", ok.failed)
	}
}

func firstTry(t *testing.T, s *Server) Try {
	t.Helper()
	if len(s.trace.routes) == 0 || len(s.trace.routes[len(s.trace.routes)-1].Tries) == 0 {
		t.Fatal("no try traced")
	}
	return s.trace.routes[len(s.trace.routes)-1].Tries[0]
}

// A non-streamed 200 whose body is only monater's TPM error is retried as
// a rate limit, and the agent gets the answer, not the error as one.
func TestCarriedErrorBodyRetriedAsRate(t *testing.T) {
	fresh(t)
	clineRestsCleared(t)
	a := &scripted{replies: []reply{{200, "", carriedTPMBody}, {200, "", chatOK}}}
	scriptedOn(t, "a", provider.Chat, a)
	s := New()
	code, body := postAs(t, s, "", `{"model":"a/m","messages":[{"role":"user","content":"hi"}]}`)
	if code != 200 || !strings.Contains(body, "hello") || a.n != 2 {
		t.Fatalf("%d %s (asked %d times)", code, body, a.n)
	}
	if try := firstTry(t, s); try.Fail != failRate {
		t.Errorf("first try failed as %q, want %q", try.Fail, failRate)
	}
}

// The same on the Responses route.
func TestCarriedErrorBodyRetriedAsRateOnResponses(t *testing.T) {
	fresh(t)
	clineRestsCleared(t)
	failed := `{"id":"r0","object":"response","status":"failed","output":[],"error":{"code":"rate_limit_exceeded","message":"` + carriedTPM + `"}}`
	done := `{"id":"r1","object":"response","status":"completed","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"hello"}]}]}`
	a := &scripted{replies: []reply{{200, "", failed}, {200, "", done}}}
	responsesOn(t, "a", a)
	s := New()
	code, body := sendTo(s, "/v1/responses", `{"model":"a/m","input":"hi"}`)
	if code != 200 || !strings.Contains(body, "hello") || a.n != 2 {
		t.Fatalf("%d %s (asked %d times)", code, body, a.n)
	}
	if try := firstTry(t, s); try.Fail != failRate {
		t.Errorf("first try failed as %q, want %q", try.Fail, failRate)
	}
}

// A 200 stream that broke off with monater's TPM error after some of the
// answer rests the account a rate limit's minute, shown as 429, not as
// "200 quota used up".
func TestCarriedStreamErrorRestsAsRate(t *testing.T) {
	fresh(t)
	clineRestsCleared(t)
	stream := sse(`data: {"id":"c1","object":"chat.completion.chunk","model":"gpt-6.1-sol","choices":[{"index":0,"delta":{"role":"assistant","content":"Hel"}}]}`,
		`data: {"error":{"message":"`+carriedTPM+`","type":"tokens","param":null,"code":"rate_limit_exceeded"}}`)
	a := &scripted{replies: []reply{{200, "text/event-stream", stream}}}
	scriptedOn(t, "a", provider.Chat, a)
	s := New()
	postAs(t, s, "", `{"model":"a/m","stream":true,"messages":[{"role":"user","content":"hi"}]}`)
	try := firstTry(t, s)
	if try.Fail != failRate {
		t.Errorf("failed as %q, want %q", try.Fail, failRate)
	}
	if try.Rest == nil || try.Rest.Why != failRate || try.Rest.Status != 429 || time.Until(try.Rest.Until) > 2*time.Minute {
		t.Errorf("rest %+v, want a rate limit's minute at 429", try.Rest)
	}
}

// An Anthropic client's request translated to a chat provider whose 200
// stream is only the TPM error fails that try as a rate limit, not as a
// vendor's 502.
func TestCarriedStreamErrorTranslatedAsRate(t *testing.T) {
	fresh(t)
	clineRestsCleared(t)
	failed := sse(`data: {"error":{"message":"` + carriedTPM + `","type":"tokens","param":null,"code":"rate_limit_exceeded"}}`)
	done := sse(`data: {"id":"c1","object":"chat.completion.chunk","model":"m","choices":[{"index":0,"delta":{"role":"assistant","content":"hello"}}]}`,
		`data: {"id":"c1","object":"chat.completion.chunk","model":"m","choices":[{"index":0,"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":2,"completion_tokens":1}}`,
		`data: [DONE]`)
	a := &scripted{replies: []reply{{200, "text/event-stream", failed}, {200, "text/event-stream", done}}}
	scriptedOn(t, "a", provider.Chat, a)
	s := New()
	code, body := sendTo(s, "/v1/messages", `{"model":"a/m","max_tokens":16,"messages":[{"role":"user","content":"hi"}]}`)
	if code != 200 || !strings.Contains(body, "hello") {
		t.Fatalf("%d %s (asked %d times)", code, body, a.n)
	}
	if try := firstTry(t, s); try.Fail != failRate {
		t.Errorf("first try failed as %q, want %q", try.Fail, failRate)
	}
}

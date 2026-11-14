package gateway

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

func init() { retryPause = time.Millisecond }

// scripted answers each request with the next of its replies, the last
// one over and over.
type scripted struct {
	replies []reply
	n       int
	hang    chan struct{} // when set, the first request waits on it
	hanging chan struct{} // when set, closed as the first request begins to wait on hang
	limited http.Header   // headers each 429 goes with
}

type reply struct {
	code  int
	ctype string
	body  string
}

func (s *scripted) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	io.ReadAll(r.Body)
	i := min(s.n, len(s.replies)-1)
	s.n++
	if s.hang != nil && i == 0 {
		if s.hanging != nil {
			close(s.hanging)
		}
		<-s.hang
	}
	x := s.replies[i]
	if x.ctype == "" {
		x.ctype = "application/json"
	}
	w.Header().Set("Content-Type", x.ctype)
	if x.code == http.StatusTooManyRequests {
		for k, vs := range s.limited {
			w.Header()[k] = vs
		}
	}
	if x.code != 0 {
		w.WriteHeader(x.code)
	}
	io.WriteString(w, x.body)
}

const chatOK = `{"id":"ok","choices":[{"index":0,"message":{"role":"assistant","content":"hello"},"finish_reason":"stop"}]}`

func scriptedOn(t *testing.T, id string, proto provider.Protocol, s *scripted) {
	t.Helper()
	up := httptest.NewServer(s)
	t.Cleanup(up.Close)
	p := provider.Provider{ID: id, Name: strings.ToUpper(id), Key: "k", Models: []string{"m"}}
	switch proto {
	case provider.Anthropic:
		p.Anthropic = up.URL
	case provider.Responses:
		p.Responses = up.URL
	default:
		p.Chat = up.URL + "/v1"
	}
	if err := provider.Save(p); err != nil {
		t.Fatal(err)
	}
}

// A completed response that carried output and counted its tokens stays
// the whole reply it is: only one that said nothing and counted nothing
// fails (repliesCounted, #2086).
func TestEmptyCompletedLeavesAWholeReplyAlone(t *testing.T) {
	fresh(t)
	whole := sse(`event: response.created`+"\n"+`data: {"type":"response.created","response":{"id":"r","status":"in_progress","output":[]}}`,
		`event: response.output_text.delta`+"\n"+`data: {"type":"response.output_text.delta","delta":"hi there"}`,
		`event: response.completed`+"\n"+`data: {"type":"response.completed","response":{"id":"r","status":"completed","output":[{"type":"message","role":"assistant","id":"m1","content":[{"type":"output_text","text":"hi there"}]}],"usage":{"input_tokens":5,"output_tokens":2,"total_tokens":7}}}`)
	a := &scripted{replies: []reply{{200, "text/event-stream", whole}}}
	scriptedOn(t, "a", provider.Responses, a)
	s := New()
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest("POST", "/v1/responses",
		strings.NewReader(`{"model":"a/m","stream":true,"input":"hi"}`)))
	code, body := rec.Code, rec.Body.String()
	if code != 200 || !strings.Contains(body, "hi there") || strings.Contains(body, "nothing in it") {
		t.Fatalf("%d %s", code, body)
	}
}

// The last one left — or the only one — is tried again after a failure
// that may pass, a moment later; one that won't is the agent's at once.
func TestLastOneLeftIsTriedAgain(t *testing.T) {
	fresh(t)
	a := &scripted{replies: []reply{{503, "", `{"error":{"message":"upstream connect error"}}`}, {502, "", `{"error":{"message":"bad gateway"}}`}, {200, "", chatOK}}}
	scriptedOn(t, "a", provider.Chat, a)
	s := New()
	code, body := postAs(t, s, "", `{"model":"a/m","messages":[{"role":"user","content":"hi"}]}`)
	if code != 200 || !strings.Contains(body, "hello") || a.n != 3 {
		t.Fatalf("%d %s (tried %d times)", code, body, a.n)
	}
	r := s.trace.routes[len(s.trace.routes)-1]
	if len(r.Tries) != 3 || r.Tries[0].Again == 0 || r.Tries[1].Again == 0 || r.Tries[2].Status != 200 {
		t.Fatalf("tries: %+v", r.Tries)
	}

	fresh(t)
	a = &scripted{replies: []reply{{503, "", `{"error":{"message":"down"}}`}}}
	scriptedOn(t, "a", provider.Chat, a)
	// down for good: the same-one retries, then the rounds of its
	// patience (singlePatience; with retryPause at 1ms the rounds'
	// backstop comes first), then the agent has the 503
	if code, _ := postAs(t, New(), "", `{"model":"a/m","messages":[{"role":"user","content":"hi"}]}`); code != 503 || a.n != 1+lastRetries+replanRounds {
		t.Fatalf("gave up with %d after %d tries", code, a.n)
	}

	fresh(t)
	a = &scripted{replies: []reply{{400, "", `{"error":{"message":"messages: field required"}}`}}}
	scriptedOn(t, "a", provider.Chat, a)
	if code, _ := postAs(t, New(), "", `{"model":"a/m","messages":[{"role":"user","content":"hi"}]}`); code != 400 || a.n != 1 {
		t.Fatalf("a request at fault: %d after %d tries", code, a.n)
	}
}

// A relay behind Cloudflare that answers with one of Cloudflare's own
// errors now and then (vdaoren on Discord: an API-key provider's 5xx
// reached the agent) is asked again as for a 502 or 503, before anything
// is sent to the agent: 520 to 524, as Cloudflare words them to an API
// client and to a browser. A sign-in refused or a request the vendor
// turns away is still the agent's after one try.
func TestCloudflareErrorIsTriedAgain(t *testing.T) {
	page := func(code int, what string) string {
		return fmt.Sprintf(`<!DOCTYPE html>
<html class="no-js" lang="en-US"> <head>
<title>relay.example.com | %d: %s</title>
<meta charset="UTF-8" />
</head><body><div id="cf-wrapper"><h1><span class="inline-block">%s</span> <span class="code-label">Error code %d</span></h1>
<span>Cloudflare Ray ID: <strong class="font-semibold">8f1e2d3c4b5a6978</strong></span></div></body></html>`, code, what, what, code)
	}
	for _, x := range []struct {
		name  string
		first reply
	}{
		{"520 plain", reply{520, "text/plain; charset=UTF-8", "error code: 520"}},
		{"521 plain", reply{521, "text/plain; charset=UTF-8", "error code: 521"}},
		{"522 page", reply{522, "text/html; charset=UTF-8", page(522, "Connection timed out")}},
		{"523 plain", reply{523, "text/plain; charset=UTF-8", "error code: 523"}},
		{"524 page", reply{524, "text/html; charset=UTF-8", page(524, "A timeout occurred")}},
	} {
		fresh(t)
		a := &scripted{replies: []reply{x.first, x.first, {200, "", chatOK}}}
		scriptedOn(t, "a", provider.Chat, a)
		s := New()
		code, body := postAs(t, s, "", `{"model":"a/m","messages":[{"role":"user","content":"hi"}]}`)
		if code != 200 || !strings.Contains(body, "hello") || a.n != 3 {
			t.Errorf("%s: %d after %d tries: %s", x.name, code, a.n, body)
			continue
		}
		r := s.trace.routes[len(s.trace.routes)-1]
		if len(r.Tries) != 3 || r.Tries[0].Again == 0 || r.Tries[1].Again == 0 {
			t.Errorf("%s: tries %+v", x.name, r.Tries)
		}
	}

	// a stream: the agent is sent nothing of the failed tries, only the
	// reply that came
	fresh(t)
	good := "data: {\"id\":\"ok\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":\"hello\"},\"finish_reason\":null}]}\n\n" +
		"data: {\"id\":\"ok\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n"
	a := &scripted{replies: []reply{{524, "text/html; charset=UTF-8", page(524, "A timeout occurred")}, {200, "text/event-stream", good}}}
	scriptedOn(t, "a", provider.Chat, a)
	code, body := postAs(t, New(), "", `{"model":"a/m","stream":true,"messages":[{"role":"user","content":"hi"}]}`)
	if code != 200 || !strings.Contains(body, "hello") || strings.Contains(body, "524") || strings.Contains(body, "timeout") || a.n != 2 {
		t.Errorf("stream: %d after %d tries: %s", code, a.n, body)
	}

	// what fails the same however often it is asked is the agent's at once
	for _, x := range []reply{
		{401, "", `{"error":{"message":"Invalid API key","type":"invalid_request_error","code":"invalid_api_key"}}`},
		{400, "", `{"error":{"message":"Your request was rejected as a result of our safety system.","type":"invalid_request_error","code":"content_policy_violation"}}`},
	} {
		fresh(t)
		a := &scripted{replies: []reply{x, {200, "", chatOK}}}
		scriptedOn(t, "a", provider.Chat, a)
		if code, body := postAs(t, New(), "", `{"model":"a/m","messages":[{"role":"user","content":"hi"}]}`); code != x.code || a.n != 1 {
			t.Errorf("%d: %d after %d tries: %s", x.code, code, a.n, body)
		}
	}
}

// A rate limit on the last one left is waited out, a moment, whether the
// vendor says how long or not (#503: a relay's 429 with no Retry-After went
// straight to the agent); one that won't clear in seconds — a plan used up,
// no money left, a Retry-After an hour away — is the agent's at once.
func TestRateLimitIsWaitedOut(t *testing.T) {
	limited := `{"error":{"message":"Rate limit exceeded, please try again later"}}`
	for _, x := range []struct {
		name    string
		said    string
		header  http.Header
		times   int // how many times it is limited first
		code, n int
	}{
		{"retry-after", limited, http.Header{"Retry-After": {"1"}}, 1, 200, 2},
		{"no retry-after", limited, nil, 1, 200, 2},
		{"relay's own words", `{"error":{"message":"当前分组上游负载已饱和，请稍后再试"}}`, nil, 2, 200, 3},
		{"thrice", limited, nil, 3, 200, 4},
		// past the same-one retries, it is waited out within the plain
		// provider's patience (zoecats on Discord), and given up on once
		// that is spent (the rounds' backstop, with retryPause at 1ms)
		{"past the same-one retries", limited, nil, 9, 200, 10},
		{"never clears", limited, nil, 99, 429, 1 + rateRetries + replanRounds},
		{"usage limit", `{"error":{"message":"You've hit your usage limit. Your limit resets in 5 hours."}}`, nil, 9, 429, 1},
		{"no balance", `{"error":{"message":"余额不足"}}`, nil, 9, 429, 1},
		{"an hour away", limited, http.Header{"Retry-After": {"3600"}}, 9, 429, 1},
	} {
		fresh(t)
		a := &scripted{limited: x.header}
		for range x.times {
			a.replies = append(a.replies, reply{429, "", x.said})
		}
		a.replies = append(a.replies, reply{200, "", chatOK})
		scriptedOn(t, "a", provider.Chat, a)
		start := time.Now()
		code, body := postAs(t, New(), "", `{"model":"a/m","messages":[{"role":"user","content":"hi"}]}`)
		if code != x.code || a.n != x.n {
			t.Errorf("%s: %d after %d tries: %s", x.name, code, a.n, body)
		}
		if x.n == 1 && time.Since(start) > time.Second {
			t.Errorf("%s: passed on after %v", x.name, time.Since(start))
		}
	}
}

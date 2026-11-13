package gateway

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// cyberPolicyMsg is what the ChatGPT backend said refusing CartethyiaQwQ's
// Codex turn (#3274), as the reporter quoted it from uni-switch, without
// uni-switch's cut at 300 characters ("bef…").
const cyberPolicyMsg = `This content was flagged for possible cybersecurity risk. If this seems wrong, try rephrasing your request. If you're doing authorized security work that requires more cyber permissive safeguards, apply for Daybreak access via https://platform.openai.com/settings/organization/status-and-access`

// liveRests is every candidate resting now, by its rest key.
func liveRests() []string {
	restingUntil.Lock()
	defer restingUntil.Unlock()
	var out []string
	for k, u := range restingUntil.m {
		if time.Now().Before(u) {
			out = append(out, k+" by "+restingUntil.note[k].By)
		}
	}
	return out
}

// A Codex account's refusal said inside its stream — response.failed or
// the error event, with a code (cyber_policy) and no type, or after some
// of the reply — is told to a client answered whole (stream:false
// Responses, Chat and Messages, each streamed upstream) as a refusal: the
// next account is asked and nobody rests (#3274). Before, the whole path
// answered it 502 api_error: the same account was asked again, and every
// account but the last rested, so two such turns benched a pool.
func TestStreamRefusalAnsweredWholeRestsNobody(t *testing.T) {
	refusals := []struct {
		name   string
		events []string
	}{
		{"response.failed", []string{
			`data: {"type":"response.created","response":{"id":"r0"}}`,
			`data: {"type":"response.failed","response":{"id":"r0","status":"failed","error":{"code":"cyber_policy","message":"` + cyberPolicyMsg + `"}}}`}},
		{"error event", []string{
			`data: {"type":"response.created","response":{"id":"r0"}}`,
			`data: {"type":"error","code":"cyber_policy","message":"` + cyberPolicyMsg + `","param":null}`}},
		{"after some text", []string{
			`data: {"type":"response.created","response":{"id":"r0"}}`,
			`data: {"type":"response.output_text.delta","delta":"Sure, "}`,
			`data: {"type":"response.failed","response":{"id":"r0","status":"failed","error":{"code":"cyber_policy","message":"` + cyberPolicyMsg + `"}}}`}},
	}
	routes := []struct{ name, path, body string }{
		{"responses", "/v1/responses", `{"model":"codex/gpt-5.5","stream":false,"input":"ping"}`},
		{"chat", "/v1/chat/completions", `{"model":"codex/gpt-5.5","messages":[{"role":"user","content":"ping"}]}`},
		{"messages", "/v1/messages", `{"model":"codex/gpt-5.5","max_tokens":64,"messages":[{"role":"user","content":"ping"}]}`},
	}
	for _, x := range refusals {
		refuse := func(w http.ResponseWriter) {
			w.Header().Set("Content-Type", "text/event-stream")
			io.WriteString(w, sse(x.events...))
		}
		for _, rt := range routes {
			ask := func(s *Server) (int, string) {
				rec := httptest.NewRecorder()
				s.Handler().ServeHTTP(rec, httptest.NewRequest("POST", rt.path, strings.NewReader(rt.body)))
				return rec.Code, rec.Body.String()
			}
			t.Run(x.name+"/"+rt.name+"/next answers", func(t *testing.T) {
				codexSignedIn(t, "b@example.com")
				var tried []string
				chatgptRefusing(t, &tried, refuse)
				s := New()
				code, body := ask(s)
				if code != 200 || !strings.Contains(body, "pong") || strings.Join(tried, ",") != "acct-1,acct-2" {
					t.Fatalf("status %d (tried %v): %s", code, tried, body)
				}
				if r := lastRoute(s); len(r.Tries) != 2 || r.Tries[0].Fail != failRefused || r.Tries[0].Rest != nil {
					t.Fatalf("tries: %+v", r.Tries)
				}
				if rs := liveRests(); len(rs) > 0 {
					t.Fatalf("set aside by a refusal: %v", rs)
				}
			})
			t.Run(x.name+"/"+rt.name+"/nobody left", func(t *testing.T) {
				codexSignedIn(t, "b@example.com", "c@example.com")
				var tried []string
				chatgptRefusing(t, &tried, refuse, "acct-2", "acct-3")
				s := New()
				code, body := ask(s)
				if code != 400 || !strings.Contains(body, "refused this request") || !strings.Contains(body, "cyber_policy") ||
					strings.Join(tried, ",") != "acct-1,acct-2,acct-3" {
					t.Fatalf("status %d (tried %v): %s", code, tried, body)
				}
				for _, try := range lastRoute(s).Tries {
					if try.Fail != failRefused || try.Rest != nil {
						t.Fatalf("tries: %+v", lastRoute(s).Tries)
					}
				}
				if rs := liveRests(); len(rs) > 0 {
					t.Fatalf("set aside by a refusal: %v", rs)
				}
			})
		}
	}
}

// restAfter, wherever it is reached, rests nobody for a 400 whose error
// object is the safety filter's (#3274's cyber_policy), counting no
// failure; read from the error's code, a 400 that only echoes the same
// words of the request in its message still rests, as a plain 400 does.
func TestRestAfterPolicyRefusal(t *testing.T) {
	for _, x := range []struct {
		name string
		body string
		rest bool
	}{
		{"cyber_policy", `{"error":{"type":"invalid_request_error","code":"cyber_policy","message":"` + cyberPolicyMsg + `","param":null}}`, false},
		{"plain", `{"error":{"message":"bad request","type":"invalid_request_error"}}`, true},
		// the user's own prompt quoted back in an unrelated 400
		{"words echoed", `{"error":{"type":"invalid_request_error","code":"string_above_max_length","message":"Invalid 'input[0].content': string too long. Got 'safety filter: cyber_policy — apply for Daybreak access …'","param":"input[0].content"}}`, true},
		{"words, no code", `{"error":{"type":"invalid_request_error","code":null,"message":"Unsupported value in 'instructions': safety filter: cyber_policy, Daybreak access"}}`, true},
	} {
		t.Run(x.name, func(t *testing.T) {
			s := &Server{}
			c := candidate{p: provider.Provider{ID: "codex", Account: &provider.Account{Agent: "codex", User: "refusal-" + strings.ReplaceAll(x.name, " ", "-") + "@x.com"}}, model: "gpt-5.5", rest: "codex#" + x.name}
			t.Cleanup(func() {
				clearRest(c.restKey())
				clearRest(c.restID())
				routed.Lock()
				delete(routed.failures, c.restKey())
				delete(routed.failures, c.restID())
				routed.Unlock()
			})
			for i := 0; i < 3; i++ {
				r := s.restAfter(c, 400, http.Header{}, []byte(x.body))
				if !x.rest {
					if r.By != "off" || r.Why != failRefused || r.Until.After(time.Now()) || resting(c) {
						t.Fatalf("refusal #%d rests by %s (%s) until %v", i+1, r.By, r.Why, r.Until)
					}
					continue
				}
				if r.By != "backoff" || time.Until(r.Until) <= 0 {
					t.Fatalf("400 #%d rests by %s until %v; want backoff", i+1, r.By, r.Until)
				}
				break
			}
			if x.rest {
				return
			}
			routed.Lock()
			n, m := routed.failures[c.restKey()], routed.failures[c.restID()]
			routed.Unlock()
			if n != 0 || m != 0 {
				t.Fatalf("refusal counted %d account and %d model failures", n, m)
			}
		})
	}
}

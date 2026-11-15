package gateway

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// factoryPolicyMsg is what the Factory plugin says of Factory's usage
// policy check refusing a request, as sweanng's ZCode showed it from uni-switch
// (Discord #general, 10-29), uni-switch's "Factory: " left off.
const factoryPolicyMsg = `This request was blocked by Factory's usage policy check. (upstream request id 8d1f0c2a3b4e5f60::LAX, code content_policy_rejected) — Factory's usage policy check reads the whole conversation, so this session's requests are refused again until the text that tripped it is gone: start a new session, or remove or reword what the conversation quotes (another agent's system prompt or instructions pasted in a message or a tool's output are the usual cause)`

// factoryPolicy403 is that refusal as the Factory plugin answers it, in
// the shape of the API the request was for: Anthropic's on /llm/a, OpenAI's
// on the others, the code kept on the error.
func factoryPolicy403(anthropic bool) string {
	e := map[string]any{"type": "permission_error", "message": factoryPolicyMsg, "code": "content_policy_rejected"}
	var v any = map[string]any{"error": e}
	if anthropic {
		v = map[string]any{"type": "error", "error": e, "request_id": "8d1f0c2a3b4e5f60::LAX"}
	}
	b, _ := json.Marshal(v)
	return string(b)
}

// Factory's 403 content_policy_rejected is its usage policy check, not the
// account: in Factory's own two shapes and in the plugin's. Its other 403s
// (an org the user can't reach, a model the org's policy leaves out) are
// the account's still.
func TestFactoryPolicyRejectedIsARefusal(t *testing.T) {
	for body, want := range map[string]bool{
		`{"code":"content_policy_rejected","detail":"This request was blocked by Factory's usage policy check."}`:            true,
		`{"error":{"code":"content_policy_rejected","message":"This request was blocked by Factory's usage policy check."}}`: true,
		factoryPolicy403(true):  true,
		factoryPolicy403(false): true,
		`{"detail":"Requested active organization is not accessible by this user"}`:                                  false,
		`{"error":{"type":"permission_error","message":"Model not allowed by your organization's model policy"}}`:    false,
		`{"error":{"type":"permission_error","code":"org_policy_denied","message":"Model not allowed by your org"}}`: false,
	} {
		note, got := policyRefusal([]byte(body))
		if got != want {
			t.Errorf("%s: refusal %v, want %v", body, got, want)
		}
		if got && strings.Contains(body, "start a new session") && !strings.Contains(note, "are the usual cause") {
			t.Errorf("the vendor's advice was cut from the note: %q", note)
		}
	}
}

// sweanng's ZCode, asking Factory through uni-switch, was handed Factory's
// usage policy refusal as the plugin's 403 permission_error, and showed
// "Provider authentication failed" (reason=auth_failed status=403). Each
// agent protocol is now told it as a refusal of the request: 400, the
// protocol's invalid-request error, Factory's words and advice kept, not
// asked again of the same account, and the account not rested.
func TestFactoryPolicyRefusalReachesAgentsAsARefusal(t *testing.T) {
	asks := []struct{ name, path, body, inShape string }{
		{"messages", "/v1/messages", `{"model":"f/m","max_tokens":64,"messages":[{"role":"user","content":"hi"}]}`, `"type":"invalid_request_error"`},
		{"messages stream", "/v1/messages", `{"model":"f/m","max_tokens":64,"stream":true,"messages":[{"role":"user","content":"hi"}]}`, `"type":"invalid_request_error"`},
		{"chat", "/v1/chat/completions", `{"model":"f/m","messages":[{"role":"user","content":"hi"}]}`, `"type":"invalid_request_error"`},
		{"responses", "/v1/responses", `{"model":"f/m","input":"hi"}`, `"type":"invalid_request_error"`},
		{"responses stream", "/v1/responses", `{"model":"f/m","stream":true,"input":"hi"}`, `"type":"invalid_request_error"`},
		{"gemini", "/v1beta/models/f/m:generateContent", `{"contents":[{"role":"user","parts":[{"text":"hi"}]}]}`, `"status":"INVALID_ARGUMENT"`},
	}
	for _, up := range []provider.Protocol{provider.Anthropic, provider.Chat} {
		for _, ask := range asks {
			t.Run(string(up)+"/"+ask.name, func(t *testing.T) {
				fresh(t)
				f := &scripted{replies: []reply{{403, "application/json", factoryPolicy403(up == provider.Anthropic)}}}
				scriptedOn(t, "f", up, f)
				s := New()
				code, out := sendTo(s, ask.path, ask.body)
				if code != http.StatusBadRequest {
					t.Fatalf("status %d, want 400: %s", code, out)
				}
				if !strings.Contains(out, ask.inShape) || strings.Contains(out, "permission_error") || strings.Contains(out, "PERMISSION_DENIED") {
					t.Errorf("not the protocol's request error: %s", out)
				}
				if !strings.Contains(out, "content_policy_rejected") || !strings.Contains(out, "are the usual cause") {
					t.Errorf("Factory's words or advice lost: %s", out)
				}
				if f.n != 1 {
					t.Errorf("Factory asked %d times, want once", f.n)
				}
				r := lastRoute(s)
				if n := len(r.Tries); n == 0 || r.Tries[n-1].Fail != failRefused || r.Tries[n-1].Rest != nil {
					t.Errorf("tries: %+v", r.Tries)
				}
				if rests := liveRests(); len(rests) != 0 {
					t.Errorf("rested for a refusal of the request: %v", rests)
				}
			})
		}
	}
}

// Any route that rests a candidate after a failure leaves it be for
// Factory's 403 refusal, as for a 400 one.
func TestFactoryPolicyRefusalRestsNobody(t *testing.T) {
	fresh(t)
	s := &Server{}
	p := provider.Provider{ID: "f", Name: "Factory", Chat: "https://api.factory.ai/api/llm/o/v1", Key: "k"}
	c := candidate{p: p, model: "glm-5.3", rest: p.ID}
	if r := s.restAfter(c, 403, http.Header{}, []byte(factoryPolicy403(false))); r.Why != failRefused || r.By != "off" {
		t.Errorf("rests as %s by %s, want %s by off", r.Why, r.By, failRefused)
	}
}

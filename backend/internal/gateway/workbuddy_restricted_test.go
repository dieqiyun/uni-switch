package gateway

import (
	"testing"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// wbRestrictedBody is WorkBuddy's 403 for an account it restricted, byte
// for byte as #1849 gives it.
const wbRestrictedBody = `{"code":11140,"msg":"request illegal",
 "displayMsg":{"en":"Your account has been restricted due to unusual activity",
               "zh":"账号存在异常行为，已被限制使用","zh-hant":"帳號存在異常行為，已被限制使用"},
 "displayTips":{"en":"We detected unusual activity on this account. Submit feedback to appeal this restriction",
                "zh":"系统检测到该账号存在异常使用行为，如需申诉请提交反馈"},
 "actions":["EDIT_INPUT","SUBMIT_FEEDBACK"]}`

// restedFor is how long r rests from when it was decided.
func restedFor(r Rest, from time.Time) time.Duration { return r.Until.Sub(from) }

// WorkBuddy's 403 11140 says the account itself is restricted (#1849): in
// each shape the gateway reads it — the vendor's JSON as it came and the
// plugin's — the account rests two hours, as "restricted", not a minute's
// backoff, and the Routing page can lift it.
func TestWorkBuddyRestrictedRestsHours(t *testing.T) {
	fresh(t)
	p := provider.Provider{ID: provider.WorkBuddyAIID, Name: "WorkBuddy AI", Account: &provider.Account{Agent: provider.WorkBuddyAIID, User: "u1"}}
	s := New()

	// the plugin's explained(), as it puts the same answer
	// (the retired opencode-workbuddy-auth 0.1.17)
	plugin := []byte(`{"error":{"message":"request illegal — ` + provider.WBRestrictedHint + `","type":"permission_error","code":11140}}`)

	escaped := []byte(`{"code":"11140","msg":"request illegal","displayMsg":{"zh":"账号存在异常行为，已被限制使用"}}`)
	worded := []byte(`{"msg":"request illegal","displayMsg":{"zh-hant":"帳號存在異常行為，已被限制使用"}}`)
	c := candidate{p: p, model: "deepseek-v4.1-flash", rest: p.ID + "#u1"}
	for name, body := range map[string][]byte{
		"vendor's JSON": []byte(wbRestrictedBody), "plugin's": plugin,
		"code as a string, Chinese escaped": escaped, "words alone": worded,
	} {
		fresh(t)
		now := time.Now()
		rest := s.restAfter(c, 403, nil, body)
		if rest.Why != failRestricted || rest.By != "restricted" {
			t.Fatalf("%s: rests %+v", name, rest)
		}
		if d := restedFor(rest, now); d < restrictedRest-time.Minute || d > restrictedRest+time.Minute {
			t.Fatalf("%s: rests %v", name, d)
		}
		// the account, not one model of it
		if rest.Key != c.restKey() {
			t.Fatalf("%s: rests %q, not the account %q", name, rest.Key, c.restKey())
		}
		// a user whose appeal went through lifts it at once
		if !s.Unrest(rest.Key) {
			t.Fatalf("%s: its rest can't be lifted", name)
		}
		if _, ok := restOf(rest.Key); ok {
			t.Fatalf("%s: still resting once lifted", name)
		}
	}
}

// Another vendor's code 11140, and WorkBuddy's other refusals, aren't an
// account restricted: a key's 403 with code 11140 rests as any failure
// does, and WorkBuddy's 11128 "unapproved channel" (#182) still rests
// nobody, as the agent's prompt is at fault.
func TestOnlyWorkBuddyRestrictedRestsAsRestricted(t *testing.T) {
	fresh(t)
	s := New()
	other := candidate{p: provider.Provider{ID: "acme", Name: "Acme"}, model: "m", rest: "acme"}
	for _, body := range []string{
		`{"code":11140,"msg":"request illegal"}`,
		`{"error":{"code":11140,"message":"forbidden"}}`,
		`{"error":{"message":"Acme: request illegal","type":"permission_error"},"type":"error"}`,
	} {
		if f := failureOf(other, 403, []byte(body)); f == failRestricted {
			t.Fatalf("another vendor's %s read as restricted", body)
		}
		rest := s.restAfter(other, 403, nil, []byte(body))
		if rest.Why != failOther || rest.By == "restricted" || restedFor(rest, time.Now()) > longestRetry {
			t.Fatalf("another vendor's %s rests %+v", body, rest)
		}
	}
	// another plugin's account (fakeco) with WorkBuddy's own body is that
	// vendor's answer too
	if restrictedRefused(provider.Provider{ID: "fakeco", Account: &provider.Account{Agent: "plugin"}}, 403, []byte(wbRestrictedBody)) {
		t.Fatal("a non-WorkBuddy account's 11140 read as restricted")
	}

	wb := provider.Provider{ID: provider.WorkBuddyAIID, Account: &provider.Account{Agent: provider.WorkBuddyAIID, User: "u1"}}
	for _, body := range []string{
		`{"code":11128,"msg":"Illegal API invocation from an unapproved channel"}`,
		`{"code":14018,"msg":"Credits exhausted. Please visit the link to purchase."}`,
		`{"code":111400,"msg":"something else"}`,
	} {
		if restrictedRefused(wb, 403, []byte(body)) || restrictedRefused(wb, 400, []byte(body)) {
			t.Fatalf("WorkBuddy's %s read as restricted", body)
		}
	}
	// 11128 is still the agent's prompt, which rests nobody
	if !promptRefused(400, []byte(`{"code":11128,"msg":"Illegal API invocation from an unapproved channel"}`)) {
		t.Fatal("11128 no longer read as the agent's prompt turned away")
	}
	// an answer that went through is nobody's refusal
	if restrictedRefused(wb, 200, []byte(wbRestrictedBody)) {
		t.Fatal("a 200 read as restricted")
	}
}

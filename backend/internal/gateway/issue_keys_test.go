package gateway

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/access"
	"github.com/dieqiyun/uni-switch/backend/internal/budget"
)

// keysCall is a request to h from from ("127.0.0.1:5000" or another
// computer's address), with secret as its key when it has one.
func keysCall(h http.Handler, method, path, from, secret, body string, hdr ...string) *httptest.ResponseRecorder {
	var rd io.Reader
	if body != "" {
		rd = strings.NewReader(body)
	}
	r := httptest.NewRequest(method, path, rd)
	r.RemoteAddr = from
	r.Host = "127.0.0.1:3425"
	if !strings.HasPrefix(from, "127.") {
		r.Host = "192.168.1.2:3425"
	}
	if secret != "" {
		r.Header.Set("Authorization", "Bearer "+secret)
	}
	r.Header.Set("Content-Type", "application/json")
	for i := 0; i+1 < len(hdr); i += 2 {
		if hdr[i] == "Host" {
			r.Host = hdr[i+1]
			continue
		}
		r.Header.Set(hdr[i], hdr[i+1])
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func issuedFrom(t *testing.T, w *httptest.ResponseRecorder) access.Issued {
	t.Helper()
	var k access.Issued
	if w.Code != http.StatusCreated || json.Unmarshal(w.Body.Bytes(), &k) != nil || k.Key == "" {
		t.Fatalf("not issued: %d %s", w.Code, w.Body.String())
	}
	return k
}

const (
	fromHere  = "127.0.0.1:5000"
	fromThere = "192.168.1.9:5000"
)

// xazaj on X: an orchestrator issues each sandboxed agent a key that stops
// working by itself. Once it has expired it is refused with a 401 that says
// so on every route, from another computer and from this one too, where a
// request with no key is let in: an agent held to a few models would
// otherwise get every one by its key expiring.
func TestIssuedKeyExpiresOnEveryRoute(t *testing.T) {
	fresh(t)
	t.Setenv("UNI_SWITCH_ADDR", "")
	budget.Forget()
	calls := limitedUpstream(t, 0)
	newCaller(t, "Laptop")
	h := localGuard(New().Handler())

	k := issuedFrom(t, keysCall(h, "POST", KeysPath, fromHere, "", `{"name":"sandbox-1","ttl":"1h","models":["plan/m1"]}`))
	if k.Name != "sandbox-1" || len(k.Models) != 1 || time.Until(k.Expires) > time.Hour || time.Until(k.Expires) < 59*time.Minute {
		t.Fatalf("%+v", k)
	}
	for _, from := range []string{fromThere, fromHere} {
		if w := keysCall(h, "POST", "/v1/chat/completions", from, k.Key, chatReq); w.Code != 200 {
			t.Fatal("an issued key's request from", from, w.Code, w.Body.String())
		}
	}
	if w := keysCall(h, "POST", "/v1/chat/completions", fromThere, k.Key, `{"model":"other/m2","messages":[{"role":"user","content":"hi"}]}`); w.Code == 200 {
		t.Fatal("an issued key used a model it wasn't issued")
	}
	before := calls.Load()

	old := access.Now
	t.Cleanup(func() { access.Now = old })
	access.Now = func() time.Time { return old().Add(61 * time.Minute) }
	for _, from := range []string{fromThere, fromHere} {
		for _, rq := range [][2]string{{"POST", "/v1/chat/completions"}, {"POST", "/v1/messages"}, {"POST", "/v1/responses"}, {"GET", "/v1/models"},
			{"GET", "/v1/uni-switch/usage"}, {"GET", "/v1/uni-switch/quotas"}, {"GET", "/v1/uni-switch/limit"}, {"GET", "/"}, {"POST", KeysPath}} {
			body := ""
			if rq[0] == "POST" {
				body = chatReq
			}
			w := keysCall(h, rq[0], rq[1], from, k.Key, body)
			if w.Code != http.StatusUnauthorized || !strings.Contains(w.Body.String(), "expired at") {
				t.Errorf("an expired key from %s: %s %s: %d %.200s", from, rq[0], rq[1], w.Code, w.Body.String())
			}
		}
	}
	if calls.Load() != before {
		t.Fatal("an expired key's request reached the provider")
	}
	// a request from this computer with no key goes on as before
	if w := keysCall(h, "POST", "/v1/chat/completions", fromHere, "", chatReq); w.Code != 200 {
		t.Fatal("no key from here:", w.Code, w.Body.String())
	}
}

// Who may issue: a program fromHere over loopback with no browser's marks,
// or a key let issue them, never a page, another computer without such a
// key, a key not let, or an issued key; and what a key issues is held to
// it.
func TestKeysEndpointWhoMayIssue(t *testing.T) {
	fresh(t)
	t.Setenv("UNI_SWITCH_ADDR", "")
	budget.Forget()
	limitedUpstream(t, 0)
	keys, secrets := newCaller(t, "Orchestrator")
	h := localGuard(New().Handler())
	body := `{"ttl":"1h"}`

	for name, w := range map[string]*httptest.ResponseRecorder{
		"a page here":                keysCall(h, "POST", KeysPath, fromHere, "", body, "Origin", "https://evil.example"),
		"a page fromHere, no Origin": keysCall(h, "POST", KeysPath, fromHere, "", body, "Sec-Fetch-Site", "cross-site", "User-Agent", "Mozilla/5.0"),
		"a rebound name":             keysCall(h, "POST", KeysPath, fromHere, "", body, "Host", "evil.example"),
		"another computer":           keysCall(h, "POST", KeysPath, fromThere, "", body),
		"a key not let":              keysCall(h, "POST", KeysPath, fromThere, secrets[0], body),
		"a key not let, here":        keysCall(h, "POST", KeysPath, fromHere, secrets[0], body),
		"a list, not let":            keysCall(h, "GET", KeysPath, fromThere, secrets[0], ""),
	} {
		if w.Code == http.StatusCreated || w.Code == http.StatusOK || strings.Contains(w.Body.String(), access.Prefix) {
			t.Errorf("%s issued: %d %.200s", name, w.Code, w.Body.String())
		}
	}
	if _, err := access.Update("models-key", access.Change{Key: keys[0].ID, Models: []string{"plan/m1"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := access.Update("issuer-key", access.Change{Key: keys[0].ID, Issues: true}); err != nil {
		t.Fatal(err)
	}
	if w := keysCall(h, "POST", KeysPath, fromThere, secrets[0], `{"ttl":"1h","models":["other/m2"]}`); w.Code != http.StatusBadRequest {
		t.Fatal("issued a model its issuer may not use:", w.Code, w.Body.String())
	}
	for _, bad := range []string{`{"ttl":"1s"}`, `{"ttl":"forever"}`, `{}`, `{"ttl":-5}`, `not json`} {
		if w := keysCall(h, "POST", KeysPath, fromThere, secrets[0], bad); w.Code != http.StatusBadRequest {
			t.Error("issued with", bad, w.Code, w.Body.String())
		}
	}
	child := issuedFrom(t, keysCall(h, "POST", KeysPath, fromThere, secrets[0], `{"name":"agent-7","ttl":600,"readOnly":false}`))
	if child.Issuer != keys[0].ID || len(child.Models) != 0 {
		t.Fatalf("%+v", child)
	}
	if w := keysCall(h, "POST", "/v1/chat/completions", fromThere, child.Key, chatReq); w.Code != 200 {
		t.Fatal("the issued key:", w.Code, w.Body.String())
	}
	if w := keysCall(h, "POST", KeysPath, fromThere, child.Key, body); w.Code != http.StatusForbidden {
		t.Fatal("an issued key issued one:", w.Code, w.Body.String())
	}
	w := keysCall(h, "GET", KeysPath, fromThere, secrets[0], "")
	if w.Code != 200 || !strings.Contains(w.Body.String(), child.ID) || strings.Contains(w.Body.String(), child.Key) {
		t.Fatal("list:", w.Code, w.Body.String())
	}
	// the user's own list (loopback, no key) holds what they issued alone
	if w := keysCall(h, "GET", KeysPath, fromHere, "", ""); w.Code != 200 || strings.Contains(w.Body.String(), child.ID) {
		t.Fatal("the user's list:", w.Code, w.Body.String())
	}
	if w := keysCall(h, "DELETE", KeysPath+"/"+keys[0].ID, fromThere, secrets[0], ""); w.Code != http.StatusNotFound {
		t.Fatal("an issuer revoked itself:", w.Code, w.Body.String())
	}
	if w := keysCall(h, "DELETE", KeysPath+"/"+child.ID, fromThere, secrets[0], ""); w.Code != 200 {
		t.Fatal("revoke:", w.Code, w.Body.String())
	}
	if w := keysCall(h, "POST", "/v1/chat/completions", fromThere, child.Key, chatReq); w.Code != http.StatusUnauthorized {
		t.Fatal("a revoked key:", w.Code, w.Body.String())
	}
}

// What an issued key spends counts against its issuer's limit: a hundred
// sandboxes each with a key of its own can't spend more than the one key
// they were issued under may.
func TestIssuedKeysSpendTheirIssuersLimit(t *testing.T) {
	fresh(t)
	t.Setenv("UNI_SWITCH_ADDR", "")
	budget.Forget()
	pinLimitClock(t)
	calls := limitedUpstream(t, 0) // 350 tokens a call
	keys, secrets := newCaller(t, "Orchestrator")
	setLimit(t, keys[0].ID, &access.Limit{Period: "day", Tokens: 700})
	if _, err := access.Update("issuer-key", access.Change{Key: keys[0].ID, Issues: true}); err != nil {
		t.Fatal(err)
	}
	h := New().Handler()
	a := issuedFrom(t, keysCall(h, "POST", KeysPath, fromThere, secrets[0], `{"ttl":"1h"}`))
	b := issuedFrom(t, keysCall(h, "POST", KeysPath, fromThere, secrets[0], `{"ttl":"1h"}`))
	for _, k := range []string{a.Key, b.Key} {
		if w := keysCall(h, "POST", "/v1/chat/completions", fromThere, k, chatReq); w.Code != 200 {
			t.Fatal("within the issuer's limit:", w.Code, w.Body.String())
		}
	}
	for name, k := range map[string]string{"a": a.Key, "b": b.Key, "the issuer": secrets[0]} {
		if w := keysCall(h, "POST", "/v1/chat/completions", fromThere, k, chatReq); w.Code != http.StatusTooManyRequests || !strings.Contains(w.Body.String(), "Orchestrator") {
			t.Errorf("%s past the issuer's limit: %d %.200s", name, w.Code, w.Body.String())
		}
	}
	if calls.Load() != 2 {
		t.Fatal("calls past the issuer's limit reached the provider:", calls.Load())
	}
	w := keysCall(h, "GET", "/v1/uni-switch/limit", fromThere, a.Key, "")
	var lim struct {
		Expires      time.Time
		IssuerLimits []budget.Status
	}
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &lim) != nil || lim.Expires.IsZero() || len(lim.IssuerLimits) != 1 || lim.IssuerLimits[0].Calls != 2 {
		t.Fatal("limit:", w.Code, w.Body.String())
	}
}

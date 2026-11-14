package gateway

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/access"
	"github.com/dieqiyun/uni-switch/backend/internal/budget"
	"github.com/dieqiyun/uni-switch/backend/internal/oidc"
	"github.com/dieqiyun/uni-switch/backend/internal/oidc/oidctest"
	"github.com/dieqiyun/uni-switch/backend/internal/settings"
)

// #4531 (xazaj on X): a GitHub Actions job trades its OIDC token for a
// short-lived key from another machine, with no gateway key of its own.
// The key is issued under the rule's key, lives no longer than the rule
// says, and works; a token no rule takes, a refused one past the burst,
// and any token while uni-switch isn't shared get nothing.
func TestOIDCTokenTradedForKey(t *testing.T) {
	fresh(t)
	t.Setenv("UNI_SWITCH_ADDR", "")
	budget.Forget()
	limitedUpstream(t, 0)
	resetAuthThrottle()
	t.Cleanup(resetAuthThrottle)
	is := oidctest.New(t)
	keys, _ := newCaller(t, "CI")
	h := localGuard(New().Handler())
	now := time.Now().Unix()
	jti := 0
	token := func(aud string, over map[string]any) string {
		jti++
		c := map[string]any{"iss": is.URL, "aud": aud, "repository": "acme/app", "ref": "refs/heads/main", "run_id": "42",
			"sub": "repo:acme/app:ref:refs/heads/main", "iat": now, "exp": now + 300, "jti": strings.Repeat("j", jti)}
		for k, v := range over {
			c[k] = v
		}
		return is.Sign(t, "k1", c)
	}

	if w := keysCall(h, "POST", OIDCKeysPath, fromThere, token("uni-switch-x", nil), ""); w.Code != http.StatusUnauthorized || !strings.Contains(w.Body.String(), "no rule") {
		t.Fatal("no rule:", w.Code, w.Body.String())
	}
	if _, err := access.Update("issuer-key", access.Change{Key: keys[0].ID, Issues: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := access.Update("models-key", access.Change{Key: keys[0].ID, Models: []string{"plan/m1"}}); err != nil {
		t.Fatal(err)
	}
	rule, err := oidc.Add(oidc.Rule{Key: keys[0].ID, Issuer: is.URL, Claims: map[string]string{"repository": "acme/app", "ref": "refs/heads/main"}})
	if err != nil {
		t.Fatal(err)
	}
	w := keysCall(h, "POST", OIDCKeysPath, fromThere, token(rule.Audience, nil), `{"ttl":"48h"}`)
	k := issuedFrom(t, w)
	if k.Issuer != keys[0].ID || k.Name != "acme/app@main (run 42)" || time.Until(k.Expires) > time.Hour || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("%+v", k)
	}
	if w := keysCall(h, "POST", "/v1/chat/completions", fromThere, k.Key, chatReq); w.Code != 200 {
		t.Fatal("the key traded for:", w.Code, w.Body.String())
	}
	// bounded by its issuer: a model the rule's key may not use is refused
	if w := keysCall(h, "POST", OIDCKeysPath, fromThere, token(rule.Audience, nil), `{"models":["other/m2"]}`); w.Code != http.StatusBadRequest {
		t.Fatal("issued a model its issuer may not use:", w.Code, w.Body.String())
	}
	// a token from a branch the rule doesn't name, and none at all
	if w := keysCall(h, "POST", OIDCKeysPath, fromThere, token(rule.Audience, map[string]any{"ref": "refs/heads/fork"}), ""); w.Code != http.StatusUnauthorized || strings.Contains(w.Body.String(), access.Prefix) {
		t.Fatal("another branch:", w.Code, w.Body.String())
	}
	if w := keysCall(h, "POST", OIDCKeysPath, fromThere, "", ""); w.Code != http.StatusUnauthorized {
		t.Fatal("no token:", w.Code, w.Body.String())
	}
	// refused tokens count against the failed authentications
	var last int
	for range authFailBurst + 1 {
		last = keysCall(h, "POST", OIDCKeysPath, fromThere, "not.a.jwt", "").Code
	}
	if last != http.StatusTooManyRequests {
		t.Fatal("refused tokens weren't counted:", last)
	}
	resetAuthThrottle()
	// the issuer turned off: the rule issues nothing
	if _, err := access.Update("issuer-key", access.Change{Key: keys[0].ID, Issues: false}); err != nil {
		t.Fatal(err)
	}
	if w := keysCall(h, "POST", OIDCKeysPath, fromThere, token(rule.Audience, nil), ""); w.Code == http.StatusCreated {
		t.Fatal("issued under a key no longer let issue")
	}
	if _, err := access.Update("issuer-key", access.Change{Key: keys[0].ID, Issues: true}); err != nil {
		t.Fatal(err)
	}
	// the exemption is this route alone: a token on a model route is a wrong key
	if w := keysCall(h, "POST", "/v1/chat/completions", fromThere, token(rule.Audience, nil), chatReq); w.Code != http.StatusUnauthorized {
		t.Fatal("a token on a model route:", w.Code, w.Body.String())
	}
	// this computer may trade one too
	issuedFrom(t, keysCall(h, "POST", OIDCKeysPath, fromHere, token(rule.Audience, nil), ""))
	// not shared: another machine gets nothing, token or not
	s := settings.Load()

	if err := settings.Save(s); err != nil {
		t.Fatal(err)
	}
	if w := keysCall(h, "POST", OIDCKeysPath, fromThere, token(rule.Audience, nil), ""); w.Code != http.StatusForbidden {
		t.Fatal("not shared:", w.Code, w.Body.String())
	}
}

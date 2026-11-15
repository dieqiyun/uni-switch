package oidc_test

import (
	"context"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/access"
	"github.com/dieqiyun/uni-switch/backend/internal/oidc"
	"github.com/dieqiyun/uni-switch/backend/internal/oidc/oidctest"
	"github.com/dieqiyun/uni-switch/backend/internal/testenv"
)

func TestMain(m *testing.M) { testenv.Main(m) }

// issuerKey is a gateway key let issue keys, as a rule names.
func issuerKey(t *testing.T) string {
	t.Helper()
	if _, err := access.Update("add-key", access.Change{Name: "CI"}); err != nil {
		t.Fatal(err)
	}
	keys, _ := access.List()
	id := keys[len(keys)-1].ID
	if _, err := access.Update("issuer-key", access.Change{Key: id, Issues: true}); err != nil {
		t.Fatal(err)
	}
	return id
}

var jtis atomic.Int64

func claims(is *oidctest.Issuer, aud string, over map[string]any) map[string]any {
	now := time.Now().Unix()
	c := map[string]any{"iss": is.URL, "aud": aud, "sub": "repo:acme/app:ref:refs/heads/main",
		"repository": "acme/app", "repository_owner": "acme", "ref": "refs/heads/main",
		"run_id": "42", "iat": now, "nbf": now, "exp": now + 300, "jti": strconv.FormatInt(jtis.Add(1), 10)}
	for k, v := range over {
		if v == nil {
			delete(c, k)
		} else {
			c[k] = v
		}
	}
	return c
}

// A token from the rule's issuer, with its audience and every claim it
// names, matches; one that is off in any single way doesn't. Each case
// changes one thing from the one that matches.
func TestMatchEachCheck(t *testing.T) {
	is := oidctest.New(t)
	key := issuerKey(t)
	r, err := oidc.Add(oidc.Rule{Key: key, Issuer: is.URL, Claims: map[string]string{"repository": "acme/app", "ref": "refs/heads/*"}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(r.Audience, "uni-switch-") || r.Lifetime() != oidc.DefaultTTL {
		t.Fatalf("defaults: %+v", r)
	}
	ctx := context.Background()
	good := is.Sign(t, "k1", claims(is, r.Audience, nil))
	got, c, err := oidc.Match(ctx, good)
	if err != nil || got.ID != r.ID || c.Str("run_id") != "42" {
		t.Fatal(got, c, err)
	}
	if _, _, err := oidc.Match(ctx, good); err == nil || !strings.Contains(err.Error(), "already") {
		t.Fatal("a token traded twice:", err)
	}
	stranger := is.AddKey(t, "spare")
	now := time.Now().Unix()
	for name, tok := range map[string]string{
		"another repository":    is.Sign(t, "k1", claims(is, r.Audience, map[string]any{"repository": "evil/app"})),
		"a tag, not a branch":   is.Sign(t, "k1", claims(is, r.Audience, map[string]any{"ref": "refs/tags/v1"})),
		"no ref":                is.Sign(t, "k1", claims(is, r.Audience, map[string]any{"ref": nil})),
		"another audience":      is.Sign(t, "k1", claims(is, "uni-switch-other", nil)),
		"no audience":           is.Sign(t, "k1", claims(is, r.Audience, map[string]any{"aud": nil})),
		"expired":               is.Sign(t, "k1", claims(is, r.Audience, map[string]any{"exp": now - 120})),
		"no expiry":             is.Sign(t, "k1", claims(is, r.Audience, map[string]any{"exp": nil})),
		"not yet valid":         is.Sign(t, "k1", claims(is, r.Audience, map[string]any{"nbf": now + 600})),
		"issued in the future":  is.Sign(t, "k1", claims(is, r.Audience, map[string]any{"iat": now + 600})),
		"another issuer":        is.Sign(t, "k1", claims(is, r.Audience, map[string]any{"iss": "https://evil.example"})),
		"signed by another key": is.Sign(t, "k1", claims(is, r.Audience, nil), stranger),
		"an unknown key id":     is.Sign(t, "nope", claims(is, r.Audience, nil), stranger),
		"not a JWT":             "abc.def",
		"alg none":              "eyJhbGciOiJub25lIn0." + strings.Split(good, ".")[1] + ".",
	} {
		if _, _, err := oidc.Match(ctx, tok); err == nil {
			t.Errorf("%s matched", name)
		}
	}
	// a list audience holding the rule's matches too
	if _, _, err := oidc.Match(ctx, is.Sign(t, "k1", claims(is, "", map[string]any{"aud": []string{"x", r.Audience}}))); err != nil {
		t.Fatal("a list audience:", err)
	}
	// the refusal says the claims, so a workflow's author sees which is off
	_, _, err = oidc.Match(ctx, is.Sign(t, "k1", claims(is, r.Audience, map[string]any{"repository": "evil/app"})))
	if err == nil || !strings.Contains(err.Error(), `repository="evil/app"`) {
		t.Fatal(err)
	}
}

// Keys are read once, again for a key id they don't have at most once in
// ten seconds, and kept as last read while the issuer can't be reached; an
// issuer no rule names is never asked.
func TestKeysCachedAndKept(t *testing.T) {
	is := oidctest.New(t)
	key := issuerKey(t)
	r, err := oidc.Add(oidc.Rule{Key: key, Issuer: is.URL, Claims: map[string]string{"repository": "acme/app"}})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, _, err := oidc.Match(ctx, is.Sign(t, "k1", claims(is, r.Audience, nil))); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if n := is.Fetches.Load(); n != 1 {
		t.Fatalf("keys read %d times for eight tokens", n)
	}
	// a rotated key is read for, once; a made-up one again only after a minute
	is.AddKey(t, "k2")
	clock := time.Now().Add(11 * time.Second)
	oidc.Now = func() time.Time { return clock }
	t.Cleanup(func() { oidc.Now = time.Now })
	if _, _, err := oidc.Match(ctx, is.Sign(t, "k2", claims(is, r.Audience, nil))); err != nil {
		t.Fatal("a rotated key:", err)
	}
	stray := is.AddKey(t, "k3")
	clock = clock.Add(11 * time.Second)
	for range 5 {
		oidc.Match(ctx, is.Sign(t, "zz", claims(is, r.Audience, nil), stray))
	}
	if n := is.Fetches.Load(); n != 3 {
		t.Fatalf("keys read %d times, want 3 (first, k2, one for zz)", n)
	}
	// the issuer down: the keys as last read still serve, past their hour
	is.Down.Store(true)
	clock = clock.Add(2 * time.Hour)
	if _, _, err := oidc.Match(ctx, is.Sign(t, "k1", claims(is, r.Audience, map[string]any{"exp": clock.Unix() + 60, "iat": clock.Unix(), "nbf": clock.Unix()}))); err != nil {
		t.Fatal("kept keys:", err)
	}
	// an issuer no rule names is never asked for keys
	before := is.Fetches.Load()
	is.Down.Store(false)
	other := oidctest.New(t)
	if _, _, err := oidc.Match(ctx, other.Sign(t, "k1", claims(other, r.Audience, nil))); err == nil {
		t.Fatal("matched an issuer no rule names")
	}
	if other.Fetches.Load() != 0 || is.Fetches.Load() != before {
		t.Fatal("asked an issuer no rule names for keys")
	}
}

// An issuer that can't be read, with no keys read before, lets nothing in.
func TestUnreadKeysLetNothingIn(t *testing.T) {
	is := oidctest.New(t)
	is.Down.Store(true)
	r, err := oidc.Add(oidc.Rule{Key: issuerKey(t), Issuer: is.URL, Claims: map[string]string{"repository": "acme/app"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := oidc.Match(context.Background(), is.Sign(t, "k1", claims(is, r.Audience, nil))); err == nil || !strings.Contains(err.Error(), "can't be read") {
		t.Fatal(err)
	}
}

// A rule that would let in any repository's workflow on GitHub, a key not
// let issue keys, and a TTL out of bounds are refused.
func TestAddRefuses(t *testing.T) {
	key := issuerKey(t)
	if _, err := access.Update("add-key", access.Change{Name: "plain"}); err != nil {
		t.Fatal(err)
	}
	keys, _ := access.List()
	plain := keys[len(keys)-1].ID
	for name, r := range map[string]oidc.Rule{
		"GitHub, no repository":        {Key: key, Claims: map[string]string{"ref": "refs/heads/main"}},
		"GitHub, a wildcard owner":     {Key: key, Claims: map[string]string{"repository_owner": "*"}},
		"GitHub, a wildcard repo":      {Key: key, Claims: map[string]string{"repository": "acme/*"}},
		"no claims":                    {Key: key, Issuer: "https://idp.example"},
		"http issuer":                  {Key: key, Issuer: "http://idp.example", Claims: map[string]string{"sub": "x"}},
		"a key not let issue":          {Key: plain, Claims: map[string]string{"repository": "acme/app"}},
		"no such key":                  {Key: "nope", Claims: map[string]string{"repository": "acme/app"}},
		"a ttl too long":               {Key: key, Claims: map[string]string{"repository": "acme/app"}, TTL: int64(31 * 24 * 3600)},
		"a ttl too short":              {Key: key, Claims: map[string]string{"repository": "acme/app"}, TTL: 10},
		"a pattern path.Match refuses": {Key: key, Claims: map[string]string{"repository": "acme/app", "ref": "["}},
	} {
		if _, err := oidc.Add(r); err == nil {
			t.Errorf("%s: added", name)
		}
	}
	r, err := oidc.Add(oidc.Rule{Key: key, Claims: map[string]string{"repository_owner": "acme"}})
	if err != nil || r.Issuer != oidc.GitHub {
		t.Fatal(r, err)
	}
	if err := oidc.Remove(r.ID); err != nil {
		t.Fatal(err)
	}
	if err := oidc.Remove(r.ID); err == nil {
		t.Fatal("removed twice")
	}
}

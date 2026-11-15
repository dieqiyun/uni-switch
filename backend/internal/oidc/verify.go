package oidc

import (
	"context"
	"crypto"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/url"
	"path"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Client fetches the issuers' keys; a variable for the tests, which serve
// them from an httptest TLS server and never reach the network.
var Client = &http.Client{Timeout: 10 * time.Second}

// Now is the clock tokens are judged by.
var Now = time.Now

// leeway is how far apart this computer's clock and the issuer's may be.
const leeway = time.Minute

// Claims are a verified token's claims.
type Claims map[string]any

// Str is claim c as text: a string as it is, a number or bool written out.
func (c Claims) Str(name string) string {
	switch v := c[name].(type) {
	case string:
		return v
	case json.Number:
		return v.String()
	case bool:
		return strconv.FormatBool(v)
	}
	return ""
}

// Match verifies token and finds the first rule it satisfies. A token is
// judged only against rules that name its issuer, so an issuer no rule
// names is never asked for keys: a token can't make uni-switch fetch from a
// URL of its choosing.
func Match(ctx context.Context, token string) (Rule, Claims, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return Rule{}, nil, errors.New("the bearer token isn't a JWT")
	}
	var head struct {
		Alg string `json:"alg"`
		Kid string `json:"kid"`
	}
	if err := decodePart(parts[0], &head); err != nil {
		return Rule{}, nil, errors.New("the token's header can't be read")
	}
	if head.Alg != "RS256" {
		return Rule{}, nil, fmt.Errorf("the token is signed with %q: only RS256 is taken", clip(head.Alg))
	}
	var claims Claims
	if err := decodePart(parts[1], &claims); err != nil {
		return Rule{}, nil, errors.New("the token's claims can't be read")
	}
	iss := claims.Str("iss")
	rules, err := List()
	if err != nil {
		return Rule{}, nil, err
	}
	rules = slices.DeleteFunc(rules, func(r Rule) bool { return r.Issuer != strings.TrimRight(iss, "/") })
	if len(rules) == 0 {
		return Rule{}, nil, fmt.Errorf("no rule takes tokens from %q (uni-switch gateway-key oidc add)", clip(iss))
	}
	sig, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return Rule{}, nil, errors.New("the token's signature can't be read")
	}
	pub, err := publicKey(ctx, rules[0].Issuer, head.Kid)
	if err != nil {
		return Rule{}, nil, err
	}
	sum := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	if rsa.VerifyPKCS1v15(pub, crypto.SHA256, sum[:], sig) != nil {
		return Rule{}, nil, errors.New("the token's signature is not its issuer's")
	}
	now := Now()
	exp, ok := claimTime(claims, "exp")
	if !ok {
		return Rule{}, nil, errors.New("the token has no expiry")
	}
	if !now.Before(exp.Add(leeway)) {
		return Rule{}, nil, errors.New("the token has expired")
	}
	if nbf, ok := claimTime(claims, "nbf"); ok && now.Add(leeway).Before(nbf) {
		return Rule{}, nil, errors.New("the token isn't valid yet")
	}
	if iat, ok := claimTime(claims, "iat"); ok && now.Add(leeway).Before(iat) {
		return Rule{}, nil, errors.New("the token was issued in the future: check this computer's clock")
	}
	aud := audiences(claims["aud"])
	for _, r := range rules {
		if !slices.Contains(aud, r.Audience) || !claimsMatch(r.Claims, claims) {
			continue
		}
		if jti := claims.Str("jti"); jti != "" && !firstUse(iss+"\x00"+jti, exp.Add(leeway), now) {
			return Rule{}, nil, errors.New("this token was traded for a key already: ask the issuer for a new one")
		}
		return r, claims, nil
	}
	return Rule{}, nil, fmt.Errorf("the token matches no rule: it has aud %q and %s", strings.Join(aud, ","), described(claims, rules))
}

func decodePart(s string, v any) error {
	b, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return err
	}
	d := json.NewDecoder(strings.NewReader(string(b)))
	d.UseNumber()
	return d.Decode(v)
}

// clip keeps an unverified value short in a reply and the log.
func clip(s string) string {
	if len(s) > 100 {
		return s[:100] + "…"
	}
	return s
}

func claimTime(c Claims, name string) (time.Time, bool) {
	n, ok := c[name].(json.Number)
	if !ok {
		return time.Time{}, false
	}
	f, err := n.Float64()
	if err != nil {
		return time.Time{}, false
	}
	return time.Unix(int64(f), 0), true
}

func audiences(v any) []string {
	switch a := v.(type) {
	case string:
		return []string{a}
	case []any:
		var out []string
		for _, x := range a {
			if s, ok := x.(string); ok {
				out = append(out, s)
			}
		}
		return out
	}
	return nil
}

func claimsMatch(want map[string]string, c Claims) bool {
	for k, pattern := range want {
		if _, ok := c[k]; !ok {
			return false
		}
		if ok, _ := path.Match(pattern, c.Str(k)); !ok {
			return false
		}
	}
	return true
}

// described names the token's values of the claims the rules ask about,
// so whoever wrote the workflow sees which one is off.
func described(c Claims, rules []Rule) string {
	seen := map[string]bool{}
	var names []string
	for _, r := range rules {
		for k := range r.Claims {
			if !seen[k] {
				seen[k] = true
				names = append(names, k)
			}
		}
	}
	sort.Strings(names)
	parts := make([]string, len(names))
	for i, k := range names {
		parts[i] = k + "=" + strconv.Quote(clip(c.Str(k)))
	}
	return strings.Join(parts, ", ")
}

// used are the token ids traded, each until its token expires.
var used = struct {
	sync.Mutex
	m map[string]time.Time
}{m: map[string]time.Time{}}

func firstUse(id string, until, now time.Time) bool {
	used.Lock()
	defer used.Unlock()
	for k, t := range used.m {
		if !now.Before(t) {
			delete(used.m, k)
		}
	}
	if _, ok := used.m[id]; ok {
		return false
	}
	used.m[id] = until
	return true
}

// keySet is an issuer's signing keys as last read.
type keySet struct {
	keys    map[string]*rsa.PublicKey
	fetched time.Time     // when keys were read
	tried   time.Time     // when they were last asked for
	loading chan struct{} // closed when a read under way ends
}

var sets = struct {
	sync.Mutex
	m map[string]*keySet
}{m: map[string]*keySet{}}

// Keys are read again after keysFresh, and at most once in refetchGap
// for a key id they don't have, so tokens with made-up ids can't make
// uni-switch ask the issuer on every request.
const (
	keysFresh  = time.Hour
	refetchGap = 10 * time.Second
)

// publicKey is the issuer's key kid. Keys that can't be read again keep
// serving as last read; with none read, nothing is let in.
func publicKey(ctx context.Context, iss, kid string) (*rsa.PublicKey, error) {
	for {
		sets.Lock()
		ks := sets.m[iss]
		if ks == nil {
			ks = &keySet{}
			sets.m[iss] = ks
		}
		now := Now()
		k, have := ks.keys[kid]
		stale := ks.fetched.IsZero() || now.Sub(ks.fetched) > keysFresh || !have
		if !stale || (!ks.tried.IsZero() && now.Sub(ks.tried) < refetchGap && ks.loading == nil) {
			sets.Unlock()
			if have {
				return k, nil
			}
			return nil, fmt.Errorf("the token's key %q isn't one %s signs with", clip(kid), iss)
		}
		if wait := ks.loading; wait != nil {
			sets.Unlock()
			select {
			case <-wait:
				continue
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}
		done := make(chan struct{})
		ks.loading, ks.tried = done, now
		sets.Unlock()
		keys, err := fetchKeys(ctx, iss)
		sets.Lock()
		if err == nil {
			ks.keys, ks.fetched = keys, Now()
		}
		ks.loading = nil
		close(done)
		k, have = ks.keys[kid]
		sets.Unlock()
		if have {
			return k, nil
		}
		if err != nil {
			return nil, fmt.Errorf("%s's signing keys can't be read: %v", iss, err)
		}
		return nil, fmt.Errorf("the token's key %q isn't one %s signs with", clip(kid), iss)
	}
}

func getJSON(ctx context.Context, u string, v any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	resp, err := Client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s answered %s", u, resp.Status)
	}
	return json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(v)
}

// fetchKeys reads the issuer's RSA keys from its discovery document's
// jwks_uri, which is https too.
func fetchKeys(ctx context.Context, iss string) (map[string]*rsa.PublicKey, error) {
	var conf struct {
		Issuer  string `json:"issuer"`
		JWKSURI string `json:"jwks_uri"`
	}
	if err := getJSON(ctx, iss+"/.well-known/openid-configuration", &conf); err != nil {
		return nil, err
	}
	if strings.TrimRight(conf.Issuer, "/") != iss {
		return nil, fmt.Errorf("its discovery document names the issuer %q", clip(conf.Issuer))
	}
	if u, err := url.Parse(conf.JWKSURI); err != nil || u.Scheme != "https" {
		return nil, fmt.Errorf("its jwks_uri isn't https: %q", clip(conf.JWKSURI))
	}
	var set struct {
		Keys []struct {
			Kty string `json:"kty"`
			Kid string `json:"kid"`
			Use string `json:"use"`
			N   string `json:"n"`
			E   string `json:"e"`
		} `json:"keys"`
	}
	if err := getJSON(ctx, conf.JWKSURI, &set); err != nil {
		return nil, err
	}
	keys := map[string]*rsa.PublicKey{}
	for _, k := range set.Keys {
		if k.Kty != "RSA" || (k.Use != "" && k.Use != "sig") {
			continue
		}
		n, err1 := base64.RawURLEncoding.DecodeString(k.N)
		e, err2 := base64.RawURLEncoding.DecodeString(k.E)
		if err1 != nil || err2 != nil || len(e) == 0 || len(e) > 4 {
			continue
		}
		pub := &rsa.PublicKey{N: new(big.Int).SetBytes(n), E: int(new(big.Int).SetBytes(e).Int64())}
		if pub.N.BitLen() < 2048 || pub.E < 3 {
			continue
		}
		keys[k.Kid] = pub
	}
	if len(keys) == 0 {
		return nil, errors.New("it lists no RSA signing key")
	}
	return keys, nil
}

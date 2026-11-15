// Package oidctest serves an OIDC issuer on an httptest TLS server for the
// tests of package oidc and the gateway: its discovery document, its keys
// and the tokens it signs, never reaching the network.
package oidctest

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/oidc"
)

// Issuer is one issuer. Fetches counts the times its keys were read;
// Down makes it answer 503.
type Issuer struct {
	URL     string
	Fetches atomic.Int32
	Down    atomic.Bool
	mu      sync.Mutex
	keys    map[string]*rsa.PrivateKey
}

// New starts an issuer with one key, "k1", and points oidc.Client at it.
func New(t testing.TB) *Issuer {
	t.Helper()
	is := &Issuer{keys: map[string]*rsa.PrivateKey{}}
	is.AddKey(t, "k1")
	srv := httptest.NewTLSServer(http.HandlerFunc(is.serve))
	t.Cleanup(srv.Close)
	is.URL = srv.URL
	was := oidc.Client
	oidc.Client = srv.Client()
	t.Cleanup(func() { oidc.Client = was })
	return is
}

// AddKey makes the issuer sign with a new key kid as well.
func (is *Issuer) AddKey(t testing.TB, kid string) *rsa.PrivateKey {
	t.Helper()
	k, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	is.mu.Lock()
	is.keys[kid] = k
	is.mu.Unlock()
	return k
}

func (is *Issuer) serve(w http.ResponseWriter, r *http.Request) {
	if is.Down.Load() {
		http.Error(w, "down", http.StatusServiceUnavailable)
		return
	}
	switch r.URL.Path {
	case "/.well-known/openid-configuration":
		json.NewEncoder(w).Encode(map[string]string{"issuer": is.URL, "jwks_uri": is.URL + "/jwks"})
	case "/jwks":
		is.Fetches.Add(1)
		is.mu.Lock()
		var keys []map[string]string
		for kid, k := range is.keys {
			keys = append(keys, map[string]string{"kty": "RSA", "kid": kid, "use": "sig", "alg": "RS256",
				"n": b64(k.N.Bytes()), "e": b64(big.NewInt(int64(k.E)).Bytes())})
		}
		is.mu.Unlock()
		json.NewEncoder(w).Encode(map[string]any{"keys": keys})
	default:
		http.NotFound(w, r)
	}
}

func b64(b []byte) string { return base64.RawURLEncoding.EncodeToString(b) }

// Sign makes a token with claims, signed by the key kid (alg RS256), or
// by key when it is given (a key the issuer doesn't list).
func (is *Issuer) Sign(t testing.TB, kid string, claims map[string]any, key ...*rsa.PrivateKey) string {
	t.Helper()
	is.mu.Lock()
	k := is.keys[kid]
	is.mu.Unlock()
	if len(key) > 0 {
		k = key[0]
	}
	h, _ := json.Marshal(map[string]string{"alg": "RS256", "typ": "JWT", "kid": kid})
	c, _ := json.Marshal(claims)
	in := b64(h) + "." + b64(c)
	sum := sha256.Sum256([]byte(in))
	sig, err := rsa.SignPKCS1v15(rand.Reader, k, crypto.SHA256, sum[:])
	if err != nil {
		t.Fatal(err)
	}
	return in + "." + b64(sig)
}

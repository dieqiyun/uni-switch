// Package oidc lets a CI job trade an OIDC token for a short-lived gateway
// key (#4531, xazaj on X: GitHub Actions' token in place of a key kept in
// the repository's secrets). The user writes a rule: which issuer, which
// audience, which claims (repository, ref, environment…), and which of
// their gateway keys the key is issued under; a token that is signed by
// that issuer and matches every claim gets a key, held to everything that
// gateway key is held to and living no longer than the rule says.
//
// No rule, no exchange: none is made by default.
package oidc

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/access"
	"github.com/dieqiyun/uni-switch/backend/internal/edit"
	"github.com/dieqiyun/uni-switch/backend/internal/filelock"
	"github.com/dieqiyun/uni-switch/backend/internal/settings"
)

// GitHub is GitHub Actions' issuer, a rule's when it names none.
const GitHub = "https://token.actions.githubusercontent.com"

// DefaultTTL is how long a key lives when the rule says nothing.
const DefaultTTL = time.Hour

// githubScopes are the claims of which a GitHub rule names one, with no
// wildcard: without it, any repository's workflow on GitHub could get a
// token GitHub signs with the rule's audience.
var githubScopes = []string{"repository", "repository_owner", "repository_id", "repository_owner_id"}

// Rule is one way in.
type Rule struct {
	ID string `json:"id"`
	// Key is the id of the gateway key the keys are issued under; it must
	// be let issue keys (uni-switch gateway-key issuer <id> on).
	Key    string `json:"key"`
	Issuer string `json:"issuer"`
	// Audience is the token's aud the rule takes: by default
	// "uni-switch-<id>", so a token asked for one rule opens no other.
	Audience string `json:"audience"`
	// Claims each name a claim and the value it must have: exact, or a
	// glob as path.Match reads it ("refs/heads/release/*").
	Claims map[string]string `json:"claims"`
	// TTL is the longest a key it issues lives, in seconds.
	TTL int64 `json:"ttl"`
}

// Lifetime is the rule's TTL as a duration.
func (r Rule) Lifetime() time.Duration { return time.Duration(r.TTL) * time.Second }

var mu = filelock.Mutex{Path: func() string { return Path() + ".lock" }}

// Path is the rules' file.
func Path() string { return filepath.Join(settings.Dir(), "oidc-rules.json") }

// load reads the rules; a file that can't be read or parsed is an error,
// never "no rules" read as something else.
func load() ([]Rule, error) {
	b, err := os.ReadFile(Path())
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var rules []Rule
	if err := json.Unmarshal(b, &rules); err != nil {
		return nil, fmt.Errorf("%s can't be read: %w", Path(), err)
	}
	return rules, nil
}

func save(rules []Rule) error {
	b, err := json.MarshalIndent(rules, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(Path()), 0o755); err != nil {
		return err
	}
	return edit.WriteAtomic(Path(), append(b, '\n'))
}

// List is every rule.
func List() ([]Rule, error) {
	mu.Lock()
	defer mu.Unlock()
	return load()
}

// Add checks r and keeps it, and gives it back with its id and defaults.
func Add(r Rule) (Rule, error) {
	r.Issuer = strings.TrimRight(strings.TrimSpace(r.Issuer), "/")
	if r.Issuer == "" {
		r.Issuer = GitHub
	}
	if u, err := url.Parse(r.Issuer); err != nil || u.Scheme != "https" || u.Host == "" || u.RawQuery != "" || u.Fragment != "" {
		return Rule{}, fmt.Errorf("the issuer is an https URL, such as %s", GitHub)
	}
	if len(r.Claims) == 0 {
		return Rule{}, errors.New("a rule names at least one claim the token must have, such as repository=owner/name")
	}
	for k, v := range r.Claims {
		if strings.TrimSpace(k) != k || k == "" || v == "" {
			return Rule{}, fmt.Errorf("a claim is name=value, both given: %q=%q", k, v)
		}
		if _, err := path.Match(v, ""); err != nil {
			return Rule{}, fmt.Errorf("the value of %s isn't a pattern path.Match reads: %q", k, v)
		}
	}
	if r.Issuer == GitHub && !slices.ContainsFunc(githubScopes, func(c string) bool {
		v, ok := r.Claims[c]
		return ok && !strings.ContainsAny(v, `*?[\`)
	}) {
		return Rule{}, errors.New("a GitHub rule names the repository (repository=owner/name) or its owner (repository_owner=owner) without a wildcard: otherwise any repository's workflow on GitHub could get a key")
	}
	if r.TTL == 0 {
		r.TTL = int64(DefaultTTL / time.Second)
	}
	if d := r.Lifetime(); d < access.MinTTL || d > access.MaxTTL {
		return Rule{}, fmt.Errorf("a key lives from %s to %d days", access.MinTTL, int(access.MaxTTL.Hours()/24))
	}
	keys, err := access.List()
	if err != nil {
		return Rule{}, err
	}
	i := slices.IndexFunc(keys, func(k access.Key) bool { return k.ID == r.Key })
	if i < 0 {
		return Rule{}, fmt.Errorf("no gateway key has the id %q (uni-switch gateway-key list)", r.Key)
	}
	if !keys[i].Issues || keys[i].Issuer != "" {
		return Rule{}, fmt.Errorf("the gateway key %s can't issue keys: let it with uni-switch gateway-key issuer %s on", keys[i].Name, r.Key)
	}
	b := make([]byte, 6)
	if _, err := rand.Read(b); err != nil {
		return Rule{}, err
	}
	r.ID = hex.EncodeToString(b)
	r.Audience = strings.TrimSpace(r.Audience)
	if r.Audience == "" {
		r.Audience = "uni-switch-" + r.ID
	}
	mu.Lock()
	defer mu.Unlock()
	rules, err := load()
	if err != nil {
		return Rule{}, err
	}
	return r, save(append(rules, r))
}

// Remove takes the rule id away.
func Remove(id string) error {
	mu.Lock()
	defer mu.Unlock()
	rules, err := load()
	if err != nil {
		return err
	}
	i := slices.IndexFunc(rules, func(r Rule) bool { return r.ID == id })
	if i < 0 {
		return fmt.Errorf("no rule has the id %q (uni-switch gateway-key oidc list)", id)
	}
	return save(slices.Delete(rules, i, i+1))
}

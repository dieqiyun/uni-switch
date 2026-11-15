package access

import (
	"errors"
	"slices"
	"strings"
	"testing"
	"time"
)

// clockAt holds Now at t for the test; the returned func moves it.
func clockAt(t *testing.T, at time.Time) func(time.Duration) {
	t.Helper()
	old := Now
	t.Cleanup(func() { Now = old })
	Now = func() time.Time { return at }
	return func(d time.Duration) { at = at.Add(d) }
}

func addKey(t *testing.T, name string) (id, secret string) {
	t.Helper()
	secret, err := Update("add-key", Change{Name: name})
	if err != nil {
		t.Fatal(err)
	}
	keys, err := List()
	if err != nil {
		t.Fatal(err)
	}
	return keys[len(keys)-1].ID, secret
}

// xazaj on X: an orchestrator gives each sandboxed agent a key that stops
// working by itself. Before its expiry it is let in; at it, every
// authentication refuses it with why, and an hour later it is gone.
func TestIssuedKeyExpires(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	advance := clockAt(t, time.Date(2026, 10, 31, 9, 0, 0, 0, time.UTC))
	k, err := IssueKey(Issue{Name: "sandbox-1", TTL: time.Hour, Models: []string{"openai/gpt-5"}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(k.Key, Prefix) || !k.Expires.Equal(Now().Add(time.Hour)) {
		t.Fatal(k)
	}
	who, ok := Authenticate(k.Key)
	if !ok || who.KeyID != k.ID || !who.Expires.Equal(k.Expires) || !slices.Equal(who.Models, []string{"openai/gpt-5"}) {
		t.Fatal(who, ok)
	}
	if !On(k.ID) {
		t.Fatal("an unexpired key is not on")
	}
	advance(time.Hour)
	if _, ok := Authenticate(k.Key); ok {
		t.Fatal("an expired key was let in")
	}
	_, err = Check(k.Key)
	var refused *Refused
	if !errors.As(err, &refused) || !strings.Contains(refused.Why, "expired at 2026-10-31T10:00:00Z") {
		t.Fatal(err)
	}
	if On(k.ID) {
		t.Fatal("an expired key is on")
	}
	// kept an hour for the message, then removed
	if n, err := Prune(); n != 0 || err != nil {
		t.Fatal(n, err)
	}
	advance(expiredKept + time.Second)
	if n, err := Prune(); n != 1 || err != nil {
		t.Fatal(n, err)
	}
	if _, err := Check(k.Key); !errors.Is(err, errUnknown) {
		t.Fatal(err)
	}
}

func TestIssueKeyRefusesBadTTL(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	for _, ttl := range []time.Duration{0, -time.Hour, time.Second, MaxTTL + time.Hour} {
		if _, err := IssueKey(Issue{TTL: ttl}); err == nil {
			t.Error("issued with ttl", ttl)
		}
	}
	if _, err := IssueKey(Issue{TTL: time.Hour, Tokens: -1}); err == nil {
		t.Error("issued with a negative limit")
	}
}

// Never let an issued key outlive or exceed its issuer.
func TestIssuedKeyBoundedByIssuer(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	advance := clockAt(t, time.Date(2026, 10, 31, 9, 0, 0, 0, time.UTC))
	pid, psecret := addKey(t, "Orchestrator")
	if _, err := IssueKey(Issue{TTL: time.Hour, Issuer: pid}); err == nil {
		t.Fatal("a key not let issue issued one")
	}
	if _, err := Update("models-key", Change{Key: pid, Models: []string{"openai/*"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := Update("issuer-key", Change{Key: pid, Issues: true}); err != nil {
		t.Fatal(err)
	}
	if who, ok := Authenticate(psecret); !ok || !who.Issues {
		t.Fatal(who, ok)
	}
	if _, err := IssueKey(Issue{TTL: time.Hour, Issuer: pid, Models: []string{"anthropic/claude-opus-4"}}); err == nil {
		t.Fatal("issued a model the issuer may not use")
	}
	if _, err := IssueKey(Issue{TTL: time.Hour, Issuer: pid, Models: []string{"*"}}); err == nil {
		t.Fatal("issued every model under a key held to openai")
	}
	// none named: the issuer's
	all, err := IssueKey(Issue{TTL: time.Hour, Issuer: pid})
	if err != nil {
		t.Fatal(err)
	}
	if who, ok := Authenticate(all.Key); !ok || !slices.Equal(who.Models, []string{"openai/*"}) || who.IssuerID() != pid || who.Issues {
		t.Fatal(who, ok)
	}
	one, err := IssueKey(Issue{TTL: time.Hour, Issuer: pid, Models: []string{"openai/gpt-5"}})
	if err != nil {
		t.Fatal(err)
	}
	// an issued key never issues
	if _, err := Update("issuer-key", Change{Key: one.ID, Issues: true}); err == nil {
		t.Fatal("an issued key was let issue")
	}
	// widening the child after it was issued doesn't widen what it may use
	if _, err := Update("models-key", Change{Key: one.ID, Models: []string{"anthropic/*", "openai/gpt-5"}}); err != nil {
		t.Fatal(err)
	}
	if who, ok := Authenticate(one.Key); !ok || !slices.Equal(who.Models, []string{"openai/gpt-5"}) {
		t.Fatal(who, ok)
	}
	// narrowing the issuer reaches its keys at once
	if _, err := Update("models-key", Change{Key: pid, Models: []string{"openai/gpt-5-mini"}}); err != nil {
		t.Fatal(err)
	}
	var refused *Refused
	if _, err := Check(one.Key); !errors.As(err, &refused) {
		t.Fatal("a key its issuer no longer covers was let in:", err)
	}
	if _, err := Update("models-key", Change{Key: pid, Models: []string{"openai/*"}}); err != nil {
		t.Fatal(err)
	}
	// read-only and turned-off issuers hold their keys too
	if _, err := Update("access-key", Change{Key: pid, ReadOnly: true}); err != nil {
		t.Fatal(err)
	}
	if who, ok := Authenticate(one.Key); !ok || !who.ReadOnly {
		t.Fatal("a read-only issuer's key may use models:", who, ok)
	}
	if _, err := Update("access-key", Change{Key: pid, ReadOnly: false}); err != nil {
		t.Fatal(err)
	}
	if _, err := Update("off-key", Change{Key: pid}); err != nil {
		t.Fatal(err)
	}
	if _, err := Check(one.Key); !errors.As(err, &refused) {
		t.Fatal("a turned-off issuer's key was let in:", err)
	}
	if _, err := IssueKey(Issue{TTL: time.Hour, Issuer: pid}); err == nil {
		t.Fatal("a turned-off key issued one")
	}
	if _, err := Update("on-key", Change{Key: pid}); err != nil {
		t.Fatal(err)
	}
	if _, ok := Authenticate(one.Key); !ok {
		t.Fatal("not let in again")
	}
	// removed: its keys are refused, then pruned
	if _, err := Update("remove-key", Change{Key: pid}); err != nil {
		t.Fatal(err)
	}
	if _, err := Check(one.Key); !errors.As(err, &refused) {
		t.Fatal("a removed issuer's key was let in:", err)
	}
	advance(time.Minute)
	if n, err := Prune(); n != 2 || err != nil {
		t.Fatal(n, err)
	}
}

// An issued key under an expiring issuer expires with it.
func TestIssuedKeyNeverOutlivesIssuer(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	clockAt(t, time.Date(2026, 10, 31, 9, 0, 0, 0, time.UTC))
	pid, _ := addKey(t, "Orchestrator")
	if _, err := Update("issuer-key", Change{Key: pid, Issues: true}); err != nil {
		t.Fatal(err)
	}
	// the user's own expiring key (made from the command line or the
	// store) can't be let issue
	short, err := IssueKey(Issue{TTL: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Update("issuer-key", Change{Key: short.ID, Issues: true}); err == nil {
		t.Fatal("an expiring key was let issue")
	}
	// clamp: an issuer that itself expires, set in the store by hand
	keys, _ := load()
	for i := range keys {
		if keys[i].ID == pid {
			keys[i].Expires = Now().Add(30 * time.Minute)
		}
	}
	if err := save(keys); err != nil {
		t.Fatal(err)
	}
	k, err := IssueKey(Issue{TTL: 2 * time.Hour, Issuer: pid})
	if err != nil {
		t.Fatal(err)
	}
	if !k.Expires.Equal(Now().Add(30 * time.Minute)) {
		t.Fatal("outlives its issuer:", k.Expires)
	}
}

// Tokens and Cost cap an issued key over its whole life, not per day.
func TestIssuedKeyLifeLimit(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	clockAt(t, time.Date(2026, 10, 31, 9, 0, 0, 0, time.UTC))
	k, err := IssueKey(Issue{TTL: 90 * time.Minute, Tokens: 100000, Cost: 2})
	if err != nil {
		t.Fatal(err)
	}
	if k.Limit == nil || k.Limit.Period != "hours" || k.Limit.Hours != 2 || k.Limit.Tokens != 100000 || k.Limit.Cost != 2 || !k.Limit.Since.Equal(Now()) {
		t.Fatal(k.Limit)
	}
	who, ok := Authenticate(k.Key)
	if !ok || !who.Limited() {
		t.Fatal(who, ok)
	}
}

// An issuer may revoke only the keys issued under it; the user any
// issued key, and no key of their own through RevokeIssued.
func TestRevokeIssued(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	clockAt(t, time.Date(2026, 10, 31, 9, 0, 0, 0, time.UTC))
	a, _ := addKey(t, "A")
	b, _ := addKey(t, "B")
	for _, id := range []string{a, b} {
		if _, err := Update("issuer-key", Change{Key: id, Issues: true}); err != nil {
			t.Fatal(err)
		}
	}
	ka, err := IssueKey(Issue{TTL: time.Hour, Issuer: a})
	if err != nil {
		t.Fatal(err)
	}
	if err := RevokeIssued(ka.ID, b); err == nil {
		t.Fatal("B revoked A's key")
	}
	if got, _ := IssuedBy(b); len(got) != 0 {
		t.Fatal("B lists A's keys:", got)
	}
	if got, _ := IssuedBy(a); len(got) != 1 || got[0].ID != ka.ID || got[0].Secret != "" {
		t.Fatal(got)
	}
	if err := RevokeIssued(b, ""); err == nil {
		t.Fatal("revoked a key of the user's own")
	}
	if err := RevokeIssued(ka.ID, a); err != nil {
		t.Fatal(err)
	}
	if _, ok := Authenticate(ka.Key); ok {
		t.Fatal("a revoked key was let in")
	}
}

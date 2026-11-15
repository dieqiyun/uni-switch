package gateway

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/catalog"
	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// readCodexUsage stands in for ChatGPT's usage readings: the five hours
// of me@example.com, at what used says now, back at resets. The stand-in
// is asked for every agent; only codex's is filled.
func readCodexUsage(t *testing.T, used func() float64, resets time.Time) {
	t.Helper()
	provider.LoginUsageVia(func(_ context.Context, agent string) map[string]provider.SubscriptionQuota {
		if agent != "codex" {
			return map[string]provider.SubscriptionQuota{}
		}
		return map[string]provider.SubscriptionQuota{"me@example.com": {Provider: agent, Windows: []provider.QuotaWindow{
			{Name: "5 hours", Used: used(), Span: 5 * time.Hour, ResetsAt: &resets},
		}}}
	})
	t.Cleanup(func() { provider.LoginUsageVia(nil) })
}

// waitUsed has codex's windows read again and waits till they say used.
func waitUsed(t *testing.T, used float64) {
	t.Helper()
	provider.StaleAllowance("codex", "me@example.com")
	for deadline := time.Now().Add(10 * time.Second); ; {
		a := provider.Allowances("codex")["me@example.com"]
		if len(a) > 0 && a[0].Used == used {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("the reading never said %v%%: %+v", used, a)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// renewedTold is the channel the next telling of renewed for codex's
// me@example.com goes to. The gateway's own hook was registered first
// (init), and hooks are told in turn, so by the time this one is told
// the gateway's lifting has run.
var renewedTold struct {
	once sync.Once
	ch   atomic.Pointer[chan struct{}]
}

// armRenewed has the next renewed telling for codex's me@example.com
// signalled, and returns a wait for it.
func armRenewed(t *testing.T) func() {
	t.Helper()
	renewedTold.once.Do(func() {
		provider.OnRenewed(func(agent, user string) {
			if agent != "codex" || user != "me@example.com" {
				return
			}
			if ch := renewedTold.ch.Load(); ch != nil {
				select {
				case *ch <- struct{}{}:
				default:
				}
			}
		})
	})
	ch := make(chan struct{}, 1)
	renewedTold.ch.Store(&ch)
	t.Cleanup(func() { renewedTold.ch.Store(nil) })
	return func() {
		t.Helper()
		select {
		case <-ch:
		case <-time.After(10 * time.Second):
			t.Fatal("the reading that found the window less than full told no renewal")
		}
	}
}

// restByRefusal rests codex's account as restAfterMarked does a refusal
// that names its reset: out of quota, by "resets", till resets.
func restByRefusal(t *testing.T, id string, resets time.Time) {
	t.Helper()
	restingUntil.Lock()
	restingUntil.m[id], restingUntil.note[id] = resets, Rest{
		Why: failQuota, Status: 429, Until: resets, By: "resets", Key: id,
		agent: "codex", user: "me@example.com",
	}
	restingUntil.Unlock()
	t.Cleanup(func() {
		restingUntil.Lock()
		delete(restingUntil.m, id)
		delete(restingUntil.note, id)
		restingUntil.Unlock()
	})
}

// A refusal that names when its quota comes back (by "resets") rests the
// account till then, and only till then: a usage reading that merely
// finds the window less than full — the vendor's usage can say so while
// its own refusal stands — does not lift it (#2718). Lifted, the account
// was tried first in its group and refused again every few minutes, all
// of them saying the same reset, with nothing gained by one.
func TestReadingKeepsARefusalsRest(t *testing.T) {
	codexSignedIn(t)
	p, err := provider.Find("codex")
	if err != nil {
		t.Fatal(err)
	}
	id := candidate{p: *p, model: "gpt-6.1-sol"}.restKey()
	resets := time.Now().Add(92 * time.Minute).Truncate(time.Second)
	used := 100.0
	readCodexUsage(t, func() float64 { return used }, resets)
	waitUsed(t, 100)
	restByRefusal(t, id, resets)
	told := armRenewed(t)
	used = 80
	waitUsed(t, 80)
	told()
	if _, ok := restOf(id); !ok {
		t.Fatal("a reading that found the window less than full lifted the refusal's rest, due at", resets.Format(time.TimeOnly))
	}
}

// AutoVision, left to pick a describer, keeps off an account the vendor
// refused till a reset — and a usage reading that finds its window less
// than full, while the refusal stands, does not bring it back (#2718):
// lifted, the pick went to the refused account, and every image's
// description was refused with it, one after another.
func TestAutoVisionKeepsOffARefusedAccount(t *testing.T) {
	codexSignedIn(t)
	if err := provider.Save(provider.Provider{ID: "keyed", Name: "keyed", Chat: "http://keyed.invalid/v1", Key: "k", Models: []string{"eye"}}); err != nil {
		t.Fatal(err)
	}
	if err := catalog.SaveLive("keyed", "http://keyed.invalid/v1", []catalog.Model{{ID: "eye", Images: true, ImageInput: imageInputBool(true)}}); err != nil {
		t.Fatal(err)
	}
	if err := catalog.SaveLive("codex", provider.CodexBase, []catalog.Model{{ID: "gpt-6.1-sol", Images: true, ImageInput: imageInputBool(true)}}); err != nil {
		t.Fatal(err)
	}
	capUsage(t, map[string]float64{"me@example.com": 80})
	p, err := provider.Find("codex")
	if err != nil {
		t.Fatal(err)
	}
	resets := time.Now().Add(92 * time.Minute).Truncate(time.Second)
	used := 100.0
	readCodexUsage(t, func() float64 { return used }, resets)
	waitUsed(t, 100)
	restByRefusal(t, candidate{p: *p, model: "gpt-6.1-sol"}.restKey(), resets)
	if m := AutoVision(); m != "keyed/eye" {
		t.Fatalf("AutoVision = %q with codex refused till %s", m, resets.Format(time.TimeOnly))
	}
	told := armRenewed(t)
	used = 80
	waitUsed(t, 80)
	told()
	if m := AutoVision(); m != "keyed/eye" {
		t.Fatalf("AutoVision = %q, back on the refused account, before its reset", m)
	}
}

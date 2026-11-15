package provider

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/catalog"
)

// #1907 (foobra): Codex Desktop and the CLI on their own ChatGPT
// subscription, two accounts, no gateway in between. Switch-only is kept on
// Codex's subscription; with it on, the account Codex is signed in to
// running out still signs Codex in to the next one with room, in its own
// auth.json, and stays there till that one is spent too: the sign-in is
// what spends the accounts, so going back at the first's reset would
// leave the next part used (returnsToFirst). A providers.json
// that can't be read
// says switch-only: the user's choice is unknown, and failover through the
// gateway, uni-switch's own idea, isn't taken up on it.
func TestCodexSwitchOnlyStillSwitchesTheSignIn(t *testing.T) {
	home := signIn(t) // me@example.com, acct-1
	rememberLogins(true)
	codexSignIn(t, home, "spare@example.com", "r-spare")
	rememberLogins(true)
	codexSignIn(t, home, "me@example.com", "r-1")
	rememberLogins(true)
	used := map[string]float64{"acct-me@example.com": 100, "acct-spare@example.com": 20}
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"plan_type": "plus", "rate_limit": map[string]any{
			"primary_window": map[string]any{"used_percent": used[r.Header.Get("chatgpt-account-id")], "limit_window_seconds": 18000}}})
	}))
	defer fake.Close()
	old := CodexBase
	CodexBase = fake.URL + "/backend-api/codex"
	t.Cleanup(func() { CodexBase = old })
	switched := func() string {
		t.Helper()
		loginUsageCache.Lock()
		loginUsageCache.m = nil
		loginUsageCache.Unlock()
		to, err := SwitchWhenSpent(context.Background(), "codex")
		if err != nil {
			t.Fatal(err)
		}
		return to
	}
	signedIn := func() string {
		t.Helper()
		var live codexAuth
		readJSON(filepath.Join(home, ".codex", "auth.json"), &live)
		return live.Tokens.RefreshToken
	}
	if err := SetLoginOn("codex", "spare@example.com", true); err != nil {
		t.Fatal(err)
	}

	if SwitchOnly("codex") {
		t.Fatal("switch-only before the user set it")
	}
	if err := SetSwitchOnly("codex", true); err != nil {
		t.Fatal(err)
	}
	if !SwitchOnly("codex") {
		t.Fatal("switch-only not kept")
	}
	p, err := Find("codex")
	if err != nil || !p.SwitchOnly {
		t.Fatalf("Find: %+v %v", p, err)
	}
	// out: on to the spare, in Codex's own auth.json
	if to := switched(); to != "spare@example.com" || signedIn() != "r-spare" {
		t.Fatalf("switched to %q, Codex signed in with %q", to, signedIn())
	}
	// the first has room again: Codex stays on the spare
	used["acct-me@example.com"] = 10
	if to := switched(); to != "" || signedIn() != "r-spare" {
		t.Fatalf("moved off the spare to %q, Codex signed in with %q", to, signedIn())
	}
	// the spare spent in turn: on to the first
	used["acct-spare@example.com"] = 99
	if to := switched(); !strings.HasPrefix(to, "me@example.com") || signedIn() != "r-1" {
		t.Fatalf("on to %q, Codex signed in with %q", to, signedIn())
	}
	if !SwitchOnly("codex") {
		t.Fatal("switch-only lost on a switch")
	}

	if err := SetSwitchOnly("codex", false); err != nil || SwitchOnly("codex") {
		t.Fatalf("off: %v, still %v", err, SwitchOnly("codex"))
	}
	// neither the file nor its last good generation reads
	for _, f := range []string{Path(), Path() + ".bak"} {
		if err := os.WriteFile(f, []byte("{not json"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if !SwitchOnly("codex") {
		t.Error("an unreadable providers.json read as switch-only off")
	}
}

// myduso on Discord: Claude's subscription kept to Claude Code alone.
// uni-switch still signs Claude Code in to the next account with room, by
// writing Claude Code's own sign-in, and serves nothing on the accounts:
// they leave the catalog, a request naming them is refused as switched
// off, and none of it is the raw Off a user's switch writes.
func TestClaudeSwitchOnlyServesNothingAndStillSwitches(t *testing.T) {
	home := claudeHome(t)
	cred := claudeSignIn(t, home, time.Now().Add(time.Hour)) // sk-ant-oat01-old
	writeFile(t, filepath.Join(home, ".claude.json"), map[string]any{"oauthAccount": map[string]any{"emailAddress": "a@example.com", "accountUuid": "u-a"}})
	rememberLogins(true)
	loginsMu.Lock()
	ls := upsertLogin(readLogins(), savedLogin{Agent: "claude", User: "b@example.com", Plan: "max", On: true, Seen: time.Now().UTC(),
		Auth: mustJSONRaw(t, map[string]any{"claudeAiOauth": map[string]any{"accessToken": "sk-ant-oat01-b", "refreshToken": "sk-ant-ort01-b",
			"expiresAt": time.Now().Add(time.Hour).UnixMilli(), "subscriptionType": "max", "scopes": []string{"user:inference", "user:profile"}}}),
		Profile: mustJSONRaw(t, map[string]any{"emailAddress": "b@example.com", "accountUuid": "u-b"})})
	if err := writeLogins(ls); err != nil {
		t.Fatal(err)
	}
	loginsMu.Unlock()
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("usage endpoint read: %s", r.URL)
	}))
	defer fake.Close()
	claudeBase = fake.URL // isolate puts it back
	os.MkdirAll(filepath.Dir(catalog.CachePath()), 0o755)
	if err := os.WriteFile(catalog.CachePath(), []byte(`{"anthropic": {"models": {
	  "claude-sonnet-5": {"id":"claude-sonnet-5","name":"Claude Sonnet 5","modalities":{"input":["text"],"output":["text"]}}}}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	catalog.Reset()
	t.Cleanup(catalog.Reset)
	if p, err := Find("claude"); err != nil {
		t.Fatal(err)
	} else if _, err := p.Fetch(context.Background()); err != nil {
		t.Fatal(err)
	}

	served := func() bool {
		t.Helper()
		for _, e := range Catalog() {
			if e.Provider.ID == "claude" {
				return true
			}
		}
		return false
	}
	p, err := Find("claude")
	if err != nil || !p.On() || p.KeptToAgent() || !served() {
		t.Fatalf("before: %+v %v, served %v", p, err, served())
	}
	if err := SetSwitchOnly("claude", true); err != nil {
		t.Fatal(err)
	}
	p, err = Find("claude")
	if err != nil || !p.SwitchOnly || !p.KeptToAgent() || p.On() || p.Off {
		t.Fatalf("kept to Claude Code: %+v %v", p, err)
	}
	if served() {
		t.Error("the catalog still serves the Claude accounts")
	}
	if _, _, ok := Resolve("claude/claude-sonnet-5"); ok {
		t.Error("claude/claude-sonnet-5 resolves kept to Claude Code")
	}
	if _, off := SwitchedOff("claude/claude-sonnet-5"); !off {
		t.Error("a request naming the Claude accounts isn't refused as switched off")
	}
	if leftAlone("claude") {
		t.Fatal("kept to Claude Code reads as left alone: no switching, no usage")
	}

	// spent: Claude Code's own sign-in goes on to b
	for user, u := range map[string]float64{"a@example.com": 99, "b@example.com": 10} {
		NoteClaudeLimits(user, []ClaudeLimit{{Kind: "five_hour", Used: u / 100, ResetsAt: time.Now().Add(time.Hour).Unix()}})
	}
	to, err := SwitchWhenSpent(context.Background(), "claude")
	if err != nil || to != "b@example.com" {
		t.Fatalf("switched to %q: %v", to, err)
	}
	var c map[string]any
	readJSON(cred, &c)
	if o, _ := c["claudeAiOauth"].(map[string]any); o["refreshToken"] != "sk-ant-ort01-b" {
		t.Fatalf("Claude Code is signed in with %v", o)
	}
	if p, _ := Find("claude"); !p.KeptToAgent() {
		t.Fatal("kept to Claude Code lost on a switch")
	}

	if err := SetSwitchOnly("claude", false); err != nil {
		t.Fatal(err)
	}
	if p, _ := Find("claude"); !p.On() || !served() {
		t.Fatalf("off again: %+v, served %v", p, served())
	}
	if _, off := SwitchedOff("claude/claude-sonnet-5"); off {
		t.Error("still refused as switched off")
	}
	if _, _, ok := Resolve("claude/claude-sonnet-5"); !ok {
		t.Error("claude/claude-sonnet-5 doesn't resolve unticked")
	}
}

// #1907 (foobra, v0.1.1224 on): "Keep Codex signed in to [the first
// account]" and "Only switch Codex's sign-in" both ticked, the account Codex
// was on at 0% of its five hours left, the other at 100%: Codex stayed on
// the spent one through a restart. Kept on one account, nothing ever moved
// the sign-in, and switch-only sent nothing through the gateway either.
// Switch-only asks for the sign-in to move, so it does, whether kept on the
// first or on an account of the user's choosing; the choice is kept, and
// holds again once switch-only is let go.
func TestSwitchOnlyMovesASignInKeptOnOne(t *testing.T) {
	for _, chosen := range []bool{false, true} {
		t.Run(fmt.Sprint("kept on one chosen ", chosen), func(t *testing.T) {
			home := signIn(t) // me@example.com, acct-1
			rememberLogins(true)
			codexSignIn(t, home, "spare@example.com", "r-spare")
			rememberLogins(true)
			codexSignIn(t, home, "me@example.com", "r-1")
			rememberLogins(true)
			used := map[string]float64{"acct-me@example.com": 100, "acct-spare@example.com": 0}
			fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				json.NewEncoder(w).Encode(map[string]any{"plan_type": "plus", "rate_limit": map[string]any{
					"primary_window": map[string]any{"used_percent": used[r.Header.Get("chatgpt-account-id")], "limit_window_seconds": 18000}}})
			}))
			defer fake.Close()
			old := CodexBase
			CodexBase = fake.URL + "/backend-api/codex"
			t.Cleanup(func() { CodexBase = old })
			switched := func() string {
				t.Helper()
				loginUsageCache.Lock()
				loginUsageCache.m = nil
				loginUsageCache.Unlock()
				to, err := SwitchWhenSpent(context.Background(), "codex")
				if err != nil {
					t.Fatal(err)
				}
				return to
			}
			signedIn := func() string {
				t.Helper()
				var live codexAuth
				readJSON(filepath.Join(home, ".codex", "auth.json"), &live)
				return live.Tokens.RefreshToken
			}
			if err := SetLoginOn("codex", "spare@example.com", true); err != nil {
				t.Fatal(err)
			}
			// the one Codex is signed in to, by the name uni-switch gives it
			as := ""
			for _, l := range Logins("codex") {
				if l.Active && chosen {
					as = l.User
				}
			}
			if !chosen {
				if err := SetKeepLogin("codex", true); err != nil {
					t.Fatal(err)
				}
			} else if err := SetKeepLoginAs("codex", as); err != nil {
				t.Fatal(err)
			}
			if to := switched(); to != "" || signedIn() != "r-1" {
				t.Fatalf("kept, without switch-only: moved to %q", to)
			}
			if err := SetSwitchOnly("codex", true); err != nil {
				t.Fatal(err)
			}
			if to := switched(); to != "spare@example.com" || signedIn() != "r-spare" {
				t.Fatalf("switch-only, kept on %q: switched to %q, Codex signed in with %q", as, to, signedIn())
			}
			used["acct-me@example.com"] = 10
			if to := switched(); !strings.HasPrefix(to, "me@example.com") || signedIn() != "r-1" {
				t.Fatalf("back to %q, Codex signed in with %q", to, signedIn())
			}
			// the user's choice is still theirs
			if p, _ := Find("codex"); !p.KeepLogin || p.KeepLoginAs != as {
				t.Fatalf("keep login lost: %v %q", p.KeepLogin, p.KeepLoginAs)
			}
			// it can't be ticked anew under switch-only, where it would
			// do nothing
			if err := SetKeepLogin("codex", true); err == nil {
				t.Error("kept on the first under switch-only")
			}
			if err := SetKeepLoginAs("codex", "spare@example.com"); err == nil {
				t.Error("kept on spare under switch-only")
			}
			// let go of switch-only, the kept account holds again
			used["acct-me@example.com"] = 100
			if err := SetSwitchOnly("codex", false); err != nil {
				t.Fatal(err)
			}
			if to := switched(); to != "" || signedIn() != "r-1" {
				t.Fatalf("kept again: moved to %q, Codex signed in with %q", to, signedIn())
			}
		})
	}
}

// The same for Claude Code: its accounts kept to its own sign-in
// (switch-only) and Claude Code kept signed in to the first, the first
// spent moves it to the next.
func TestClaudeSwitchOnlyMovesASignInKeptOnOne(t *testing.T) {
	home := claudeHome(t)
	cred := claudeSignIn(t, home, time.Now().Add(time.Hour))
	writeFile(t, filepath.Join(home, ".claude.json"), map[string]any{"oauthAccount": map[string]any{"emailAddress": "a@example.com", "accountUuid": "u-a"}})
	rememberLogins(true)
	loginsMu.Lock()
	ls := upsertLogin(readLogins(), savedLogin{Agent: "claude", User: "b@example.com", Plan: "max", On: true, Seen: time.Now().UTC(),
		Auth: mustJSONRaw(t, map[string]any{"claudeAiOauth": map[string]any{"accessToken": "sk-ant-oat01-b", "refreshToken": "sk-ant-ort01-b",
			"expiresAt": time.Now().Add(time.Hour).UnixMilli(), "subscriptionType": "max", "scopes": []string{"user:inference", "user:profile"}}}),
		Profile: mustJSONRaw(t, map[string]any{"emailAddress": "b@example.com", "accountUuid": "u-b"})})
	if err := writeLogins(ls); err != nil {
		t.Fatal(err)
	}
	loginsMu.Unlock()
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("usage endpoint read: %s", r.URL)
	}))
	defer fake.Close()
	claudeBase = fake.URL // isolate puts it back
	for user, u := range map[string]float64{"a@example.com": 100, "b@example.com": 10} {
		NoteClaudeLimits(user, []ClaudeLimit{{Kind: "five_hour", Used: u / 100, ResetsAt: time.Now().Add(time.Hour).Unix()}})
	}
	if err := SetKeepLogin("claude", true); err != nil {
		t.Fatal(err)
	}
	if to, err := SwitchWhenSpent(context.Background(), "claude"); err != nil || to != "" {
		t.Fatalf("kept, without switch-only: moved to %q: %v", to, err)
	}
	if err := SetSwitchOnly("claude", true); err != nil {
		t.Fatal(err)
	}
	to, err := SwitchWhenSpent(context.Background(), "claude")
	if err != nil || to != "b@example.com" {
		t.Fatalf("switch-only, kept on the first: switched to %q: %v", to, err)
	}
	var c map[string]any
	readJSON(cred, &c)
	if o, _ := c["claudeAiOauth"].(map[string]any); o["refreshToken"] != "sk-ant-ort01-b" {
		t.Fatalf("Claude Code is signed in with %v", o)
	}
}

// cn_Vincentzyx on X: Claude Code not connected to uni-switch, only its
// sign-in switched, the accounts in the order Make first gave them. The
// account Claude Code was on ran out and it stayed there: no request of a
// saved account passes uni-switch then, so one Claude Code wasn't signed in
// to since had no reading, and the switch counted no account known to
// have room. A saved account with no reading is read by Claude Code's own
// /usage, in its own directory, in order and only till one has room, and
// one read isn't read again on the next look.
func TestClaudeSwitchOnlyReadsUnreadSpares(t *testing.T) {
	claudeThree(t) // pro@x.com signed in; team and max saved
	var text atomic.Value
	text.Store("Current session: 100% used")
	runs := fakeClaudeUsage(t, &text, nil)
	forget := func() {
		lastQuotas.Lock()
		lastQuotas.m, lastQuotas.loaded = nil, false
		lastQuotas.Unlock()
		loginUsageCache.Lock()
		loginUsageCache.m = nil
		loginUsageCache.Unlock()
	}
	forget()
	t.Cleanup(forget)
	if err := SetSwitchOnly("claude", true); err != nil {
		t.Fatal(err)
	}
	var spares []string
	for _, l := range Logins("claude") {
		if !l.Active {
			if err := SetLoginOn("claude", l.User, true); err != nil {
				t.Fatal(err)
			}
			spares = append(spares, l.User)
		}
	}
	if len(spares) != 2 {
		t.Fatalf("spares: %v", spares)
	}
	// the first in line can't be read now (Claude Code failed), the
	// second has room
	used := map[string]string{"": "100", claudeAccountDir(spares[1]): "10"}
	ran := map[string]int{}
	var mu sync.Mutex
	UsageClaudeVia(func(_ context.Context, dir string) (string, error) {
		runs.Add(1)
		mu.Lock()
		ran[dir]++
		mu.Unlock()
		if used[dir] == "" {
			return "", errors.New("Claude Code: network is unreachable")
		}
		return "Current session: " + used[dir] + "% used", nil
	})
	for _, u := range spares {
		if q := LoginUsage(context.Background(), "claude")[u]; q.Error != errClaudeSaved.Error() {
			t.Fatalf("%s read before the switch looked: %+v", u, q)
		}
	}
	from, to, back, ok := NextLogin(context.Background(), "claude")
	if !ok || back || to != spares[1] || !strings.HasPrefix(from, "pro@x.com") {
		t.Fatalf("got %q→%q back=%v ok=%v, want on to %q", from, to, back, ok, spares[1])
	}
	mu.Lock()
	if ran[""] != 1 || ran[claudeAccountDir(spares[0])] != 1 || ran[claudeAccountDir(spares[1])] != 1 {
		t.Fatalf("/usage runs by directory: %v", ran)
	}
	mu.Unlock()
	// the next look, a few minutes on (the minute's cache gone, past the
	// 30s floor): the failed one isn't run again before its wait, the
	// one read isn't run again at all
	look := func() {
		t.Helper()
		loginUsageCache.Lock()
		loginUsageCache.m = nil
		loginUsageCache.Unlock()
		claudeUsage.Lock()
		for k, e := range claudeUsage.m {
			e.tried = e.tried.Add(-3 * time.Minute)
			claudeUsage.m[k] = e
		}
		claudeUsage.Unlock()
		if _, to, _, ok := NextLogin(context.Background(), "claude"); !ok || to != spares[1] {
			t.Fatalf("next look: to %q ok=%v", to, ok)
		}
	}
	mu.Lock()
	failed, read := ran[claudeAccountDir(spares[0])], ran[claudeAccountDir(spares[1])]
	mu.Unlock()
	look()
	mu.Lock()
	if ran[claudeAccountDir(spares[0])] != failed || ran[claudeAccountDir(spares[1])] != read {
		t.Fatalf("a spare run again on the next look: %v", ran)
	}
	mu.Unlock()
	// past its wait (usageTestWait), the one that failed is tried again
	look()
	look()
	mu.Lock()
	if ran[claudeAccountDir(spares[0])] != failed+1 || ran[claudeAccountDir(spares[1])] != read {
		t.Fatalf("after the wait: %v", ran)
	}
	mu.Unlock()
}

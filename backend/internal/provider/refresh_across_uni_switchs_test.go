package provider

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/filelock"
)

// anotherUniSwitchRefreshing holds lock as another uni-switch mid-refresh would
// (a restart handing the gateway over runs two at once, xuanwo on X),
// runs refresh, gives it the time it takes to get past an in-process
// lock, then has the other uni-switch's refresh land and let go. What
// refresh returned.
func anotherUniSwitchRefreshing[T any](t *testing.T, lock string, refresh func() T, lands func()) T {
	t.Helper()
	unlock, held := filelock.Lock(lock, time.Second)
	if !held {
		t.Fatal("the lock wasn't to be had")
	}
	got := make(chan T, 1)
	go func() { got <- refresh() }()
	select {
	case v := <-got: // refreshed alongside the other uni-switch
		unlock()
		return v
	case <-time.After(300 * time.Millisecond):
	}
	lands()
	unlock()
	select {
	case v := <-got:
		return v
	case <-time.After(15 * time.Second):
		t.Fatal("never refreshed")
	}
	panic("unreachable")
}

// A Codex sign-in another uni-switch is refreshing is waited for, and its
// rotated tokens used, rather than refreshed again with the refresh token
// it spent.
func TestCodexRefreshWaitsForAnotherUniSwitch(t *testing.T) {
	isolate(t)
	var refreshes atomic.Int32
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		refreshes.Add(1)
		json.NewEncoder(w).Encode(map[string]any{
			"access_token":  fakeJWT(map[string]any{"exp": float64(time.Now().Add(time.Hour).Unix())}),
			"refresh_token": "r-reused"})
	}))
	t.Cleanup(fake.Close)
	old := codexTokenURL
	codexTokenURL = fake.URL
	t.Cleanup(func() { codexTokenURL = old })

	path, _ := dueCodexAuth(t)
	theirs := fakeJWT(map[string]any{"exp": float64(time.Now().Add(time.Hour).Unix()), "by": "the other uni-switch"})
	type answer struct {
		tok string
		err error
	}
	got := anotherUniSwitchRefreshing(t, loginsPath()+".codex.lock", func() answer {
		tok, _, err := codexToken(context.Background(), filepath.Dir(path))
		return answer{tok, err}
	}, func() {
		writeFile(t, path, map[string]any{"tokens": map[string]any{
			"access_token": theirs, "refresh_token": "r-rotated", "account_id": "acct-1"}})
	})
	if got.err != nil || got.tok != theirs || refreshes.Load() != 0 {
		t.Fatalf("refreshed %d times beside another uni-switch: %q, %v", refreshes.Load(), got.tok, got.err)
	}
}

// The same for a ChatGPT API account (siwcFresh).
func TestChatGPTAPIRefreshWaitsForAnotherUniSwitch(t *testing.T) {
	claudeHome(t)
	f := newFakeOpenAI(t)
	if st := signInVia(t, f, "oaiapp_9", same); st.State != "done" {
		t.Fatalf("sign-in %+v", st)
	}
	user := "me@example.com"
	set := func(edit func(*siwcCreds)) {
		if err := editSideLogin(ChatGPTAPIID, user, func(ls []savedLogin, i int) ([]savedLogin, error) {
			c, _ := siwcSaved(ls[i])
			edit(&c)
			ls[i].Auth, _ = json.Marshal(c)
			return ls, nil
		}); err != nil {
			t.Fatal(err)
		}
	}
	set(func(c *siwcCreds) { c.Expires = time.Now().Add(time.Minute) })
	f.mu.Lock()
	n := len(f.forms)
	f.mu.Unlock()
	type answer struct {
		c   siwcCreds
		err error
	}
	got := anotherUniSwitchRefreshing(t, loginsPath()+".siwc.lock", func() answer {
		c, err := siwcFresh(context.Background(), user, false)
		return answer{c, err}
	}, func() {
		set(func(c *siwcCreds) {
			c.Access, c.Refresh, c.Expires = "at-theirs", "rt-theirs", time.Now().Add(time.Hour)
		})
	})
	f.mu.Lock()
	sent := len(f.forms) - n
	f.mu.Unlock()
	if got.err != nil || got.c.Access != "at-theirs" || sent != 0 {
		t.Fatalf("refreshed %d times beside another uni-switch: %+v, %v", sent, got.c, got.err)
	}
}

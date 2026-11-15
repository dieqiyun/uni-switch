package provider

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

// An account signed in again is read afresh: the usage reading kept from
// the sign-in the vendor refused doesn't answer for the new one, saying it
// is refused for up to a minute after it was renewed. 8c0ffcc6 did this
// for a plugin's accounts; these are the built-ins'.

// Codex, signed in again in the browser (addLogin, as Claude's sign-in and
// an import keep theirs).
func TestCodexSignInAgainReadsUsageAfresh(t *testing.T) {
	claudeHome(t)
	resetLoginUsage(t)
	var status atomic.Int32
	status.Store(http.StatusUnauthorized)
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/backend-api/wham/usage" {
			if s := int(status.Load()); s != http.StatusOK {
				w.WriteHeader(s)
				return
			}
			io.WriteString(w, `{"plan_type":"plus","rate_limit":{"allowed":true,"primary_window":{"used_percent":12,"limit_window_seconds":18000,"reset_after_seconds":600}}}`)
			return
		}
		json.NewEncoder(w).Encode(map[string]any{
			"id_token": fakeJWT(map[string]any{"email": "cx@example.com",
				"https://api.openai.com/auth": map[string]any{"chatgpt_plan_type": "plus", "chatgpt_account_id": "acct-cx"}}),
			"access_token":  fakeJWT(map[string]any{"exp": float64(time.Now().Add(time.Hour).Unix())}),
			"refresh_token": "r-cx",
		})
	}))
	defer fake.Close()
	oldTok, oldAddr, oldBase := codexTokenURL, codexCallbackAddr, CodexBase
	t.Cleanup(func() { codexTokenURL, codexCallbackAddr, CodexBase = oldTok, oldAddr, oldBase })
	codexTokenURL, CodexBase = fake.URL, fake.URL+"/backend-api/codex"
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	codexCallbackAddr = ln.Addr().String()
	ln.Close()

	login := func() {
		t.Helper()
		st, err := StartSignIn("codex")
		if err != nil {
			t.Fatal(err)
		}
		finishInBrowser(t, st, "cx")
		if st = waitDone(t, st.ID); st.State != "done" {
			t.Fatalf("state %+v", st)
		}
	}
	login()
	if q := LoginUsage(context.Background(), "codex")["cx@example.com"]; q.Error == "" {
		t.Fatalf("the vendor's 401 read clean: %+v", q)
	}
	status.Store(http.StatusOK)
	time.Sleep(1200 * time.Millisecond) // the callback port is let go
	login()
	if q := LoginUsage(context.Background(), "codex")["cx@example.com"]; q.Error != "" || len(q.Windows) == 0 {
		t.Fatalf("signed in again, but the usage is the old sign-in's: %+v", q)
	}
}

func resetLoginUsage(t *testing.T) {
	clear := func() {
		loginUsageCache.Lock()
		loginUsageCache.m, loginUsageCache.pending = nil, nil
		loginUsageCache.Unlock()
	}
	clear()
	t.Cleanup(clear)
}

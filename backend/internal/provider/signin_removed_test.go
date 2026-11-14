package provider

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"

	"sync"
	"testing"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/catalog"
)

// A sign-in canceled while its account is shown again, as the window still
// offers Cancel until it reads as done, ends done all the same: the account
// was signed in to and saved already, and a sign-in records one outcome.
func TestSignInCanceledWhileTheAccountComesBack(t *testing.T) {
	fakeCodexSignIn(t)
	if err := Delete("codex"); err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	id, canceled := "", false
	was := catalog.Changed
	catalog.Changed = func() {
		mu.Lock()
		defer mu.Unlock()
		if id != "" && !canceled && !hiddenAccount("codex") {
			canceled = true
			CancelSignIn(id)
		}
	}
	t.Cleanup(func() { catalog.Changed = was })

	st, err := StartSignIn("codex")
	if err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	id = st.ID
	mu.Unlock()
	finishInBrowser(t, st, "code")
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	if st, err := WaitSignIn(ctx, st.ID); err != nil || st.State != "done" || st.User != "me@example.com" {
		t.Fatalf("sign-in %+v, %v", st, err)
	}
	mu.Lock()
	shown := canceled
	mu.Unlock()
	if !shown {
		t.Fatal("signing in didn't show the removed account again")
	}
	if _, ok := find(All(), "codex"); !ok {
		t.Fatal("the account isn't listed after its sign-in")
	}
}

// fakeCodexSignIn signs in to Codex (me@example.com, personal Pro,
// workspace acct-1) and points its sign-in at a token server that signs in
// to that account again, coming back to a free port.
func fakeCodexSignIn(t *testing.T) {
	t.Helper()
	signIn(t)
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"id_token": fakeJWT(map[string]any{"email": "me@example.com",
				"https://api.openai.com/auth": map[string]any{"chatgpt_plan_type": "pro", "chatgpt_account_id": "acct-1"}}),
			"access_token":  fakeJWT(map[string]any{"exp": float64(time.Now().Add(time.Hour).Unix())}),
			"refresh_token": "r2",
		})
	}))
	t.Cleanup(fake.Close)
	oldTok, oldAddr := codexTokenURL, codexCallbackAddr
	t.Cleanup(func() { codexTokenURL, codexCallbackAddr = oldTok, oldAddr })
	codexTokenURL = fake.URL
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	codexCallbackAddr = ln.Addr().String()
	ln.Close()
}

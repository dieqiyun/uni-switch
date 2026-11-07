package accounts

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestMain(m *testing.M) {
	if os.Getenv("UNI_SWITCH_ACCOUNT_HELPER") == "1" {
		if len(os.Args) > 2 && os.Args[1] == "auth" && os.Args[2] == "status" {
			fmt.Print(`{"loggedIn":true,"email":"synthetic@example.invalid"}`)
			os.Exit(0)
		}
		if len(os.Args) > 2 && os.Args[1] == "auth" && os.Args[2] == "login" {
			if os.Getenv("ANTHROPIC_API_KEY") != "" || os.Getenv("CLAUDE_CONFIG_DIR") == "" {
				os.Exit(2)
			}
			fmt.Println("https://claude.ai/oauth/authorize?synthetic=1")
			var code string
			_, _ = fmt.Scanln(&code)
			if code != "synthetic-code" {
				os.Exit(3)
			}
			os.Exit(0)
		}
		os.Exit(4)
	}
	os.Exit(m.Run())
}
func waitLogin(t *testing.T, m *Manager, id string, want string) Login {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		got, e := m.LoginStatus(id)
		if e != nil {
			t.Fatal(e)
		}
		if got.State == want {
			return got
		}
		if got.State == "failed" {
			t.Fatal(got)
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("login timed out")
	return Login{}
}
func TestGooglePKCEStateCallbackAndRedactedSummary(t *testing.T) {
	var challenge string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/token":
			_ = r.ParseForm()
			if r.Form.Get("client_id") != "synthetic-google-client" || r.Form.Get("client_secret") != "synthetic-google-secret" {
				t.Error("account registration changed")
			}
			if r.Form.Get("grant_type") == "authorization_code" {
				h := sha256.Sum256([]byte(r.Form.Get("code_verifier")))
				if base64.RawURLEncoding.EncodeToString(h[:]) != challenge || r.Form.Get("code") != "synthetic-code" {
					t.Error("PKCE failed", r.Form)
				}
			} else if r.Form.Get("refresh_token") != "synthetic-refresh" {
				t.Error("refresh credentials changed")
			}
			fmt.Fprint(w, `{"access_token":"synthetic-access","refresh_token":"synthetic-refresh","expires_in":3600}`)
		case "/who":
			if r.Header.Get("Authorization") != "Bearer synthetic-access" {
				t.Error("identity auth")
			}
			fmt.Fprint(w, `{"email":"synthetic@example.invalid"}`)
		default:
			t.Error(r.URL)
		}
	}))
	defer server.Close()
	m := Open(t.TempDir())
	m.Endpoints.GoogleToken = server.URL + "/token"
	m.Endpoints.GoogleWho = server.URL + "/who"
	login, e := m.StartWithOAuth("google", "test", "project", OAuthClient{ID: "synthetic-google-client", Secret: "synthetic-google-secret"})
	if e != nil {
		t.Fatal(e)
	}
	defer m.Cancel(login.ID)
	u, _ := url.Parse(login.URL)
	q := u.Query()
	challenge = q.Get("code_challenge")
	if challenge == "" || q.Get("state") == "" || q.Get("code_challenge_method") != "S256" || q.Get("client_id") != "synthetic-google-client" || strings.Contains(login.URL, "synthetic-google-secret") {
		t.Fatal(q)
	}
	callback := q.Get("redirect_uri")
	if m.Callback(login.ID, callback+"?state=wrong&code=synthetic-code") == nil {
		t.Fatal("accepted incorrect state")
	}
	if m.Callback(login.ID, callback+"?state="+q.Get("state")+"&code=synthetic-code") != nil {
		t.Fatal("callback rejected")
	}
	waitLogin(t, m, login.ID, "done")
	list, e := m.List()
	if e != nil || len(list) != 1 || list[0].User != "synthetic@example.invalid" {
		t.Fatal(list, e)
	}
	b, _ := json.Marshal(list)
	if strings.Contains(string(b), "synthetic-access") || strings.Contains(string(b), "synthetic-refresh") || strings.Contains(string(b), "synthetic-google-secret") || strings.Contains(string(b), "synthetic-google-client") {
		t.Fatal("leaked token")
	}
	d, e := m.load()
	if e != nil || len(d.Accounts) != 1 || d.Accounts[0].OAuthClientID != "synthetic-google-client" || d.Accounts[0].OAuthClientSecret != "synthetic-google-secret" {
		t.Fatal("registration not durably bound to account")
	}
	if _, e := m.Resolve(context.Background(), "google", true); e != nil {
		t.Fatal("refresh did not use saved registration", e)
	}
	if m.Callback(login.ID, callback+"?state="+q.Get("state")+"&code=again") == nil {
		t.Fatal("accepted completed callback")
	}
}
func TestRefreshRotationSingleOwnerAndAccountSwitch(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		_ = r.ParseForm()
		if r.Form.Get("refresh_token") != "synthetic-old" {
			t.Error("reused rotated token", r.Form)
		}
		fmt.Fprint(w, `{"access_token":"synthetic-new-access","refresh_token":"synthetic-next","expires_in":3600}`)
	}))
	defer server.Close()
	m := Open(t.TempDir())
	m.Endpoints.Token = server.URL
	a := Account{ID: "one", Kind: "chatgpt", User: "one", Enabled: true, Refresh: "synthetic-old", Expiry: 1}
	if e := m.store(a); e != nil {
		t.Fatal(e)
	}
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			a, e := m.Resolve(context.Background(), "chatgpt", false)
			if e != nil || a.Access != "synthetic-new-access" {
				t.Error(a, e)
			}
		}()
	}
	wg.Wait()
	if calls.Load() != 1 {
		t.Fatal("duplicate refresh", calls.Load())
	}
	d, e := m.load()
	if e != nil || d.Accounts[0].Refresh != "synthetic-next" {
		t.Fatal(d, e)
	}
	_ = m.store(Account{ID: "two", Kind: "chatgpt", User: "two", Enabled: true, Access: "two", Expiry: time.Now().Add(time.Hour).Unix()})
	_ = m.Edit("one", "switch", "")
	got, e := m.Resolve(context.Background(), "chatgpt", false)
	if e != nil || got.ID != "one" {
		t.Fatal(got, e)
	}
	_ = m.Edit("one", "enable", "false")
	if _, e = m.Resolve(context.Background(), "chatgpt", false); e == nil {
		t.Fatal("disabled account resolved")
	}
}
func TestCancelCallbackAndDeviceLogin(t *testing.T) {
	var polls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/device":
			fmt.Fprint(w, `{"device_code":"synthetic-device","user_code":"ABCD-EFGH","verification_uri":"https://github.com/login/device","expires_in":120,"interval":1}`)
		case "/token":
			polls.Add(1)
			fmt.Fprint(w, `{"access_token":"synthetic-github"}`)
		case "/who":
			fmt.Fprint(w, `{"login":"synthetic-user"}`)
		case "/session":
			if r.Header.Get("Authorization") != "Bearer synthetic-github" {
				t.Error("github exchange auth")
			}
			fmt.Fprintf(w, `{"token":"synthetic-session","expires_at":%d,"endpoints":{"api":"https://api.githubcopilot.com"}}`, time.Now().Add(time.Hour).Unix())
		default:
			t.Error(r.URL)
		}
	}))
	defer server.Close()
	m := Open(t.TempDir())
	m.Endpoints.Device = server.URL + "/device"
	m.Endpoints.DeviceToken = server.URL + "/token"
	m.Endpoints.GithubWho = server.URL + "/who"
	m.Endpoints.Copilot = server.URL + "/session"
	login, e := m.Start("copilot", "", "")
	if e != nil {
		t.Fatal(e)
	}
	waitLogin(t, m, login.ID, "done")
	a, e := m.Resolve(context.Background(), "copilot", false)
	if e != nil || a.Session != "synthetic-session" || polls.Load() != 1 {
		t.Fatal(a, e, polls.Load())
	}
	login, e = m.StartWithOAuth("google", "", "", OAuthClient{ID: "synthetic-client", Secret: "synthetic-secret"})
	if e != nil {
		t.Fatal(e)
	}
	u, _ := url.Parse(login.URL)
	_ = m.Cancel(login.ID)
	if m.Callback(login.ID, u.Query().Get("redirect_uri")+"?code=x&state="+u.Query().Get("state")) == nil {
		t.Fatal("cancelled callback accepted")
	}
	status, _ := m.LoginStatus(login.ID)
	if status.State != "cancelled" {
		t.Fatal(status)
	}
}
func TestChatGPTExchangeRequiresAccountIdentity(t *testing.T) {
	token := func(v any) string {
		b, _ := json.Marshal(v)
		return "e30." + base64.RawURLEncoding.EncodeToString(b) + ".signature"
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		if r.Form.Get("client_id") != codexClient {
			t.Error(r.Form)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "synthetic", "refresh_token": "synthetic-refresh", "id_token": token(map[string]any{"email": "test@example.invalid", "https://api.openai.com/auth": map[string]any{"chatgpt_account_id": "synthetic-account"}}), "expires_in": 3600})
	}))
	defer server.Close()
	m := Open(t.TempDir())
	m.Endpoints.Token = server.URL
	a, e := m.exchange(context.Background(), &flow{status: Login{Kind: "chatgpt"}}, "code")
	if e != nil || a.AccountID != "synthetic-account" || a.User != "test@example.invalid" {
		t.Fatal(a, e)
	}
}
func TestClaudeLoginUsesIsolatedCLIAndPastedCode(t *testing.T) {
	t.Setenv("UNI_SWITCH_ACCOUNT_HELPER", "1")
	t.Setenv("ANTHROPIC_API_KEY", "synthetic-real-client-must-not-use")
	exe, _ := os.Executable()
	t.Setenv("UNI_SWITCH_CLAUDE_BINARY", exe)
	dir := t.TempDir()
	m := Open(dir)
	login, e := m.Start("claude", "Claude test", "")
	if e != nil {
		t.Fatal(e)
	}
	defer m.Cancel(login.ID)
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		got, _ := m.LoginStatus(login.ID)
		if got.URL != "" {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if e = m.Callback(login.ID, "synthetic-code"); e != nil {
		t.Fatal(e)
	}
	waitLogin(t, m, login.ID, "done")
	a, e := m.Resolve(context.Background(), "claude", false)
	if e != nil || !strings.HasPrefix(a.ConfigDir, filepath.Join(dir, "subscriptions", "claude")) {
		t.Fatal(a, e)
	}
	if a.Access != "" {
		t.Fatal("Claude bypassed CLI credentials")
	}
}
func TestRefreshFailureKeepsDurableCredentials(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(401)
		io.WriteString(w, `{"error":"invalid_grant"}`)
	}))
	defer server.Close()
	m := Open(t.TempDir())
	m.Endpoints.Token = server.URL
	_ = m.store(Account{ID: "a", Kind: "chatgpt", User: "test", Enabled: true, Refresh: "synthetic-old"})
	if _, e := m.Resolve(context.Background(), "a", true); e == nil {
		t.Fatal("failed refresh succeeded")
	}
	d, _ := m.load()
	if d.Accounts[0].Refresh != "synthetic-old" {
		t.Fatal("failed refresh damaged credentials")
	}
}

func TestGoogleRegistrationRequiredWithoutStartingLoginOrRefresh(t *testing.T) {
	for _, kind := range []string{"google", "antigravity"} {
		m := Open(t.TempDir())
		for _, client := range []OAuthClient{{}, {ID: "synthetic"}, {Secret: "synthetic"}, {ID: "bad\nclient", Secret: "synthetic"}} {
			if _, e := m.StartWithOAuth(kind, "", "", client); e == nil {
				t.Fatal("accepted missing/invalid registration")
			}
		}
		if len(m.flows) != 0 {
			t.Fatal("invalid registration started a callback flow")
		}
		if e := m.store(Account{ID: "legacy", Kind: kind, User: "synthetic", Enabled: true, Refresh: "synthetic-refresh", Project: "project"}); e != nil {
			t.Fatal(e)
		}
		if _, e := m.Resolve(context.Background(), "legacy", true); e == nil || !strings.Contains(e.Error(), "OAuth") {
			t.Fatal("missing registration did not require a new login", e)
		}
	}
}

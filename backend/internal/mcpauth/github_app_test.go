package mcpauth_test

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/mcpauth"
)

// GitHub's remote MCP server (新v学员 on Discord): its sign-in is GitHub's
// own OAuth, which has no registration endpoint, so uni-switch signs in only as
// an OAuth app the user made. The answers are GitHub's own, saved from
// api.githubcopilot.com/mcp/ and github.com/login/oauth on 2026-10-12, with
// the addresses pointed here; the token endpoint answers its errors with a
// 200, as GitHub's documents.
type gitHub struct {
	*httptest.Server
	URL string // the MCP endpoint, <server>/mcp/

	id, secret string // the user's OAuth app
	refresh    bool   // tokens that run out, with a refresh token (a GitHub App's)
	// exact is the app's one callback URL when the server matches it
	// exactly, port and host too, as many do; empty, a loopback callback
	// takes any port, as GitHub's does
	exact string

	mu        sync.Mutex
	codes     map[string]string // code → its PKCE challenge
	redirects []string          // the redirect_uri of each authorize
	forms     []url.Values      // each token request
	valid     map[string]bool
	n         int
}

func newGitHub(t *testing.T, id, secret string) *gitHub {
	g := &gitHub{id: id, secret: secret, codes: map[string]string{}, valid: map[string]bool{}}
	mux := http.NewServeMux()
	g.Server = httptest.NewServer(mux)
	t.Cleanup(g.Server.Close)
	g.URL = g.Server.URL + "/mcp/"
	oauth := g.Server.URL + "/login/oauth"
	mux.HandleFunc("/mcp/", func(w http.ResponseWriter, r *http.Request) {
		g.mu.Lock()
		ok := g.valid[strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")]
		g.mu.Unlock()
		if !ok {
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			w.Header().Set("Www-Authenticate", `Bearer error="invalid_request", error_description="No access token was provided in this request", resource_metadata="`+g.Server.URL+`/.well-known/oauth-protected-resource/mcp/"`)
			w.WriteHeader(http.StatusUnauthorized)
			io.WriteString(w, "bad request: missing required Authorization header\n")
			return
		}
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"jsonrpc":"2.0","id":0,"result":{}}`)
	})
	mux.HandleFunc("/.well-known/oauth-protected-resource/mcp/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"resource":%q,"authorization_servers":[%q],"scopes_supported":["repo","read:org","read:user","user:email","read:packages","write:packages","read:project","project","gist","notifications"],"bearer_methods_supported":["header"],"resource_name":"GitHub MCP Server"}`, g.URL, oauth)
	})
	mux.HandleFunc("/.well-known/oauth-authorization-server/login/oauth", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"issuer":%q,"authorization_endpoint":%q,"token_endpoint":%q,"response_types_supported":["code"],"grant_types_supported":["authorization_code","refresh_token","urn:ietf:params:oauth:grant-type:device_code"],"service_documentation":"https://docs.github.com/apps/creating-github-apps/registering-a-github-app/registering-a-github-app","code_challenge_methods_supported":["S256"],"device_authorization_endpoint":%q,"authorization_response_iss_parameter_supported":true,"scopes_supported":["offline_access"]}`,
			oauth, oauth+"/authorize", oauth+"/access_token", g.Server.URL+"/login/device/code")
	})
	mux.HandleFunc("/login/oauth/authorize", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		back, err := url.Parse(q.Get("redirect_uri"))
		// a loopback callback URL registered as http://localhost/callback
		// takes any port; an exact one takes only itself
		loopback := err == nil && (back.Hostname() == "localhost" || back.Hostname() == "127.0.0.1") && back.Path == "/callback"
		if g.exact != "" {
			loopback = q.Get("redirect_uri") == g.exact
		}
		if q.Get("client_id") != g.id || !loopback || q.Get("code_challenge_method") != "S256" {
			http.Error(w, "The redirect_uri is not associated with this application.", http.StatusBadRequest)
			return
		}
		g.mu.Lock()
		g.n++
		code := fmt.Sprintf("%020x", g.n)
		g.codes[code] = q.Get("code_challenge")
		g.redirects = append(g.redirects, q.Get("redirect_uri"))
		g.mu.Unlock()
		bq := back.Query()
		bq.Set("code", code)
		bq.Set("state", q.Get("state"))
		bq.Set("iss", oauth)
		back.RawQuery = bq.Encode()
		http.Redirect(w, r, back.String(), http.StatusFound)
	})
	mux.HandleFunc("/login/oauth/access_token", func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		g.mu.Lock()
		defer g.mu.Unlock()
		g.forms = append(g.forms, r.PostForm)
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		if r.PostForm.Get("client_id") != g.id {
			w.WriteHeader(http.StatusNotFound)
			io.WriteString(w, `{"error":"Not Found"}`)
			return
		}
		if r.PostForm.Get("client_secret") != g.secret {
			io.WriteString(w, `{"error":"incorrect_client_credentials","error_description":"The client_id and/or client_secret passed are incorrect.","error_uri":"https://docs.github.com/apps/managing-oauth-apps/troubleshooting-oauth-app-access-token-request-errors/#incorrect-client-credentials"}`)
			return
		}
		switch r.PostForm.Get("grant_type") {
		case "refresh_token":
			io.WriteString(w, `{"error":"bad_refresh_token","error_description":"The refresh token passed is incorrect or expired.","error_uri":"https://docs.github.com/apps/managing-oauth-apps/troubleshooting-oauth-app-access-token-request-errors"}`)
			return
		default:
			challenge, ok := g.codes[r.PostForm.Get("code")]
			delete(g.codes, r.PostForm.Get("code"))
			sum := sha256.Sum256([]byte(r.PostForm.Get("code_verifier")))
			if !ok || base64.RawURLEncoding.EncodeToString(sum[:]) != challenge || (g.exact != "" && r.PostForm.Get("redirect_uri") != g.exact) {
				io.WriteString(w, `{"error":"bad_verification_code","error_description":"The code passed is incorrect or expired.","error_uri":"https://docs.github.com/apps/managing-oauth-apps/troubleshooting-oauth-app-access-token-request-errors/#bad-verification-code"}`)
				return
			}
		}
		g.n++
		a := fmt.Sprintf("gho_%036d", g.n)
		g.valid[a] = true
		out := map[string]any{"access_token": a, "token_type": "bearer", "scope": "gist,notifications,project,read:org,read:packages,read:user,repo,user:email,write:packages"}
		if g.refresh {
			out["expires_in"], out["refresh_token"], out["refresh_token_expires_in"] = 28800, fmt.Sprintf("ghr_%076d", g.n), 15897600
		}
		json.NewEncoder(w).Encode(out)
	})
	return g
}

func (g *gitHub) lastForm() url.Values {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.forms[len(g.forms)-1]
}

func (g *gitHub) redirect() string {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.redirects[0]
}

// signInAs signs in as app, standing in for the browser; the State the
// sign-in ended in.
func signInAs(t *testing.T, name, u string, app *mcpauth.App) mcpauth.State {
	t.Helper()
	st, err := mcpauth.StartWith(t.Context(), name, u, app)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.Get(st.URL)
	if err != nil {
		t.Fatal(err)
	}
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	got, _ := mcpauth.Progress(st.ID)
	return got
}

// Without an app of the user's own, the sign-in says what to do: make one,
// and give it in the server's editor.
func TestGitHubWithoutAnApp(t *testing.T) {
	home(t)
	g := newGitHub(t, "Ov23liAbCdEfGhIjKlMn", "0123456789abcdef0123456789abcdef01234567")
	_, err := mcpauth.Start(t.Context(), "github", g.URL)
	if !errors.Is(err, mcpauth.ErrNoRegistration) {
		t.Fatalf("err = %v", err)
	}
	for _, s := range []string{"OAuth app of your own", "http://localhost/callback", "client ID"} {
		if !strings.Contains(err.Error(), s) {
			t.Errorf("the error doesn't say %q: %v", s, err)
		}
	}
}

// With the user's app, uni-switch registers nothing: it signs in as that app,
// called back at localhost's /callback as Claude Code is, and gives the secret in the form,
// as GitHub's token endpoint takes it. The record keeps the app, to renew
// as it.
func TestGitHubSignInAsTheUsersApp(t *testing.T) {
	home(t)
	id, secret := "Ov23liAbCdEfGhIjKlMn", "0123456789abcdef0123456789abcdef01234567"
	g := newGitHub(t, id, secret)
	if st := signInAs(t, "github", g.URL, &mcpauth.App{ClientID: id, ClientSecret: secret}); st.State != "done" {
		t.Fatalf("sign-in %s: %s", st.State, st.Error)
	}
	r, ok := mcpauth.Get("github")
	if !ok || !strings.HasPrefix(r.Access, "gho_") || r.ClientID != id || r.Secret != secret || r.AuthMethod != "client_secret_post" || r.Resource != g.URL {
		t.Fatalf("record = %+v", r)
	}
	if !mcpauth.SignedIn("github", g.URL) {
		t.Fatal("not signed in")
	}
	f := g.lastForm()
	if f.Get("client_id") != id || f.Get("client_secret") != secret || f.Get("grant_type") != "authorization_code" {
		t.Errorf("token request = %v", f)
	}
	if u, _ := url.Parse(g.redirect()); u.Hostname() != "localhost" || u.Path != "/callback" || u.Port() == "" {
		t.Errorf("redirect_uri = %s", g.redirect())
	}
}

// A wrong secret: GitHub answers 200 with its error, which the sign-in
// says rather than "no access token".
func TestGitHubWrongSecret(t *testing.T) {
	home(t)
	id := "Ov23liAbCdEfGhIjKlMn"
	g := newGitHub(t, id, "0123456789abcdef0123456789abcdef01234567")
	st := signInAs(t, "github", g.URL, &mcpauth.App{ClientID: id, ClientSecret: "wrong"})
	if st.State != "failed" || !strings.Contains(st.Error, "incorrect_client_credentials") {
		t.Fatalf("sign-in %s: %s", st.State, st.Error)
	}
	if mcpauth.SignedIn("github", g.URL) {
		t.Fatal("signed in with a wrong secret")
	}
}

// A callback port is the port the callback comes to, for an app
// registered with one.
func TestGitHubCallbackPort(t *testing.T) {
	home(t)
	id, secret := "Ov23liAbCdEfGhIjKlMn", "0123456789abcdef0123456789abcdef01234567"
	g := newGitHub(t, id, secret)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	// taken: the sign-in says so
	if _, err := mcpauth.StartWith(t.Context(), "github", g.URL, &mcpauth.App{ClientID: id, ClientSecret: secret, CallbackPort: port}); err == nil || !strings.Contains(err.Error(), fmt.Sprint(port)) {
		t.Fatalf("a taken port: %v", err)
	}
	ln.Close()
	if st := signInAs(t, "github", g.URL, &mcpauth.App{ClientID: id, ClientSecret: secret, CallbackPort: port}); st.State != "done" {
		t.Fatalf("sign-in %s: %s", st.State, st.Error)
	}
	if want := fmt.Sprintf("http://localhost:%d/callback", port); g.redirect() != want {
		t.Errorf("redirect_uri = %s, want %s", g.redirect(), want)
	}
}

// An app made for Claude Code signs uni-switch in as it is. Claude Code calls
// a pre-configured client back at http://localhost:<callbackPort>/callback
// (code.claude.com/docs/en/mcp, "Use pre-configured OAuth credentials";
// its 2.1.296 builds it as `http://localhost:${port}/callback` and listens
// on 127.0.0.1), and a server that matches the registered URL exactly
// refuses any other host: Claude Code's own 2.1.229 sent 127.0.0.1 and was
// refused, until 2.1.231 went back to localhost.
func TestAppMadeForClaudeCode(t *testing.T) {
	home(t)
	id, secret := "Ov23liAbCdEfGhIjKlMn", "0123456789abcdef0123456789abcdef01234567"
	g := newGitHub(t, id, secret)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()
	g.exact = fmt.Sprintf("http://localhost:%d/callback", port)
	if st := signInAs(t, "github", g.URL, &mcpauth.App{ClientID: id, ClientSecret: secret, CallbackPort: port}); st.State != "done" {
		t.Fatalf("sign-in %s: %s (registered %s)", st.State, st.Error, g.exact)
	}
	if f := g.lastForm(); f.Get("redirect_uri") != g.exact {
		t.Errorf("token request's redirect_uri = %s", f.Get("redirect_uri"))
	}
}

// A refresh token GitHub won't take again (its 200 bad_refresh_token) runs
// the sign-in out, as invalid_grant does elsewhere.
func TestGitHubBadRefreshToken(t *testing.T) {
	home(t)
	id, secret := "Iv23liAbCdEfGhIjKlMn", "0123456789abcdef0123456789abcdef01234567"
	g := newGitHub(t, id, secret)
	g.refresh = true
	if st := signInAs(t, "github", g.URL, &mcpauth.App{ClientID: id, ClientSecret: secret}); st.State != "done" {
		t.Fatalf("sign-in %s: %s", st.State, st.Error)
	}
	r, _ := mcpauth.Get("github")
	if r.Refresh == "" || r.Expires == 0 {
		t.Fatalf("record = %+v", r)
	}
	if _, err := mcpauth.Renew(t.Context(), "github", r.Access); !errors.Is(err, mcpauth.ErrExpired) {
		t.Fatalf("renew: %v", err)
	}
	if st := mcpauth.StatusOf("github", g.URL); !st.Dead {
		t.Fatalf("status = %+v", st)
	}
	if f := g.lastForm(); f.Get("client_secret") != secret || f.Get("grant_type") != "refresh_token" {
		t.Errorf("renewal = %v", f)
	}
}

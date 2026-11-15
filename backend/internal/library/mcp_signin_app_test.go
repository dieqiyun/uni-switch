package library

import (
	"errors"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/mcpauth"
	"github.com/dieqiyun/uni-switch/backend/internal/mcpauth/mcpauthtest"
)

// A server whose sign-in doesn't let uni-switch register itself (GitHub's, 新v学员
// on Discord) is signed in to as the user's own OAuth app, given in the
// library by Claude Code's names. Without one, the sign-in says to give
// one. The app is uni-switch's alone: no agent's file is given it, its secret
// above all.
func TestSignInAsTheUsersOwnApp(t *testing.T) {
	sandbox(t)
	f := mcpauthtest.New(t)
	f.NoRegistration = true
	f.AddApp("Ov23liAbCdEfGhIjKlMn", "s3cr3t-of-the-app")
	gh := Server{Name: "github", Transport: "http", URL: f.URL, Agents: []string{"claude", "codex"}}
	ok(t)(SaveServer("", gh))
	if _, err := SignInServer(t.Context(), "github"); !errors.Is(err, mcpauth.ErrNoRegistration) {
		t.Fatalf("without an app: %v", err)
	}

	gh.OAuth = &OAuthApp{ClientID: " Ov23liAbCdEfGhIjKlMn ", ClientSecret: "s3cr3t-of-the-app\n"}
	ok(t)(SaveServer("github", gh))
	v, err := Read(nil)
	if err != nil {
		t.Fatal(err)
	}
	if a := v.Servers[0].OAuth; a == nil || a.ClientID != "Ov23liAbCdEfGhIjKlMn" || a.ClientSecret != "s3cr3t-of-the-app" {
		t.Fatalf("the library's app: %+v", a)
	}
	st, err := SignInServer(t.Context(), "github")
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.Get(st.URL)
	if err != nil {
		t.Fatal(err)
	}
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	if got, _ := mcpauth.Progress(st.ID); got.State != "done" {
		t.Fatalf("sign-in %s: %s", got.State, got.Error)
	}
	if r, _ := mcpauth.Get("github"); r.ClientID != "Ov23liAbCdEfGhIjKlMn" || r.AuthMethod != "client_secret_post" {
		t.Fatalf("record = %+v", r)
	}
	if len(f.Registered) != 0 {
		t.Fatalf("registered %v", f.Registered)
	}
	for _, id := range []string{"claude", "codex"} {
		b, err := os.ReadFile(targetByID(id).MCP.Path)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(b), "s3cr3t") || strings.Contains(string(b), "Ov23li") {
			t.Errorf("%s was given the app: %s", id, b)
		}
	}
}

// The app is checked as it is saved: only an HTTP server has one, an app
// needs its client ID, and a callback port is a port.
func TestOAuthAppChecked(t *testing.T) {
	for _, c := range []struct {
		s    Server
		want *OAuthApp
		err  string
	}{
		{Server{Name: "a", Transport: "http", URL: "https://x/mcp", OAuth: &OAuthApp{ClientID: "id", CallbackPort: 8080}}, &OAuthApp{ClientID: "id", CallbackPort: 8080}, ""},
		{Server{Name: "a", Transport: "http", URL: "https://x/mcp", OAuth: &OAuthApp{ClientID: "  "}}, nil, ""},
		{Server{Name: "a", Transport: "sse", URL: "https://x/sse", OAuth: &OAuthApp{ClientID: "id"}}, nil, ""},
		{Server{Name: "a", Transport: "stdio", Command: "x", OAuth: &OAuthApp{ClientID: "id"}}, nil, ""},
		{Server{Name: "a", Transport: "http", URL: "https://x/mcp", OAuth: &OAuthApp{ClientSecret: "s"}}, nil, "client ID"},
		{Server{Name: "a", Transport: "http", URL: "https://x/mcp", OAuth: &OAuthApp{ClientID: "id", CallbackPort: 70000}}, nil, "callback port"},
	} {
		s := c.s
		err := s.check()
		if c.err != "" {
			if err == nil || !strings.Contains(err.Error(), c.err) {
				t.Errorf("%+v: err = %v", c.s.OAuth, err)
			}
			continue
		}
		if err != nil || (s.OAuth == nil) != (c.want == nil) || (s.OAuth != nil && *s.OAuth != *c.want) {
			t.Errorf("%s %+v: %+v, %v", c.s.Transport, c.s.OAuth, s.OAuth, err)
		}
	}
}

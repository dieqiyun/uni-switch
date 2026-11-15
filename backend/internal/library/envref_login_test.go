//go:build !windows

package library

import (
	"context"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/testenv"
)

// loginToken gives the test a login shell that exports CYRUS_TOKEN, as
// a .zshrc would, while uni-switch's own environment hasn't it, as a uni-switch
// opened from the Finder hasn't (Cyrus on Discord). The token is returned.
func loginToken(t *testing.T, home string) string {
	t.Helper()
	const tok = "s3cret-from-zshrc"
	sh := filepath.Join(home, "login-sh")
	testenv.Program(t, sh, "#!/bin/sh\nexport CYRUS_TOKEN="+tok+"\nexport CYRUS_EMPTY=\neval \"$2\"\n")
	t.Setenv("SHELL", sh)
	for _, v := range []string{"CYRUS_TOKEN", "CYRUS_EMPTY", "CYRUS_NOWHERE"} {
		t.Setenv(v, "")
		os.Unsetenv(v)
	}
	return tok
}

// A ${NAME} only the login shell exports reaches the server uni-switch starts
// or calls itself, for the tool list (a command's environment and a URL's
// header) and for the router, and is never written anywhere: not into
// library.json, not into an agent's file. One set nowhere is named.
func TestEnvRefsFromLoginShell(t *testing.T) {
	h := sandbox(t)
	tok := loginToken(t, h)

	got, unset := expandAll(map[string]string{"A": "Bearer ${CYRUS_TOKEN}", "B": "x${CYRUS_EMPTY}y", "C": "${CYRUS_NOWHERE}"})
	if got["A"] != "Bearer "+tok || got["B"] != "xy" || len(unset) != 1 || unset[0] != "CYRUS_NOWHERE" {
		t.Errorf("expanded %v, unset %v", got, unset)
	}

	t.Run("command", func(t *testing.T) {
		s := fakeStdio(t, "ok", map[string]string{"UNI_SWITCH_FAKE_MCP_TOKEN": "${CYRUS_TOKEN}", "UNI_SWITCH_FAKE_MCP_WANT": tok})
		if hl := CheckServer(context.Background(), s); hl.State != "ok" || hl.Tools != 3 {
			t.Fatalf("got %+v, want ok with 3 tools", hl)
		}
		// the router starts it the same way
		c, err := dialHub(context.Background(), s, nil)
		if err != nil {
			t.Fatalf("router: %v", err)
		}
		c.close()
		// and runs it so for Codex
		if spec, ok := specOf(s); !ok || spec.Env["UNI_SWITCH_FAKE_MCP_TOKEN"] != tok {
			t.Fatalf("Codex's run: %v %+v", ok, spec.Env)
		}
	})
	t.Run("url", func(t *testing.T) {
		var seen http.Header
		mcp := mcpHandler(t, false, &seen)
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("Authorization") != "Bearer "+tok {
				http.Error(w, "unauthorized", 401)
				return
			}
			mcp(w, r)
		}))
		defer srv.Close()
		s := &Server{Name: "r", Transport: "http", URL: srv.URL, Headers: map[string]string{"Authorization": "Bearer ${CYRUS_TOKEN}"}}
		if hl := CheckServer(context.Background(), s); hl.State != "ok" || hl.Tools != 2 {
			t.Fatalf("got %+v, want ok with 2 tools", hl)
		}
		c, err := dialHub(context.Background(), s, nil)
		if err != nil {
			t.Fatalf("router: %v", err)
		}
		c.close()
	})
	t.Run("set nowhere", func(t *testing.T) {
		hl := CheckServer(context.Background(), fakeStdio(t, "ok", map[string]string{"UNI_SWITCH_FAKE_MCP_TOKEN": "${CYRUS_NOWHERE}"}))
		if hl.Why != "novar" || hl.Detail != "CYRUS_NOWHERE" {
			t.Fatalf("got %+v, want novar naming CYRUS_NOWHERE", hl)
		}
		if _, ok := specOf(fakeStdio(t, "ok", map[string]string{"UNI_SWITCH_FAKE_MCP_TOKEN": "${CYRUS_NOWHERE}"})); ok {
			t.Fatal("Codex is run a server with a variable set nowhere")
		}
	})
	t.Run("never written", func(t *testing.T) {
		s := Server{Name: "gh", Transport: "http", URL: "http://127.0.0.1:9/mcp",
			Headers: map[string]string{"Authorization": "Bearer ${CYRUS_TOKEN}"}, Agents: []string{"claude", "codex"}}
		if _, err := SaveServer("", s); err != nil {
			t.Fatal(err)
		}
		CheckServers(context.Background(), []string{"gh"}, true)
		if _, err := Sync(); err != nil {
			t.Fatal(err)
		}
		filepath.WalkDir(h, func(p string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || p == filepath.Join(h, "login-sh") {
				return nil
			}
			if b, _ := os.ReadFile(p); strings.Contains(string(b), tok) {
				t.Errorf("%s has the token", p)
			}
			return nil
		})
		if os.Getenv("CYRUS_TOKEN") != "" {
			t.Error("the token was set in uni-switch's own environment")
		}
	})
}

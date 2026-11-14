package library

import (
	"errors"
	"strings"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/mcpauth"
	"github.com/dieqiyun/uni-switch/backend/internal/mcpauth/mcpauthtest"
)

// Xichun123 (#1619): Figma's remote MCP server (mcp.figma.com/mcp) admits
// only the clients it lists, and its authorization server answers uni-switch's
// dynamic client registration 403 with the bare body "Forbidden". The
// Library's Sign in said "registering uni-switch with the server's sign-in
// failed (403): Forbidden", which reads as a fault to retry. It now says the
// server admits only its listed apps and that each agent signs in itself,
// and the agents given the server keep its own address, where they do.
func TestSignInRefusedByAnAllowListLeavesItToTheAgents(t *testing.T) {
	sandbox(t)
	f := mcpauthtest.New(t)
	f.RefuseRegistration = true
	agents := []string{"claude", "codex", "cursor"}
	figma := Server{Name: "figma", Transport: "http", URL: f.URL, Agents: agents}
	ok(t)(SaveServer("", figma))

	_, err := SignInServer(t.Context(), "figma")
	if !errors.Is(err, mcpauth.ErrClientRefused) {
		t.Fatalf("sign-in error %v, want the server admitting only its listed apps", err)
	}
	if msg := err.Error(); !strings.Contains(msg, "admits only the apps it lists") || !strings.Contains(msg, "/mcp") || strings.Contains(msg, "(403)") {
		t.Fatalf("message %q", msg)
	}
	if mcpauth.SignedIn("figma", f.URL) {
		t.Fatal("signed in after a refused registration")
	}
	for _, id := range agents {
		got, err := targetByID(id).MCP.read()
		if err != nil {
			t.Fatal(err)
		}
		if s := got["figma"]; s == nil || !s.same(&figma) {
			t.Fatalf("%s isn't given the server's own address to sign in at: %+v", id, s)
		}
	}
}

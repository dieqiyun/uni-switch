package library

import (
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/gateway"
	"github.com/dieqiyun/uni-switch/backend/internal/mcpauth"
	"github.com/dieqiyun/uni-switch/backend/internal/mcpauth/mcpauthtest"
)

// A remote server uni-switch signed in to (#615) is given to the agents at the
// gateway's /mcp/<name>, without the Authorization the library has for it,
// and only while uni-switch is signed in: before, after signing out and for an
// SSE server, the agents are given the server's own address as before. A
// rename keeps the sign-in, and a server taken out of the library loses it.
func TestSignedInServerGoesThroughUniSwitch(t *testing.T) {
	sandbox(t)
	f := mcpauthtest.New(t)
	agents := []string{"claude", "codex", "cursor"}
	neon := Server{Name: "neon", Transport: "http", URL: f.URL, Headers: map[string]string{"Authorization": "Bearer mine", "X-Team": "acme"}, Agents: agents}
	ok(t)(SaveServer("", neon))
	sse := Server{Name: "old", Transport: "sse", URL: f.URL, Agents: []string{"claude"}}
	ok(t)(SaveServer("", sse))
	given := func(name string) map[string]*Server {
		t.Helper()
		out := map[string]*Server{}
		for _, id := range agents {
			got, err := targetByID(id).MCP.read()
			if err != nil {
				t.Fatal(err)
			}
			out[id] = got[name]
		}
		return out
	}
	for id, s := range given("neon") {
		if s == nil || !s.same(&neon) {
			t.Fatalf("%s before signing in: %+v", id, s)
		}
	}

	f.SignIn(t, "neon") // the agents are given uni-switch's address as it ends
	local := gateway.URL() + "/mcp/neon"
	for id, s := range given("neon") {
		if s == nil || s.URL != local || s.Transport != "http" || s.Headers["Authorization"] != "" || s.Headers["X-Team"] != "acme" {
			t.Fatalf("%s signed in: %+v", id, s)
		}
	}
	if s := given("old")["claude"]; s == nil || s.URL != f.URL {
		t.Fatalf("an SSE server isn't relayed: %+v", s)
	}
	v, err := Read(nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, sv := range v.Servers {
		switch sv.Name {
		case "neon":
			if sv.SignIn == nil || !sv.SignIn.SignedIn || sv.URL != f.URL {
				t.Fatalf("the page's neon: %+v %+v", sv.Server, sv.SignIn)
			}
		case "old":
			if sv.SignIn != nil {
				t.Fatal("an SSE server has a sign-in to show")
			}
		}
	}
	// a second sync leaves the files as they are
	if res := ok(t)(Sync()); len(res.Changed) != 0 {
		t.Fatalf("a sync with nothing new changed %v", res.Changed)
	}

	renamed := neon
	renamed.Name = "neon2"
	ok(t)(SaveServer("neon", renamed))
	for id, s := range given("neon2") {
		if s == nil || s.URL != gateway.URL()+"/mcp/neon2" {
			t.Fatalf("%s renamed: %+v", id, s)
		}
	}

	// another URL is another server, which uni-switch isn't signed in to
	moved := renamed
	moved.URL = f.URL + "?v=2"
	ok(t)(SaveServer("neon2", moved))
	for id, s := range given("neon2") {
		if s == nil || s.URL != moved.URL || s.Headers["Authorization"] != "Bearer mine" {
			t.Fatalf("%s at another URL: %+v", id, s)
		}
	}
	ok(t)(SaveServer("neon2", renamed))

	if err := SignOutServer("neon2"); err != nil {
		t.Fatal(err)
	}
	for id, s := range given("neon2") {
		if s == nil || !s.same(&renamed) {
			t.Fatalf("%s signed out: %+v", id, s)
		}
	}

	f.SignIn(t, "neon2")
	ok(t)(RemoveServer("neon2"))
	if _, ok := mcpauth.Get("neon2"); ok {
		t.Fatal("a server taken out kept its sign-in")
	}
}

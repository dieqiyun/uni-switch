package library

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"slices"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/agent"
)

// claudeOwnSignIn is Claude Code's .credentials.json after /mcp signed in
// to name at url, as Claude Code 2.1.310 writes it: under mcpOAuth, keyed
// by the name and a hash of the server's config, beside its own account's
// claudeAiOauth; a second, unfinished sign-in (no token) to another server
// that Claude Code doesn't count.
func claudeOwnSignIn(name, url string) string {
	return `{"claudeAiOauth":{"accessToken":"sk-ant-oat01-x","refreshToken":"sk-ant-ort01-x"},
"mcpOAuth":{
  "` + name + `|0123456789abcdef":{"serverName":"` + name + `","serverUrl":"` + url + `","accessToken":"tok","refreshToken":"ref","expiresAt":1893456000000,"clientId":"c"},
  "half|fedcba9876543210":{"serverName":"half","serverUrl":"` + url + `/half","accessToken":""}
}}`
}

// CyberCaius on Discord: a remote server signed in to in Claude Code in
// WSL was "needs sign-in" in the Library, as uni-switch's check connects
// without the agent's tokens. The check reads each Claude Code's own
// sign-ins where it keeps them, the WSL distro's home for one there, and
// names the agents signed in; a server no agent signed in to, and a
// sign-in left unfinished, still needs one.
func TestCheckNamesAgentsSignedInThemselves(t *testing.T) {
	h := sandbox(t)
	// every variable the reader's places come from: Claude Code's folder
	// here, and the distro's, which only its probe says
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	checked.Lock()
	checked.m = map[string]checkedServer{}
	checked.Unlock()
	root := t.TempDir()
	// the distro's Claude Code, its home \\wsl.localhost\Ubuntu\home\me
	write(t, filepath.Join(root, "home", "me", ".claude", "settings.json"), "{}\n")
	// a Windows machine running Ubuntu, whatever the OS the test runs on
	t.Cleanup(agent.FakeWSL(map[string]string{
		"Ubuntu": "home:/home/me\ndir:.claude\nnet:mirrored\n",
	}, map[string]string{"Ubuntu": root}))
	const wsl = "claude@wsl:Ubuntu"
	if tg := targetByID(wsl); tg == nil {
		t.Fatalf("%s isn't a target: %v", wsl, ids(Targets()))
	}

	// a server that asks every client without a token to sign in
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("WWW-Authenticate", `Bearer resource_metadata="http://`+r.Host+`/.well-known/oauth-protected-resource"`)
		w.WriteHeader(401)
	}))
	t.Cleanup(srv.Close)
	url := srv.URL + "/mcp"
	write(t, filepath.Join(h, ".claude", "settings.json"), "{}\n")
	ok(t)(SaveServer("", Server{Name: "linear", Transport: "http", URL: url, Agents: []string{"claude", wsl}}))
	ok(t)(SaveServer("", Server{Name: "half", Transport: "http", URL: url + "/half", Agents: []string{wsl}}))

	check := func() map[string]ServerHealth {
		t.Helper()
		got, err := CheckServers(context.Background(), nil, true)
		if err != nil {
			t.Fatal(err)
		}
		return got
	}
	if got := check()["linear"]; got.State != "auth" || len(got.AgentSignIn) != 0 {
		t.Fatalf("no agent signed in yet: %+v", got)
	}

	// signed in in WSL's Claude Code only
	write(t, filepath.Join(root, "home", "me", ".claude", ".credentials.json"), claudeOwnSignIn("linear", url))
	got := check()
	if l := got["linear"]; l.State != "auth" || !l.OAuth || !slices.Equal(l.AgentSignIn, []string{wsl}) {
		t.Fatalf("signed in in %s, the check says %+v", wsl, l)
	}
	if hf := got["half"]; len(hf.AgentSignIn) != 0 {
		t.Fatalf("a sign-in with no token counted: %+v", hf)
	}

	// and in this computer's Claude Code too; a page opened again (not a
	// fresh check) isn't given the answer from before it
	write(t, filepath.Join(h, ".claude", ".credentials.json"), claudeOwnSignIn("linear", url+"/"))
	again, err := CheckServers(context.Background(), []string{"linear"}, false)
	if err != nil {
		t.Fatal(err)
	}
	if l := again["linear"]; !slices.Equal(l.AgentSignIn, []string{"claude", wsl}) {
		t.Fatalf("signed in on both sides, the check says %+v", l)
	}

	// another server's sign-in by the same name isn't this one's
	write(t, filepath.Join(h, ".claude", ".credentials.json"), claudeOwnSignIn("linear", "https://elsewhere.example/mcp"))
	write(t, filepath.Join(root, "home", "me", ".claude", ".credentials.json"), "{not json")
	if l := check()["linear"]; l.State != "auth" || len(l.AgentSignIn) != 0 {
		t.Fatalf("no agent signed in to this URL, unreadable credentials: %+v", l)
	}
}

// codexOwnSignIn is Codex's .credentials.json after `codex mcp login` kept
// the tokens in the file, as codex-rs's rmcp-client writes it
// (FallbackTokenEntry in oauth.rs): an entry under the key its
// compute_store_key makes of the name and a 16-hex hash of the server's
// config, the tokens flattened into it; a second, unfinished sign-in (no
// token) that codex itself doesn't answer a lookup with.
func codexOwnSignIn(name, url string) string {
	return `{
  "` + name + `|0123456789abcdef":{"server_name":"` + name + `","server_url":"` + url + `","client_id":"c","access_token":"tok","refresh_token":"ref","expires_at":1893456000000,"scopes":["mcp"]},
  "half|fedcba9876543210":{"server_name":"half","server_url":"` + url + `/half","client_id":"c","access_token":""}
}`
}

// codexOwnSignInExecutor is one entry of the executor's own, which codex
// never answers a host lookup with.
func codexOwnSignInExecutor(name, url string) string {
	return `{"` + name + `:0123456789abcdef":{"server_name":"` + name + `","server_url":"` + url + `","client_id":"c","access_token":"tok","executor_owned":true}}`
}

// A remote server signed in to in Codex itself (`codex mcp login`) was
// "needs sign-in" in the Library, as uni-switch's check connects without the
// agent's tokens. The check reads the file codex-rs falls back to when
// mcp_oauth_credentials_store is file or the keyring is missing, beside
// the config.toml the target already has, a distro's too, and names the
// agents signed in; the keyring itself is never read, the executor's
// entries are not a `codex mcp login`, and a file that can't be read or
// parsed names nobody.
func TestCheckNamesCodexSignedInItself(t *testing.T) {
	h := sandbox(t)
	// every variable the reader's places come from: Codex's folder here,
	// and the distro's, which only its probe says
	t.Setenv("CODEX_HOME", "")
	checked.Lock()
	checked.m = map[string]checkedServer{}
	checked.Unlock()
	root := t.TempDir()
	// the distro's Codex, its home \\wsl.localhost\Ubuntu\home\me
	write(t, filepath.Join(root, "home", "me", ".codex", "config.toml"), "model = \"gpt-5.5\"\n")
	// a Windows machine running Ubuntu, whatever the OS the test runs on
	t.Cleanup(agent.FakeWSL(map[string]string{
		"Ubuntu": "home:/home/me\ndir:.codex\nnet:mirrored\n",
	}, map[string]string{"Ubuntu": root}))
	const wsl = "codex@wsl:Ubuntu"
	if tg := targetByID(wsl); tg == nil {
		t.Fatalf("%s isn't a target: %v", wsl, ids(Targets()))
	}

	// a server that asks every client without a token to sign in
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("WWW-Authenticate", `Bearer resource_metadata="http://`+r.Host+`/.well-known/oauth-protected-resource"`)
		w.WriteHeader(401)
	}))
	t.Cleanup(srv.Close)
	url := srv.URL + "/mcp"
	ok(t)(SaveServer("", Server{Name: "linear", Transport: "http", URL: url, Agents: []string{"codex", wsl}}))
	ok(t)(SaveServer("", Server{Name: "half", Transport: "http", URL: url + "/half", Agents: []string{wsl}}))

	check := func() map[string]ServerHealth {
		t.Helper()
		got, err := CheckServers(context.Background(), nil, true)
		if err != nil {
			t.Fatal(err)
		}
		return got
	}
	if got := check()["linear"]; got.State != "auth" || len(got.AgentSignIn) != 0 {
		t.Fatalf("no agent signed in yet: %+v", got)
	}

	// signed in in WSL's Codex only
	write(t, filepath.Join(root, "home", "me", ".codex", ".credentials.json"), codexOwnSignIn("linear", url))
	got := check()
	if l := got["linear"]; l.State != "auth" || !slices.Equal(l.AgentSignIn, []string{wsl}) {
		t.Fatalf("signed in in %s, the check says %+v", wsl, l)
	}
	if hf := got["half"]; len(hf.AgentSignIn) != 0 {
		t.Fatalf("a sign-in with no token counted: %+v", hf)
	}

	// and in this computer's Codex too; a page opened again (not a fresh
	// check) isn't given the answer from before it
	write(t, filepath.Join(h, ".codex", ".credentials.json"), codexOwnSignIn("linear", url+"/"))
	again, err := CheckServers(context.Background(), []string{"linear"}, false)
	if err != nil {
		t.Fatal(err)
	}
	if l := again["linear"]; !slices.Equal(l.AgentSignIn, []string{"codex", wsl}) {
		t.Fatalf("signed in on both sides, the check says %+v", l)
	}

	// the executor's entry isn't a `codex mcp login`, and a file that
	// can't be read or parsed names nobody
	write(t, filepath.Join(root, "home", "me", ".codex", ".credentials.json"), codexOwnSignInExecutor("linear", url))
	write(t, filepath.Join(h, ".codex", ".credentials.json"), "{not json")
	if l := check()["linear"]; l.State != "auth" || len(l.AgentSignIn) != 0 {
		t.Fatalf("only the executor's sign-in counted, unreadable credentials: %+v", l)
	}
}

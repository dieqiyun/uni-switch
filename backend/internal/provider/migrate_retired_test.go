package provider

import (
	"path/filepath"
	"strings"
	"testing"
)

// A retired built-in is its plugin's to serve before any move: signing in
// to the built-in is turned away, and only an id with a plugin can be
// retired. An id not retired is the plugin's only once moved.
func TestRetiredBuiltinIsThePlugins(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", home+"/.config")
	id := ""
	for m := range movers {
		if !retiredBuiltins[m] {
			id = m
			break
		}
	}
	if id == "" {
		t.Skip("every built-in with a plugin is retired")
	}
	if PluginServes(id) {
		t.Fatalf("%s is the plugin's with nothing moved or retired", id)
	}
	retiredBuiltins[id], retiredBuiltins["claude"] = true, true
	t.Cleanup(func() { delete(retiredBuiltins, id); delete(retiredBuiltins, "claude") })
	if !PluginServes(id) || !BuiltinRetired(id) {
		t.Fatalf("retired %s isn't the plugin's", id)
	}
	if BuiltinRetired("claude") || PluginServes("claude") {
		t.Fatal("claude, which has no plugin, is retired")
	}
	if _, err := StartSignInWith(id, SignInOptions{}); err == nil || !strings.Contains(err.Error(), "plugin") {
		t.Fatalf("signing in to the retired built-in: %v", err)
	}
}

// A retired built-in's accounts not moved yet are its mover's to take,
// the agent's own sign-in too: listed as the agent's own, not left out as
// the plugin's, until the move has run.
func TestRetiredOwnAccountStillMoves(t *testing.T) {
	home := signIn(t)
	was := retiredBuiltins["zcode"]
	retiredBuiltins["zcode"] = true
	t.Cleanup(func() { retiredBuiltins["zcode"] = was })
	t.Setenv("ZCODE_CREDENTIAL_SECRET", "test-secret")
	writeFile(t, filepath.Join(home, ".zcode", "v2", "credentials.json"), map[string]any{
		"oauth:zai:user_info": zcodeEncrypt(t, `{"user_id":"u1","email":"own@example.com"}`),
		"account-provider:coding-plan:account:zai-individual-coding-plan:account:abc:api-key": zcodeEncrypt(t, "own.secret"),
	})
	if own := movingOf(t, "zcode")["own@example.com"]; !own.Own {
		t.Fatalf("ZCode's own, retired and not moved: %+v", own)
	}
	if c := MoveCandidates(); len(c) != 1 || c[0].ID != "zcode" || c[0].Accounts != 1 {
		t.Fatalf("candidates: %+v", c)
	}
}

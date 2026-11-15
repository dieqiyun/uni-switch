package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Codex rewrites auth.json in place when it refreshes its tokens
// (codex-rs FileAuthStorage::save opens it with truncate), so a read in
// that moment finds it empty or cut off. That says nothing about the
// sign-in, and must not move Codex off it: Sync took such a read for "no
// ChatGPT sign-in" and made uni-switch Codex's provider, and the Codex app
// then showed "uni-switch" for its account and uni-switch's catalog for its
// models (lucy on Discord, v0.1.1394, on and off). Nothing put it back,
// as codex.out wasn't marked. A half-written file is unknown, and unknown
// is the sign-in as last read.
func TestCodexHalfWrittenAuthKeepsSignIn(t *testing.T) {
	const signedIn = `{"auth_mode":"chatgpt","tokens":{"access_token":"x","id_token":"x.e30.x","refresh_token":"r"}}`
	for name, mid := range map[string]string{
		"empty":     "",
		"cut off":   signedIn[:30],
		"not json":  "\x00\x00\x00",
		"truncated": `{"auth_mode":"chatgpt","tokens":{"access_tok`,
	} {
		t.Run(name, func(t *testing.T) {
			home, read := codexHome(t, signedIn, "")
			cx := codex(home)
			if err := cx.Fields[0].Set("fake/m1"); err != nil {
				t.Fatal(err)
			}
			beside := func(when string) {
				t.Helper()
				if cfg := read(); strings.Contains(cfg, "model_provider =") || strings.Contains(cfg, "model_catalog_json") ||
					!strings.Contains(cfg, "openai_base_url") || !strings.Contains(cfg, `model = "fake/m1"`) {
					t.Fatalf("%s: not beside the sign-in:\n%s", when, cfg)
				}
			}
			beside("picked")
			auth := filepath.Join(home, ".codex", "auth.json")
			if err := os.WriteFile(auth, []byte(mid), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := cx.Sync(); err != nil {
				t.Fatal(err)
			}
			beside("synced while Codex rewrites auth.json")
			// a pick in that moment (a group switch, the Agents page) too
			if err := cx.Fields[0].Set("fake/m1"); err != nil {
				t.Fatal(err)
			}
			beside("picked while Codex rewrites auth.json")
			if err := os.WriteFile(auth, []byte(signedIn), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := cx.Sync(); err != nil {
				t.Fatal(err)
			}
			beside("written again")
		})
	}
}

// The same while the Codex app holds the account and uni-switch is Codex's
// provider for that (codex.out): a pick in that moment keeps the mark, so
// Sync puts uni-switch back beside the sign-in once the account has room. It
// was forgotten, and Codex stayed on uni-switch as its provider.
func TestCodexHalfWrittenAuthKeepsOut(t *testing.T) {
	const signedIn = `{"tokens":{"access_token":"x","id_token":"x.e30.x"}}`
	home, read := codexHome(t, signedIn, "")
	cx := codex(home)
	codexUsedUp = func() bool { return true }
	if err := cx.Fields[0].Set("fake/m1"); err != nil {
		t.Fatal(err)
	}
	if cfg := read(); !strings.Contains(cfg, `model_provider = "uni-switch"`) {
		t.Fatalf("held: uni-switch its provider:\n%s", cfg)
	}
	auth := filepath.Join(home, ".codex", "auth.json")
	os.WriteFile(auth, nil, 0o600)
	if err := cx.Fields[0].Set("fake/m1"); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(auth, []byte(signedIn), 0o600)
	codexUsedUp = func() bool { return false }
	if err := cx.Sync(); err != nil {
		t.Fatal(err)
	}
	if cfg := read(); strings.Contains(cfg, "model_provider =") || !strings.Contains(cfg, "openai_base_url") {
		t.Fatalf("room again after a pick mid-rewrite: not back beside the sign-in:\n%s", cfg)
	}
}

// A sign-in really gone (auth.json removed: codex logout) or one with no
// ChatGPT tokens (an API key) is known, and still moves Codex onto uni-switch
// as its provider, as before.
func TestCodexSignInGoneStillKnown(t *testing.T) {
	for name, after := range map[string]string{
		"logged out": "",
		"api key":    `{"OPENAI_API_KEY":"sk-x","tokens":null}`,
	} {
		t.Run(name, func(t *testing.T) {
			home, read := codexHome(t, `{"tokens":{"access_token":"x","id_token":"x.e30.x"}}`, "")
			cx := codex(home)
			if err := cx.Fields[0].Set("fake/m1"); err != nil {
				t.Fatal(err)
			}
			auth := filepath.Join(home, ".codex", "auth.json")
			if after == "" {
				os.Remove(auth)
			} else if err := os.WriteFile(auth, []byte(after), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := cx.Sync(); err != nil {
				t.Fatal(err)
			}
			if cfg := read(); !strings.Contains(cfg, `model_provider = "uni-switch"`) {
				t.Fatalf("no ChatGPT sign-in: uni-switch its provider:\n%s", cfg)
			}
		})
	}
}

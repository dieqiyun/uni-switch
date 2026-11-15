package agent

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/edit"
)

const chatGPTAuth = `{"tokens":{"access_token":"x","id_token":"x.e30.x"}}`

// namesGone is the model_catalog_json config.toml names when no file is
// there: Codex then loads no config at all ("No such file or directory
// (os error 2)"), the CLI and the ChatGPT app both (zwaydot on X).
func namesGone(home string) string {
	dir := filepath.Join(home, ".codex")
	c, _ := edit.GetTOMLTop(filepath.Join(dir, "config.toml"), "model_catalog_json")
	if c == "" {
		return ""
	}
	if !filepath.IsAbs(c) {
		c = filepath.Join(dir, c)
	}
	if _, err := os.Stat(c); err != nil {
		return c
	}
	return ""
}

// Whatever leads up to it, uni-switch stepping out of Codex never leaves its
// config naming a model list that isn't there. Disconnect removed
// uni-switch-models.json whatever Codex's provider was, and took
// model_catalog_json out only while model_provider was still uni-switch's: one
// moved off uni-switch (by the app, another tool, a hand edit) kept the key
// naming the removed file, and Codex didn't open again.
func TestCodexNeverNamesGoneCatalog(t *testing.T) {
	path := func(home string) string { return filepath.Join(home, ".codex", "config.toml") }
	cases := []struct {
		name, auth string
		between    func(t *testing.T, home string, cx *Agent)
		end        func(cx *Agent) error
	}{
		{"connect, disconnect", "", nil, func(cx *Agent) error { return cx.Disconnect() }},
		{"connect, Codex's default", "", nil, func(cx *Agent) error { return cx.Fields[0].Set("") }},
		{"connect, a model of Codex's own", "", nil, func(cx *Agent) error { return cx.Fields[0].Set("gpt-5.5") }},
		{"provider taken out, disconnect", "", func(t *testing.T, home string, cx *Agent) {
			edit.DelTOMLTop(path(home), "model_provider")
		}, func(cx *Agent) error { return cx.Disconnect() }},
		{"provider taken out, a model of Codex's own", "", func(t *testing.T, home string, cx *Agent) {
			edit.DelTOMLTop(path(home), "model_provider")
		}, func(cx *Agent) error { return cx.Fields[0].Set("gpt-5.5") }},
		{"provider taken out, sync", "", func(t *testing.T, home string, cx *Agent) {
			edit.DelTOMLTop(path(home), "model_provider")
		}, func(cx *Agent) error { return cx.Sync() }},
		{"provider and base URL taken out, disconnect", "", func(t *testing.T, home string, cx *Agent) {
			edit.DelTOMLTop(path(home), "model_provider", "openai_base_url")
		}, func(cx *Agent) error { return cx.Disconnect() }},
		{"another provider, no base URL, disconnect", "", func(t *testing.T, home string, cx *Agent) {
			edit.SetTOMLTop(path(home), edit.KV{Path: "model_provider", Value: "custom"})
			edit.DelTOMLTop(path(home), "openai_base_url")
		}, func(cx *Agent) error { return cx.Disconnect() }},
		{"own model, provider and base URL taken out, unwire", "", func(t *testing.T, home string, cx *Agent) {
			edit.SetTOMLTop(path(home), edit.KV{Path: "model", Value: "gpt-5.5"})
			edit.DelTOMLTop(path(home), "model_provider", "openai_base_url")
		}, func(cx *Agent) error { return cx.Unwire() }},
		{"own model picked in the app, disconnect", "", func(t *testing.T, home string, cx *Agent) {
			edit.SetTOMLTop(path(home), edit.KV{Path: "model", Value: "gpt-5.5"})
		}, func(cx *Agent) error { return cx.Disconnect() }},
		{"ChatGPT, disconnect", chatGPTAuth, nil, func(cx *Agent) error { return cx.Disconnect() }},
		{"ChatGPT out, disconnect", chatGPTAuth, func(t *testing.T, home string, cx *Agent) {
			codexUsedUp = func() bool { return true }
			cx.Fields[0].Set("fake/m1")
		}, func(cx *Agent) error { return cx.Disconnect() }},
		{"ChatGPT out, room again, sync", chatGPTAuth, func(t *testing.T, home string, cx *Agent) {
			codexUsedUp = func() bool { return true }
			cx.Fields[0].Set("fake/m1")
			codexUsedUp = func() bool { return false }
		}, func(cx *Agent) error { return cx.Sync() }},
		{"API key, then signs in to ChatGPT, disconnect", "", func(t *testing.T, home string, cx *Agent) {
			os.WriteFile(filepath.Join(home, ".codex", "auth.json"), []byte(chatGPTAuth), 0o600)
		}, func(cx *Agent) error { return cx.Disconnect() }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			home, read := codexHome(t, c.auth, "model = \"gpt-5.5\"\n")
			cx := codex(home)
			if err := cx.Connect(); err != nil {
				t.Fatal(err)
			}
			if c.auth != "" {
				cx.Fields[0].Set("fake/m1")
			}
			if c.between != nil {
				c.between(t, home, cx)
			}
			err := c.end(cx)
			if g := namesGone(home); g != "" {
				t.Errorf("err=%v; config.toml names %s, which is gone:\n%s", err, g, read())
			}
		})
	}
}

// A model list of the user's own, put back as uni-switch steps out, is put
// back only while its file is there: one removed since uni-switch connected
// would keep Codex from loading its config.
func TestCodexOwnCatalogGoneNotPutBack(t *testing.T) {
	mine := filepath.Join(t.TempDir(), "mine.json")
	os.WriteFile(mine, []byte(`{"models":[]}`), 0o644)
	home, read := codexHome(t, "", "model = \"gpt-5.5\"\nmodel_catalog_json = \""+filepath.ToSlash(mine)+"\"\n")
	cx := codex(home)
	if err := cx.Connect(); err != nil {
		t.Fatal(err)
	}
	os.Remove(mine)
	if err := cx.Disconnect(); err != nil {
		t.Fatal(err)
	}
	if g := namesGone(home); g != "" {
		t.Fatalf("config.toml names %s, which is gone:\n%s", g, read())
	}

	// one still there is put back
	mine2 := filepath.Join(t.TempDir(), "mine.json")
	os.WriteFile(mine2, []byte(`{"models":[]}`), 0o644)
	home2, read2 := codexHome(t, "", "model = \"gpt-5.5\"\nmodel_catalog_json = \""+filepath.ToSlash(mine2)+"\"\n")
	cx2 := codex(home2)
	if err := cx2.Connect(); err != nil {
		t.Fatal(err)
	}
	if err := cx2.Disconnect(); err != nil {
		t.Fatal(err)
	}
	if c, _ := edit.GetTOMLTop(filepath.Join(home2, ".codex", "config.toml"), "model_catalog_json"); filepath.Clean(c) != filepath.Clean(mine2) {
		t.Fatalf("the user's own catalog wasn't put back:\n%s", read2())
	}
}

// A config an older uni-switch left naming its removed list is mended at the
// next start (Sync), with no reconnect: the key taken out while uni-switch is
// not Codex's provider, the list written again while it is.
func TestCodexSyncMendsGoneCatalog(t *testing.T) {
	t.Run("not the provider", func(t *testing.T) {
		home, read := codexHome(t, "", "model = \"gpt-5.5\"\n")
		list := filepath.Join(home, ".codex", "uni-switch-models.json")
		os.WriteFile(filepath.Join(home, ".codex", "config.toml"),
			[]byte("model = \"gpt-5.5\"\nmodel_catalog_json = \""+filepath.ToSlash(list)+"\"\n"), 0o644)
		if err := codex(home).Sync(); err != nil {
			t.Fatal(err)
		}
		if g := namesGone(home); g != "" {
			t.Fatalf("config.toml still names %s:\n%s", g, read())
		}
		if m, _ := edit.GetTOMLTop(filepath.Join(home, ".codex", "config.toml"), "model"); m != "gpt-5.5" {
			t.Fatalf("the user's model went:\n%s", read())
		}
	})
	t.Run("the provider", func(t *testing.T) {
		home, read := codexHome(t, "", "model = \"gpt-5.5\"\n")
		cx := codex(home)
		if err := cx.Connect(); err != nil {
			t.Fatal(err)
		}
		list := filepath.Join(home, ".codex", "uni-switch-models.json")
		if err := os.Remove(list); err != nil {
			t.Fatal(err)
		}
		if err := cx.Sync(); err != nil {
			t.Fatal(err)
		}
		if g := namesGone(home); g != "" {
			t.Fatalf("config.toml still names %s:\n%s", g, read())
		}
		if p, _ := edit.GetTOMLTop(filepath.Join(home, ".codex", "config.toml"), "model_provider"); p != uniSwitchID {
			t.Fatalf("uni-switch is no longer the provider:\n%s", read())
		}
	})
}

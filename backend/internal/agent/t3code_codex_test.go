package agent

import (
	"encoding/json"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/edit"
	"github.com/dieqiyun/uni-switch/backend/internal/gateway"
	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// t3Overrides is the -c overrides in launchArgs, key to raw TOML value.
func t3Overrides(t *testing.T, args string) map[string]string {
	t.Helper()
	toks := t3Tokens(args)
	out := map[string]string{}
	for i := 0; i < len(toks); i += 2 {
		if toks[i] != "-c" || i+1 >= len(toks) {
			t.Fatalf("not -c pairs: %q", toks)
		}
		k, v, ok := strings.Cut(toks[i+1], "=")
		if !ok {
			t.Fatalf("override %q", toks[i+1])
		}
		out[k] = v
	}
	return out
}

type t3CodexEnvelope struct {
	Driver      string `json:"driver"`
	DisplayName string `json:"displayName"`
	Enabled     *bool  `json:"enabled"`
	Config      struct {
		BinaryPath string `json:"binaryPath"`
		HomePath   string `json:"homePath"`
		LaunchArgs string `json:"launchArgs"`
	} `json:"config"`
}

func t3CodexRead(t *testing.T, path string) (t3CodexEnvelope, bool) {
	t.Helper()
	raw, ok := edit.GetJSON(path, t3CodexInstance)
	if !ok {
		return t3CodexEnvelope{}, false
	}
	var e t3CodexEnvelope
	if err := json.Unmarshal([]byte(raw), &e); err != nil {
		t.Fatalf("%v\n%s", err, raw)
	}
	return e, true
}

// karissasel_ on X: T3 Code through uni-switch ran Claude Code only. uni-switch
// gives T3 a Codex instance too, so a uni-switch model can be picked under
// either harness: Codex made to use uni-switch as its provider by its own -c
// overrides in the instance's launch arguments, as T3 tokenizes them, with
// uni-switch's catalog for T3 as Codex's, and T3's own Codex's binary and home
// carried over. ~/.codex is never written.
func TestT3CodeCodexInstance(t *testing.T) {
	home := syncHome(t)
	// a home whose path a shell and TOML both need quoting for
	base := filepath.Join(home, `T3 "a" $b`)
	t.Setenv("T3CODE_HOME", base)
	if err := provider.Save(provider.Provider{ID: "resp", Name: "Resp", Key: "k", Responses: "http://127.0.0.1:1/v1", Models: []string{"gpt-5.5"}}); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(base, "userdata", "settings.json")
	os.MkdirAll(filepath.Dir(path), 0o755)
	os.WriteFile(path, []byte(`{"providerInstances":{"codex":{"driver":"codex","config":{"binaryPath":"/opt/codex","homePath":"~/.codex-t3"}}}}`), 0o600)
	a := t3code(home)
	f := a.Field("provider")
	if err := f.Set(uniSwitchID); err != nil {
		t.Fatal(err)
	}
	e, ok := t3CodexRead(t, path)
	if !ok || e.Driver != "codex" || e.DisplayName == "" || e.Enabled == nil || !*e.Enabled {
		t.Fatalf("instance: %+v\n%s", e, readFile(path))
	}
	if e.Config.BinaryPath != "/opt/codex" || e.Config.HomePath != "~/.codex-t3" {
		t.Errorf("T3's Codex not carried over: %+v", e.Config)
	}
	cat := filepath.Join(base, "userdata", "uni-switch-codex-models.json")
	str := func(v string) string { return t3TOML(v) }
	want := map[string]string{
		"model_provider":                                       str(uniSwitchID),
		"model_providers.uni-switch.name":                      str("uni-switch"),
		"model_providers.uni-switch.base_url":                  str(gateway.URL() + "/v1"),
		"model_providers.uni-switch.wire_api":                  str("responses"),
		"model_providers.uni-switch.experimental_bearer_token": str(gateway.Token),
		"model_providers.uni-switch.http_headers":              `{"x-openai-actor-authorization"="uni-switch"}`,
		"model_catalog_json":                                   str(cat),
	}
	if got := t3Overrides(t, e.Config.LaunchArgs); !maps.Equal(got, want) {
		t.Errorf("overrides:\n got %v\nwant %v", got, want)
	}
	if !strings.Contains(str(cat), `\"a\"`) {
		t.Fatalf("the path's quotes aren't TOML-escaped: %s", str(cat))
	}
	var list struct {
		Models []struct {
			Slug string `json:"slug"`
		} `json:"models"`
	}
	if err := json.Unmarshal([]byte(readFile(cat)), &list); err != nil {
		t.Fatalf("catalog: %v", err)
	}
	if !slices.ContainsFunc(list.Models, func(m struct {
		Slug string `json:"slug"`
	}) bool {
		return m.Slug == "resp/gpt-5.5"
	}) {
		t.Errorf("catalog lacks resp/gpt-5.5: %s", readFile(cat))
	}
	if _, err := os.Stat(filepath.Join(home, ".codex")); err == nil {
		t.Error("~/.codex written")
	}
	if why := a.Check(); why != "" {
		t.Errorf("check: %s", why)
	}

	// the base URL gone from the launch arguments is said, and a sync
	// puts it back
	if err := edit.SetJSON(path, edit.KV{Path: t3CodexInstance + ".config.launchArgs", Value: "--strict-config"}); err != nil {
		t.Fatal(err)
	}
	if why := a.Check(); !strings.Contains(why, "Codex") {
		t.Errorf("check after the args went: %q", why)
	}
	if err := a.Sync(); err != nil {
		t.Fatal(err)
	}
	if e, _ := t3CodexRead(t, path); !strings.Contains(e.Config.LaunchArgs, t3CodexBase(gateway.URL())) || a.Check() != "" {
		t.Errorf("not repaired: %s / %q", e.Config.LaunchArgs, a.Check())
	}

	// off takes both instances and the catalog out
	if err := f.Set(""); err != nil {
		t.Fatal(err)
	}
	if _, ok := t3CodexRead(t, path); ok || f.Get() != "" {
		t.Errorf("after off: %s", readFile(path))
	}
	if _, err := os.Stat(cat); err == nil {
		t.Error("catalog left after off")
	}
	if _, ok := edit.GetJSON(path, "providerInstances.codex"); !ok {
		t.Error("T3's own Codex removed")
	}
}

// An older uni-switch wrote its Claude Code instance alone: the next sync adds
// the Codex one. One the user then removed in T3 stays removed through
// syncs, and turning uni-switch on again brings it back. The name and on/off
// the user gave it stay.
func TestT3CodeCodexInstanceAddedOnce(t *testing.T) {
	home := syncHome(t)
	t.Setenv("T3CODE_HOME", "")
	path := filepath.Join(home, ".t3", "userdata", "settings.json")
	a := t3code(home)
	os.MkdirAll(filepath.Dir(path), 0o755)
	if err := edit.SetJSON(path, edit.KV{Path: t3Instance, Value: t3Merged(path, gateway.URL())}); err != nil {
		t.Fatal(err)
	}
	if err := a.Sync(); err != nil {
		t.Fatal(err)
	}
	if _, ok := t3CodexRead(t, path); !ok {
		t.Fatalf("not added by a sync: %s", readFile(path))
	}

	if err := edit.SetJSON(path, edit.KV{Path: t3CodexInstance + ".displayName", Value: "Codex via uni-switch"}, edit.KV{Path: t3CodexInstance + ".enabled", Value: false}); err != nil {
		t.Fatal(err)
	}
	if err := a.Sync(); err != nil {
		t.Fatal(err)
	}
	if e, _ := t3CodexRead(t, path); e.DisplayName != "Codex via uni-switch" || e.Enabled == nil || *e.Enabled {
		t.Errorf("the user's name or off lost: %+v", e)
	}

	if err := edit.DelJSON(path, t3CodexInstance); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := a.Sync(); err != nil {
			t.Fatal(err)
		}
	}
	if _, ok := t3CodexRead(t, path); ok {
		t.Fatal("a sync brought back the instance the user removed")
	}
	if a.Field("provider").Get() != uniSwitchID {
		t.Fatal("uni-switch off with its Claude Code instance there")
	}
	if err := a.Field("provider").Set(uniSwitchID); err != nil {
		t.Fatal(err)
	}
	if _, ok := t3CodexRead(t, path); !ok {
		t.Fatal("turning uni-switch on didn't bring the Codex instance back")
	}

	// the Claude Code instance removed in T3, the Codex one keeps uni-switch on
	if err := edit.DelJSON(path, t3Instance); err != nil {
		t.Fatal(err)
	}
	if a.Field("provider").Get() != uniSwitchID || a.Check() != "" {
		t.Errorf("Codex instance alone: %q %q", a.Field("provider").Get(), a.Check())
	}
}

package agent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/catalog"
	"github.com/dieqiyun/uni-switch/backend/internal/gateway"
	"github.com/dieqiyun/uni-switch/backend/internal/provider"
	"github.com/dieqiyun/uni-switch/backend/internal/settings"
)

// snowDefault is a profile as Snow CLI 0.9.2 writes it for someone on a
// provider of their own, with settings uni-switch has nothing to do with.
const snowDefault = `{
  "snowcfg": {
    "baseUrl": "https://idealab.example.com/v1",
    "baseUrlMode": "auto",
    "apiKey": "team-key",
    "requestMethod": "chat",
    "advancedModel": "glm-5",
    "basicModel": "glm-5-air",
    "supportsVision": true,
    "maxContextTokens": 200000,
    "maxTokens": 64000,
    "streamingDisplay": false,
    "systemPromptId": "terse"
  },
  "companionMuted": true
}`

// snowWork is a second profile of the user's, signed in to a subscription
// through Snow's own OAuth.
const snowWork = `{
  "snowcfg": {
    "baseUrl": "https://api.example.com/v1",
    "apiKey": "",
    "requestMethod": "responses",
    "advancedModel": "kimi-k3",
    "basicModel": "kimi-k3",
    "oauth": {"provider": "example", "accessToken": "secret"}
  }
}`

func snowHome(t *testing.T) (home, dir string) {
	t.Helper()
	home = syncHome(t)
	if err := provider.Save(provider.Provider{ID: "deepseek", Name: "DeepSeek", Key: "k", Chat: "http://127.0.0.1:1/v1", Models: []string{"pro"}}); err != nil {
		t.Fatal(err)
	}
	dir = filepath.Join(home, ".snow")
	os.MkdirAll(filepath.Join(dir, "profiles"), 0o755)
	return home, dir
}

// snowUser lays out ~/.snow as Snow leaves it with the user's two
// profiles, default the active one.
func snowUser(t *testing.T, dir string) {
	t.Helper()
	os.WriteFile(filepath.Join(dir, "profiles", "default.json"), []byte(snowDefault), 0o644)
	os.WriteFile(filepath.Join(dir, "profiles", "work.json"), []byte(snowWork), 0o644)
	os.WriteFile(filepath.Join(dir, "active-profile.json"), []byte("{\n  \"activeProfile\": \"default\"\n}"), 0o644)
	os.WriteFile(filepath.Join(dir, "config.json"), []byte(snowDefault), 0o644)
}

func snowCfg(t *testing.T, path string) map[string]any {
	t.Helper()
	var f map[string]any
	if err := json.Unmarshal([]byte(readFile(path)), &f); err != nil {
		t.Fatalf("%s: %v\n%s", path, err, readFile(path))
	}
	sc, _ := f["snowcfg"].(map[string]any)
	if sc == nil {
		t.Fatalf("%s has no snowcfg:\n%s", path, readFile(path))
	}
	return sc
}

func snowActive(dir string) string {
	b, err := os.ReadFile(filepath.Join(dir, "active-profile.json"))
	if err != nil {
		return "(none)"
	}
	var v struct{ ActiveProfile string }
	json.Unmarshal(b, &v)
	return v.ActiveProfile
}

func TestSnow(t *testing.T) {
	_, dir := snowHome(t)
	snowUser(t, dir)
	a, err := Find("snow")
	if err != nil {
		t.Fatal(err)
	}
	if !a.Detected() || a.Dir != dir {
		t.Fatalf("not found in %s: %+v", dir, a)
	}
	if got := a.Values()["model"]; got != "glm-5" {
		t.Fatalf("model: %q", got)
	}
	// the picker: the user's profiles' models, then uni-switch's catalog
	opts := a.Field("model").Options(a.Values())
	if len(opts) < 3 || opts[0].Value != "glm-5" || opts[1].Value != "kimi-k3" || opts[0].Group != "Snow CLI" {
		t.Fatalf("own options: %+v", opts)
	}
	var via bool
	for _, o := range opts {
		via = via || o.Value == "uni-switch/deepseek/pro"
	}
	if !via {
		t.Fatalf("no uni-switch/deepseek/pro in %+v", opts)
	}

	if err := a.Apply("model", "uni-switch/deepseek/pro"); err != nil {
		t.Fatal(err)
	}
	ours := filepath.Join(dir, "profiles", "uni-switch.json")
	sc := snowCfg(t, ours)
	for k, want := range map[string]any{
		"baseUrl": gatewayV1(), "baseUrlMode": "base", "apiKey": gateway.TokenFor("snow"), "requestMethod": "chat",
		"advancedModel": "deepseek/pro", "basicModel": "deepseek/pro",
		// the user's own settings come along
		"streamingDisplay": false, "systemPromptId": "terse",
	} {
		if sc[k] != want {
			t.Errorf("snowcfg.%s = %v, want %v", k, sc[k], want)
		}
	}
	if !strings.Contains(readFile(ours), `"companionMuted": true`) {
		t.Errorf("the profile's other settings are gone:\n%s", readFile(ours))
	}
	if got := snowActive(dir); got != "uni-switch" {
		t.Fatalf("active profile %q", got)
	}
	// config.json is a copy of the active profile, as Snow's switch makes it
	if readFile(filepath.Join(dir, "config.json")) != readFile(ours) {
		t.Fatalf("config.json isn't uni-switch's profile:\n%s", readFile(filepath.Join(dir, "config.json")))
	}
	if readFile(filepath.Join(dir, "profiles", "default.json")) != snowDefault || readFile(filepath.Join(dir, "profiles", "work.json")) != snowWork {
		t.Fatal("a profile of the user's was rewritten")
	}
	if got := a.Values()["model"]; got != "uni-switch/deepseek/pro" {
		t.Fatalf("model after pick: %q", got)
	}
	if !a.Wired() {
		t.Fatal("not connected on uni-switch's model")
	}
	if msg := a.Check(); msg != "" {
		t.Fatalf("check: %s", msg)
	}
	// its own profile's list doesn't take uni-switch's for the user's
	for _, o := range a.Field("model").Options(a.Values()) {
		if o.Group == "Snow CLI" && o.Value == "deepseek/pro" {
			t.Fatalf("uni-switch's profile listed as the user's: %+v", o)
		}
	}

	// another of uni-switch's: the same profile, the model moved
	if err := a.Apply("model", "uni-switch/relay/glm-4.6"); err != nil {
		t.Fatal(err)
	}
	if sc := snowCfg(t, ours); sc["advancedModel"] != "relay/glm-4.6" || sc["streamingDisplay"] != false {
		t.Fatalf("second pick: %v", sc)
	}
	// glm-4.6's context, from the catalog
	if sc := snowCfg(t, ours); sc["maxContextTokens"] != float64(204800) {
		t.Fatalf("context: %v", sc["maxContextTokens"])
	}

	// back: the profile the user was on, uni-switch's gone
	if err := a.Apply("model", ""); err != nil {
		t.Fatal(err)
	}
	if got := snowActive(dir); got != "default" {
		t.Fatalf("active after reset: %q", got)
	}
	if _, err := os.Stat(ours); !os.IsNotExist(err) {
		t.Fatalf("uni-switch.json left: %v", err)
	}
	if readFile(filepath.Join(dir, "config.json")) != snowDefault {
		t.Fatalf("config.json not the user's profile again:\n%s", readFile(filepath.Join(dir, "config.json")))
	}
	if got := a.Values()["model"]; got != "glm-5" {
		t.Fatalf("model after reset: %q", got)
	}
	for k := range stashLoad() {
		if strings.HasPrefix(k, "snow:") {
			t.Errorf("stash keeps %s", k)
		}
	}
}

// A model of another profile of the user's, picked while on uni-switch's,
// switches Snow to that profile; its OAuth sign-in never comes into
// uni-switch's.
func TestSnowOwnProfile(t *testing.T) {
	_, dir := snowHome(t)
	snowUser(t, dir)
	os.WriteFile(filepath.Join(dir, "active-profile.json"), []byte(`{"activeProfile": "work"}`), 0o644)
	os.WriteFile(filepath.Join(dir, "config.json"), []byte(snowWork), 0o644)
	a, _ := Find("snow")
	if err := a.Apply("model", "uni-switch/deepseek/pro"); err != nil {
		t.Fatal(err)
	}
	ours := filepath.Join(dir, "profiles", "uni-switch.json")
	if body := readFile(ours); strings.Contains(body, "oauth") || strings.Contains(body, "secret") {
		t.Fatalf("the user's sign-in copied into uni-switch's profile:\n%s", body)
	}
	if sc := snowCfg(t, ours); sc["requestMethod"] != "chat" {
		t.Fatalf("requestMethod %v", sc["requestMethod"])
	}
	if err := a.Apply("model", "glm-5"); err != nil {
		t.Fatal(err)
	}
	if got := snowActive(dir); got != "default" {
		t.Fatalf("active %q, want default (the profile on glm-5)", got)
	}
	if _, err := os.Stat(ours); !os.IsNotExist(err) {
		t.Fatal("uni-switch.json left")
	}
	if readFile(filepath.Join(dir, "config.json")) != snowDefault {
		t.Fatal("config.json isn't default's")
	}
	if readFile(filepath.Join(dir, "profiles", "work.json")) != snowWork {
		t.Fatal("work.json rewritten")
	}
}

// A Snow from before profiles (config.json alone) and one never started
// (an empty ~/.snow) are each left as they were.
func TestSnowNoProfiles(t *testing.T) {
	t.Run("config.json alone", func(t *testing.T) {
		_, dir := snowHome(t)
		os.Remove(filepath.Join(dir, "profiles"))
		os.WriteFile(filepath.Join(dir, "config.json"), []byte(snowDefault), 0o644)
		a, _ := Find("snow")
		if got := a.Values()["model"]; got != "glm-5" {
			t.Fatalf("model: %q", got)
		}
		if err := a.Apply("model", "uni-switch/deepseek/pro"); err != nil {
			t.Fatal(err)
		}
		// Snow makes a default profile of config.json at its next start
		// when there is none: the user's, not uni-switch's
		if readFile(filepath.Join(dir, "profiles", "default.json")) != snowDefault {
			t.Fatalf("default.json:\n%s", readFile(filepath.Join(dir, "profiles", "default.json")))
		}
		if err := a.Apply("model", ""); err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(filepath.Join(dir, "active-profile.json")); !os.IsNotExist(err) {
			t.Fatal("active-profile.json left, there was none")
		}
		if readFile(filepath.Join(dir, "config.json")) != snowDefault {
			t.Fatal("config.json not the user's")
		}
		if got := a.Values()["model"]; got != "glm-5" {
			t.Fatalf("model after: %q", got)
		}
	})
	t.Run("never started", func(t *testing.T) {
		_, dir := snowHome(t)
		a, _ := Find("snow")
		if err := a.Apply("model", "uni-switch/deepseek/pro"); err != nil {
			t.Fatal(err)
		}
		sc := snowCfg(t, filepath.Join(dir, "profiles", "uni-switch.json"))
		if sc["baseUrl"] != gatewayV1() || sc["maxContextTokens"] != float64(snowContext) {
			t.Fatalf("%v", sc)
		}
		if err := a.Apply("model", ""); err != nil {
			t.Fatal(err)
		}
		for _, f := range []string{"active-profile.json", "config.json", "profiles/uni-switch.json", "profiles/default.json"} {
			if _, err := os.Stat(filepath.Join(dir, f)); !os.IsNotExist(err) {
				t.Errorf("%s left behind", f)
			}
		}
	})
}

// A profile of the user's own named uni-switch is theirs: not read as
// uni-switch's, and back as it was once uni-switch steps out.
func TestSnowUsersUniSwitchProfile(t *testing.T) {
	_, dir := snowHome(t)
	snowUser(t, dir)
	mine := filepath.Join(dir, "profiles", "uni-switch.json")
	os.WriteFile(mine, []byte(snowWork), 0o644)
	os.WriteFile(filepath.Join(dir, "active-profile.json"), []byte(`{"activeProfile": "uni-switch"}`), 0o644)
	a, _ := Find("snow")
	if got := a.Values()["model"]; got != "kimi-k3" || a.Wired() {
		t.Fatalf("the user's own uni-switch profile reads %q, wired %v", got, a.Wired())
	}
	if err := a.Apply("model", "uni-switch/deepseek/pro"); err != nil {
		t.Fatal(err)
	}
	if sc := snowCfg(t, mine); sc["apiKey"] != gateway.TokenFor("snow") {
		t.Fatalf("%v", sc)
	}
	if err := a.Apply("model", ""); err != nil {
		t.Fatal(err)
	}
	if readFile(mine) != snowWork || snowActive(dir) != "uni-switch" {
		t.Fatalf("the user's uni-switch profile not back (active %q):\n%s", snowActive(dir), readFile(mine))
	}
}

// Something else rewrote uni-switch's profile: Check says what, and Sync puts
// the gateway back; $SNOW_CONFIG_DIR is where Snow looks.
func TestSnowCheckSync(t *testing.T) {
	home, _ := snowHome(t)
	dir := filepath.Join(home, "elsewhere")
	t.Setenv("SNOW_CONFIG_DIR", dir)
	os.MkdirAll(filepath.Join(dir, "profiles"), 0o755)
	snowUser(t, dir)
	a, _ := Find("snow")
	if a.Dir != dir {
		t.Fatalf("dir %s", a.Dir)
	}
	if err := a.Apply("model", "uni-switch/deepseek/pro"); err != nil {
		t.Fatal(err)
	}
	ours := filepath.Join(dir, "profiles", "uni-switch.json")
	os.WriteFile(ours, []byte(strings.Replace(readFile(ours), gatewayV1(), "http://127.0.0.1:9/v1", 1)), 0o644)
	if msg := a.Check(); !strings.Contains(msg, "baseUrl") {
		t.Fatalf("check: %q", msg)
	}
	if err := a.Sync(); err != nil {
		t.Fatal(err)
	}
	if msg := a.Check(); msg != "" {
		t.Fatalf("after sync: %s", msg)
	}
	if readFile(filepath.Join(dir, "config.json")) != readFile(ours) {
		t.Fatal("config.json not synced")
	}
	if _, err := os.Stat(filepath.Join(home, ".snow", "profiles", "uni-switch.json")); !os.IsNotExist(err) {
		t.Fatal("wrote ~/.snow with SNOW_CONFIG_DIR set")
	}
}

// snowSees is a catalog with a model that takes no images (GLM-4.6, as
// models.dev lists it: text in) and one that does (GLM-4.5V), both on the
// relay.
func snowSees(t *testing.T) {
	t.Helper()
	os.WriteFile(catalog.CachePath(), []byte(`{"zai":{"models":{
"glm-4.6":{"id":"glm-4.6","name":"GLM-4.6","modalities":{"input":["text"],"output":["text"]},"limit":{"context":204800,"output":131072}},
"glm-4.5v":{"id":"glm-4.5v","name":"GLM-4.5V","modalities":{"input":["text","image"],"output":["text"]},"limit":{"context":65536,"output":16384}}}}}`), 0o644)
	catalog.Reset()
	if err := provider.Save(provider.Provider{ID: "relay", Name: "Relay", Key: "k", Chat: "http://127.0.0.1:1/v1", Models: []string{"glm-4.6", "glm-4.5v"}}); err != nil {
		t.Fatal(err)
	}
}

// The window, output and image support of uni-switch's profile follow its
// model, from the catalog, until the user sets one of them in Snow's own
// settings: theirs then stays through later picks and catalog syncs (#1457).
func TestSnowSyncedKeepsTheUsers(t *testing.T) {
	_, dir := snowHome(t)
	snowUser(t, dir)
	snowSees(t)
	st := settings.Load()
	st.Vision = "off"
	if err := settings.Save(st); err != nil {
		t.Fatal(err)
	}
	a, _ := Find("snow")
	ours := filepath.Join(dir, "profiles", "uni-switch.json")
	if err := a.Apply("model", "uni-switch/relay/glm-4.6"); err != nil {
		t.Fatal(err)
	}
	sc := snowCfg(t, ours)
	// with Vision off, nothing describes GLM-4.6's images: Snow is told
	// it takes none rather than the user's profile's true for glm-5
	if sc["maxContextTokens"] != float64(204800) || sc["maxTokens"] != float64(131072) || sc["supportsVision"] != false {
		t.Fatalf("from the catalog: %v", sc)
	}
	// the user sets the window lower in Snow's own settings, on uni-switch's
	// profile; Snow writes the profile and config.json whole
	body := strings.Replace(readFile(ours), `"maxContextTokens": 204800`, `"maxContextTokens": 128000`, 1)
	os.WriteFile(ours, []byte(body), 0o644)
	os.WriteFile(filepath.Join(dir, "config.json"), []byte(body), 0o644)
	if err := a.Apply("model", "uni-switch/relay/glm-4.5v"); err != nil {
		t.Fatal(err)
	}
	sc = snowCfg(t, ours)
	if sc["maxContextTokens"] != float64(128000) {
		t.Fatalf("the user's window was written over: %v", sc["maxContextTokens"])
	}
	// what the user left alone follows the new model
	if sc["maxTokens"] != float64(16384) || sc["supportsVision"] != true {
		t.Fatalf("not GLM-4.5V's: %v", sc)
	}
	if err := a.Sync(); err != nil {
		t.Fatal(err)
	}
	if sc = snowCfg(t, ours); sc["maxContextTokens"] != float64(128000) {
		t.Fatalf("a sync wrote over the user's window: %v", sc["maxContextTokens"])
	}
	if readFile(filepath.Join(dir, "config.json")) != readFile(ours) {
		t.Fatal("config.json not uni-switch's profile")
	}
	// a model the catalog has no limits for: Snow's own default output,
	// text-only as the gateway takes it, and the window as the user set it
	if err := a.Apply("model", "uni-switch/deepseek/pro"); err != nil {
		t.Fatal(err)
	}
	sc = snowCfg(t, ours)
	if sc["supportsVision"] != false || sc["maxContextTokens"] != float64(128000) || sc["maxTokens"] != float64(snowOutput) {
		t.Fatalf("unknown model: %v", sc)
	}
	// off uni-switch and on again: the user's profile is the start again
	if err := a.Apply("model", ""); err != nil {
		t.Fatal(err)
	}
	if err := a.Apply("model", "uni-switch/relay/glm-4.6"); err != nil {
		t.Fatal(err)
	}
	if sc = snowCfg(t, ours); sc["maxContextTokens"] != float64(204800) {
		t.Fatalf("a window from uni-switch's last profile: %v", sc["maxContextTokens"])
	}
}

// With uni-switch's Vision on, a model that takes no images has them described
// at the gateway, so Snow is told it takes them and sends them on.
func TestSnowVisionThroughUniSwitch(t *testing.T) {
	_, dir := snowHome(t)
	snowUser(t, dir)
	snowSees(t)
	st := settings.Load()
	st.Vision = "relay/glm-4.5v"
	if err := settings.Save(st); err != nil {
		t.Fatal(err)
	}
	a, _ := Find("snow")
	if err := a.Apply("model", "uni-switch/relay/glm-4.6"); err != nil {
		t.Fatal(err)
	}
	if sc := snowCfg(t, filepath.Join(dir, "profiles", "uni-switch.json")); sc["supportsVision"] != true {
		t.Fatalf("supportsVision %v with GLM-4.5V to describe images", sc["supportsVision"])
	}
}

// A vision model of the user's profile comes into uni-switch's only with an
// endpoint and key of its own: on the profile's, it would go to the
// gateway, which doesn't know it, or carry uni-switch's key to the user's
// endpoint.
func TestSnowUsersVisionModel(t *testing.T) {
	for _, c := range []struct {
		name, vision string
		kept         bool
	}{
		{"on the profile's endpoint", `"supportsVision": false, "visionModel": "glm-4.5v", "visionBaseUrl": "", "visionApiKey": "", "visionRequestMethod": "chat"`, false},
		{"own endpoint, the profile's key", `"supportsVision": false, "visionModel": "glm-4.5v", "visionBaseUrl": "https://vision.example.com/v1", "visionApiKey": ""`, false},
		{"own endpoint and key", `"supportsVision": false, "visionModel": "glm-4.5v", "visionBaseUrl": "https://vision.example.com/v1", "visionApiKey": "vkey"`, true},
	} {
		t.Run(c.name, func(t *testing.T) {
			_, dir := snowHome(t)
			snowUser(t, dir)
			prof := strings.Replace(snowDefault, `"supportsVision": true`, c.vision, 1)
			os.WriteFile(filepath.Join(dir, "profiles", "default.json"), []byte(prof), 0o644)
			a, _ := Find("snow")
			if err := a.Apply("model", "uni-switch/deepseek/pro"); err != nil {
				t.Fatal(err)
			}
			sc := snowCfg(t, filepath.Join(dir, "profiles", "uni-switch.json"))
			if !c.kept {
				for _, k := range snowVision {
					if _, ok := sc[k]; ok {
						t.Errorf("%s kept: %v", k, sc[k])
					}
				}
				return
			}
			// the protocol and URL mode it was asked on in the user's profile
			if sc["visionModel"] != "glm-4.5v" || sc["visionApiKey"] != "vkey" || sc["visionRequestMethod"] != "chat" || sc["visionBaseUrlMode"] != "auto" {
				t.Fatalf("the user's own vision model: %v", sc)
			}
			if readFile(filepath.Join(dir, "profiles", "default.json")) != prof {
				t.Fatal("the user's profile rewritten")
			}
		})
	}
}

// Snow's light model (basicModel) is picked apart from its main one on
// uni-switch's profile, stays through a new main model and a sync, and follows
// the main one again when cleared; on a profile of the user's it is
// theirs, and the field offers nothing.
func TestSnowSmallModel(t *testing.T) {
	_, dir := snowHome(t)
	snowUser(t, dir)
	snowSees(t)
	ours := filepath.Join(dir, "profiles", "uni-switch.json")
	a, _ := Find("snow")
	if opts := a.Field("small").Options(a.Values()); len(opts) != 0 {
		t.Fatalf("small offered on the user's profile: %+v", opts)
	}
	if err := a.Apply("small", "uni-switch/relay/glm-4.6"); err == nil {
		t.Fatal("small set on the user's profile")
	}
	if readFile(filepath.Join(dir, "profiles", "default.json")) != snowDefault {
		t.Fatal("the user's profile was written")
	}

	if err := a.Apply("model", "uni-switch/deepseek/pro"); err != nil {
		t.Fatal(err)
	}
	if v := a.Values()["small"]; v != "" {
		t.Fatalf("small follows the model, got %q", v)
	}
	if opts := a.Field("small").Options(a.Values()); len(opts) == 0 {
		t.Fatal("no small options on uni-switch's profile")
	}
	if err := a.Apply("small", "uni-switch/relay/glm-4.6"); err != nil {
		t.Fatal(err)
	}
	sc := snowCfg(t, ours)
	if sc["advancedModel"] != "deepseek/pro" || sc["basicModel"] != "relay/glm-4.6" {
		t.Fatalf("small pick: %v", sc)
	}
	if readFile(filepath.Join(dir, "config.json")) != readFile(ours) {
		t.Fatal("config.json not copied from uni-switch's profile")
	}
	if v := a.Values()["small"]; v != "uni-switch/relay/glm-4.6" {
		t.Fatalf("small read back %q", v)
	}
	// a model of the user's own can't be Snow's light model beside the
	// gateway's
	if err := a.Apply("small", "glm-5"); err == nil {
		t.Fatal("a non-uni-switch small model was taken")
	}

	// a new main model and a sync keep it
	if err := a.Apply("model", "uni-switch/relay/glm-4.5v"); err != nil {
		t.Fatal(err)
	}
	if err := a.Sync(); err != nil {
		t.Fatal(err)
	}
	if sc := snowCfg(t, ours); sc["advancedModel"] != "relay/glm-4.5v" || sc["basicModel"] != "relay/glm-4.6" {
		t.Fatalf("small after a new model and sync: %v", sc)
	}

	// cleared: it follows the main model, and keeps following it
	if err := a.Apply("small", ""); err != nil {
		t.Fatal(err)
	}
	if sc := snowCfg(t, ours); sc["basicModel"] != "relay/glm-4.5v" {
		t.Fatalf("small cleared: %v", sc)
	}
	if err := a.Apply("model", "uni-switch/deepseek/pro"); err != nil {
		t.Fatal(err)
	}
	if sc := snowCfg(t, ours); sc["basicModel"] != "deepseek/pro" {
		t.Fatalf("small doesn't follow the model: %v", sc)
	}

	// Disconnect: the user's profile again, uni-switch's gone
	if err := a.Apply("small", "uni-switch/relay/glm-4.6"); err != nil {
		t.Fatal(err)
	}
	if err := a.Disconnect(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(ours); !os.IsNotExist(err) || snowActive(dir) != "default" {
		t.Fatalf("after Disconnect: %v, active %q", err, snowActive(dir))
	}
}

// Snow's thinking on uni-switch's profile is chatThinking, as Snow's own
// settings write it (ui/pages/configScreen/useConfigState.ts): the
// model's levels are offered, a pick turns it on with that level, and
// clearing it is Snow's default, off. A chatThinking the user brought from
// their own profile is shown as theirs, not written over.
func TestSnowThinking(t *testing.T) {
	_, dir := snowHome(t)
	prof := `{
  "snowcfg": {
    "baseUrl": "https://api.example.com/v1",
    "apiKey": "user-key",
    "requestMethod": "chat",
    "advancedModel": "glm-5",
    "basicModel": "glm-5",
    "chatThinking": {"enabled": true, "reasoning_effort": "medium"}
  }
}`
	os.WriteFile(filepath.Join(dir, "profiles", "default.json"), []byte(prof), 0o644)
	os.WriteFile(filepath.Join(dir, "active-profile.json"), []byte(`{"activeProfile": "default"}`), 0o644)
	os.WriteFile(filepath.Join(dir, "config.json"), []byte(prof), 0o644)
	snowSees(t)
	if err := provider.SetModelEfforts("relay/glm-4.6", []string{"low", "medium", "high", "max"}); err != nil {
		t.Fatal(err)
	}
	ours := filepath.Join(dir, "profiles", "uni-switch.json")
	a, _ := Find("snow")
	// on the user's own profile: theirs, nothing offered
	if v := a.Values()["effort"]; v != "" {
		t.Fatalf("effort on the user's profile: %q", v)
	}
	if opts := a.Field("effort").Options(a.Values()); len(opts) != 0 {
		t.Fatalf("effort offered on the user's profile: %+v", opts)
	}
	if err := a.Apply("effort", "high"); err == nil {
		t.Fatal("effort set on the user's profile")
	}

	if err := a.Apply("model", "uni-switch/relay/glm-4.6"); err != nil {
		t.Fatal(err)
	}
	// the user's own thinking came along with their profile, as they set it
	if v := a.Values()["effort"]; v != "medium" {
		t.Fatalf("the user's chatThinking: %q", v)
	}
	opts := a.Field("effort").Options(a.Values())
	if len(opts) != 5 || opts[0].Value != "" || opts[0].Takes != "off" || opts[4].Value != "max" {
		t.Fatalf("levels: %+v", opts)
	}
	if err := a.Apply("effort", "max"); err != nil {
		t.Fatal(err)
	}
	ct, _ := snowCfg(t, ours)["chatThinking"].(map[string]any)
	if ct["enabled"] != true || ct["reasoning_effort"] != "max" {
		t.Fatalf("chatThinking: %v", ct)
	}
	if readFile(filepath.Join(dir, "config.json")) != readFile(ours) {
		t.Fatal("config.json not copied from uni-switch's profile")
	}
	// kept through a sync and a new model
	if err := a.Sync(); err != nil {
		t.Fatal(err)
	}
	if v := a.Values()["effort"]; v != "max" {
		t.Fatalf("after sync: %q", v)
	}
	// a model with no levels: none offered, the value still shown
	if err := a.Apply("model", "uni-switch/relay/glm-4.5v"); err != nil {
		t.Fatal(err)
	}
	if opts := a.Field("effort").Options(a.Values()); len(opts) != 0 || a.Values()["effort"] != "max" {
		t.Fatalf("no levels: %+v %q", opts, a.Values()["effort"])
	}
	// cleared: Snow's default, thinking off
	if err := a.Apply("effort", ""); err != nil {
		t.Fatal(err)
	}
	if _, ok := snowCfg(t, ours)["chatThinking"]; ok || a.Values()["effort"] != "" {
		t.Fatalf("cleared: %v", snowCfg(t, ours))
	}
	if readFile(filepath.Join(dir, "profiles", "default.json")) != prof {
		t.Fatal("the user's profile was rewritten")
	}
}

// snowFiles is every file under dir, by its path there.
func snowFiles(t *testing.T, dir string) map[string]string {
	t.Helper()
	out := map[string]string{}
	filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			rel, _ := filepath.Rel(dir, p)
			out[rel] = readFile(p)
		}
		return nil
	})
	return out
}

// A model Snow has no profile on, set while on uni-switch's, is refused and
// changes nothing: `uni-switch snow --help` took uni-switch out of Snow (deleted
// profiles/uni-switch.json, put back the active profile) and wrote "--help"
// as the model of the profile it went back to (#1457, blackrion). An
// unknown model through uni-switch is refused the same way, on and off it.
func TestSnowUnknownModelChangesNothing(t *testing.T) {
	_, dir := snowHome(t)
	snowUser(t, dir)
	a, _ := Find("snow")
	if err := a.Field("model").Set("uni-switch/nope/x"); err == nil {
		t.Error("an unknown model through uni-switch taken off uni-switch")
	}
	if err := a.Apply("model", "uni-switch/deepseek/pro"); err != nil {
		t.Fatal(err)
	}
	before := snowFiles(t, dir)
	for _, v := range []string{"--help", "-h", "nonsense-model", "uni-switch/nope/x"} {
		if err := a.Field("model").Set(v); err == nil {
			t.Errorf("%q taken", v)
		}
		if got := snowFiles(t, dir); !reflect.DeepEqual(got, before) {
			t.Fatalf("%q changed Snow's files:\nbefore %v\nafter  %v", v, before, got)
		}
	}
	if got := a.Values()["model"]; got != "uni-switch/deepseek/pro" {
		t.Fatalf("model %q", got)
	}
	// a model of a profile of theirs still takes Snow there
	if err := a.Apply("model", "kimi-k3"); err != nil {
		t.Fatal(err)
	}
	if got := snowActive(dir); got != "work" {
		t.Fatalf("active %q, want work", got)
	}
}

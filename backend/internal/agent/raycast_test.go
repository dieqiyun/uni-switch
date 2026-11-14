package agent

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/dieqiyun/uni-switch/backend/internal/gateway"
	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// raycastUser is a providers.yaml as the manual's examples write one
// (manual.raycast.com/ai/custom-providers): a provider of the user's own
// with comments, a tools block sharing one entry by an anchor.
const raycastUser = `# my Raycast providers
providers:
  - id: lmstudio
    name: LM Studio
    base_url: http://localhost:1234/v1
    # No ` + "`api_keys`" + ` - the local server doesn't require authentication
    models:
      - id: qwen/qwen3-14b # use the model id shown in LM Studio's server page
        name: "Qwen 3 14B"
        context: 32000
        abilities:
          temperature:
            supported: true
          system_message:
            supported: true
          tools:
            supported: true

tools:
  web_search: &tavily
    provider: tavily
    api_key: TAVILY_KEY
  read_page: *tavily
`

// raycastEntry is a provider of providers.yaml with the manual's fields
// alone, so a field of uni-switch's own fails to decode.
type raycastEntry struct {
	ID                   string            `yaml:"id"`
	Name                 string            `yaml:"name"`
	BaseURL              string            `yaml:"base_url"`
	APIKeys              map[string]string `yaml:"api_keys"`
	AdditionalParameters map[string]any    `yaml:"additional_parameters"`
	IncludeUserEmail     bool              `yaml:"include_user_email"`
	Models               []struct {
		ID          string `yaml:"id"`
		Name        string `yaml:"name"`
		Provider    string `yaml:"provider"`
		Description string `yaml:"description"`
		Context     int    `yaml:"context"`
		Abilities   map[string]struct {
			Supported *bool `yaml:"supported"`
		} `yaml:"abilities"`
	} `yaml:"models"`
}

type raycastFile struct {
	Providers []raycastEntry `yaml:"providers"`
	Tools     map[string]any `yaml:"tools"`
}

// raycastRead decodes providers.yaml strictly to the manual's format.
func raycastRead(t *testing.T, path string) raycastFile {
	t.Helper()
	var f raycastFile
	d := yaml.NewDecoder(strings.NewReader(readFile(path)))
	d.KnownFields(true)
	if err := d.Decode(&f); err != nil {
		t.Fatalf("not Raycast's format: %v\n%s", err, readFile(path))
	}
	return f
}

func raycastOf(f raycastFile, id string) *raycastEntry {
	for i := range f.Providers {
		if f.Providers[i].ID == id {
			return &f.Providers[i]
		}
	}
	return nil
}

// Raycast AI reads Custom Providers from ~/.config/raycast/ai/providers.yaml
// (lx_78654 on Discord). uni-switch adds itself as the entry "uni-switch" at the
// gateway's /v1, with its models and their abilities, and leaves the user's
// providers, tools and comments as they are; a sync keeps what the user
// added to uni-switch's entry and writes nothing when nothing changed; off, it
// takes only its own entry out.
func TestRaycastProviders(t *testing.T) {
	home := syncHome(t)
	if err := provider.Save(provider.Provider{ID: "resp", Name: "Resp", Key: "k", Responses: "http://127.0.0.1:1/v1", Models: []string{"gpt-5.5"}}); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(home, ".config", "raycast")
	path := filepath.Join(dir, "ai", "providers.yaml")

	a := raycast(home)
	if a.Path != path {
		t.Fatalf("path %s", a.Path)
	}
	if a.Detected() {
		t.Fatal("found with no ~/.config/raycast")
	}
	if !slices.ContainsFunc(All(), func(x *Agent) bool { return x.ID == "raycast" && x.Name == "Raycast" && x.ListsModels }) {
		t.Fatal("Raycast is not among the agents")
	}
	os.MkdirAll(filepath.Dir(path), 0o755)
	os.WriteFile(path, []byte(raycastUser), 0o600)
	if !a.Detected() {
		t.Fatal("not found with ~/.config/raycast")
	}
	f := a.Field("provider")
	if f == nil || f.Get() != "" {
		t.Fatalf("provider field: %v", f)
	}
	if err := f.Set(uniSwitchID); err != nil {
		t.Fatal(err)
	}
	if f.Get() != uniSwitchID {
		t.Fatalf("get: %q\n%s", f.Get(), readFile(path))
	}

	var user raycastFile
	if err := yaml.Unmarshal([]byte(raycastUser), &user); err != nil {
		t.Fatal(err)
	}
	got := raycastRead(t, path)
	if !reflect.DeepEqual(raycastOf(got, "lmstudio"), raycastOf(user, "lmstudio")) || !reflect.DeepEqual(got.Tools, user.Tools) {
		t.Errorf("the user's provider or tools changed:\n%s", readFile(path))
	}
	for _, s := range []string{"# my Raycast providers", "# No `api_keys`", "# use the model id shown", "&tavily", "*tavily"} {
		if !strings.Contains(readFile(path), s) {
			t.Errorf("%q is gone:\n%s", s, readFile(path))
		}
	}
	m := raycastOf(got, uniSwitchID)
	if m == nil || m.Name != "uni-switch" || m.BaseURL != gatewayV1() || strings.HasSuffix(m.BaseURL, "/chat/completions") ||
		!reflect.DeepEqual(m.APIKeys, map[string]string{uniSwitchID: gateway.TokenFor("raycast")}) {
		t.Fatalf("uni-switch's entry: %+v", m)
	}
	ids := []string{}
	for _, x := range m.Models {
		ids = append(ids, x.ID)
		if x.ID == "" || x.Name == "" {
			t.Errorf("a model without an id or name: %+v", x)
		}
		for _, ab := range []string{"temperature", "vision", "system_message", "tools", "reasoning_effort"} {
			if x.Abilities[ab].Supported == nil {
				t.Errorf("%s doesn't say %s", x.ID, ab)
			}
		}
	}
	for _, id := range []string{"relay/glm-4.6", "resp/gpt-5.5"} {
		if !slices.Contains(ids, id) {
			t.Errorf("%s not listed: %v", id, ids)
		}
	}

	// the user adds to uni-switch's entry: a sync keeps it
	withTheirs := strings.Replace(readFile(path), "    name: uni-switch\n", "    name: uni-switch\n    include_user_email: true # mine\n", 1)
	withTheirs = strings.Replace(withTheirs, "        name: GLM-4.6 · Relay\n", "        name: GLM-4.6 · Relay\n        description: the one I use\n", 1)
	if withTheirs == readFile(path) || !strings.Contains(withTheirs, "the one I use") {
		t.Fatalf("fixture edit didn't apply:\n%s", readFile(path))
	}
	os.WriteFile(path, []byte(withTheirs), 0o600)
	if err := a.Sync(); err != nil {
		t.Fatal(err)
	}
	if readFile(path) != withTheirs {
		t.Errorf("a sync with nothing to change wrote:\n%s", readFile(path))
	}
	// a model gone from the catalog leaves the list; the user's keys stay
	if err := provider.Delete("resp"); err != nil {
		t.Fatal(err)
	}
	if err := a.Sync(); err != nil {
		t.Fatal(err)
	}
	got = raycastRead(t, path)
	m = raycastOf(got, uniSwitchID)
	if m == nil || !m.IncludeUserEmail || !strings.Contains(readFile(path), "# mine") {
		t.Errorf("the user's key in uni-switch's entry is gone:\n%s", readFile(path))
	}
	kept := false
	for _, x := range m.Models {
		if x.ID == "resp/gpt-5.5" {
			t.Errorf("a removed model is still listed")
		}
		kept = kept || x.ID == "relay/glm-4.6" && x.Description == "the one I use"
	}
	if !kept {
		t.Errorf("a model's description of the user's is gone:\n%s", readFile(path))
	}

	// off: uni-switch's entry goes, the rest as it was
	if err := f.Set(""); err != nil {
		t.Fatal(err)
	}
	if f.Get() != "" {
		t.Fatalf("get after off: %q", f.Get())
	}
	got = raycastRead(t, path)
	if len(got.Providers) != 1 || !reflect.DeepEqual(raycastOf(got, "lmstudio"), raycastOf(user, "lmstudio")) || !reflect.DeepEqual(got.Tools, user.Tools) {
		t.Errorf("after off:\n%s", readFile(path))
	}
	for _, s := range []string{"# my Raycast providers", "# No `api_keys`", "&tavily", "*tavily"} {
		if !strings.Contains(readFile(path), s) {
			t.Errorf("%q is gone after off:\n%s", s, readFile(path))
		}
	}
	// off, a sync leaves it off
	if err := a.Sync(); err != nil {
		t.Fatal(err)
	}
	if f.Get() != "" {
		t.Fatalf("sync put it back: %s", readFile(path))
	}
}

// With no providers.yaml (Raycast makes only the template), connecting makes
// one holding uni-switch's entry alone, and Disconnect takes the file it made
// away; a file with a tools block alone gets a providers list beside it.
func TestRaycastMakesAndRemovesItsFile(t *testing.T) {
	home := syncHome(t)
	path := filepath.Join(home, ".config", "raycast", "ai", "providers.yaml")
	template := filepath.Join(filepath.Dir(path), "providers.template.yaml")
	os.MkdirAll(filepath.Dir(path), 0o755)
	os.WriteFile(template, []byte("# Raycast's template\nproviders: []\n"), 0o644)
	a := raycast(home)
	f := a.Field("provider")
	if err := f.Set(uniSwitchID); err != nil {
		t.Fatal(err)
	}
	got := raycastRead(t, path)
	if len(got.Providers) != 1 || got.Providers[0].ID != uniSwitchID || len(got.Providers[0].Models) == 0 {
		t.Fatalf("made:\n%s", readFile(path))
	}
	if err := f.Set(""); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("the file uni-switch made is still there:\n%s", readFile(path))
	}
	if readFile(template) != "# Raycast's template\nproviders: []\n" {
		t.Fatal("the template changed")
	}

	tools := "tools:\n  web_search: raycast # shorthand\n"
	os.WriteFile(path, []byte(tools), 0o600)
	if err := f.Set(uniSwitchID); err != nil {
		t.Fatal(err)
	}
	got = raycastRead(t, path)
	if got.Tools["web_search"] != "raycast" || raycastOf(got, uniSwitchID) == nil || !strings.Contains(readFile(path), "# shorthand") {
		t.Fatalf("beside tools:\n%s", readFile(path))
	}
	if err := f.Set(""); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(path); !bytes.Contains(b, []byte("web_search: raycast # shorthand")) || bytes.Contains(b, []byte("providers")) {
		t.Fatalf("after off:\n%s", b)
	}

	// a providers that isn't a list is the user's to fix: refused, the
	// file untouched
	odd := "providers:\n  lmstudio: {}\n"
	os.WriteFile(path, []byte(odd), 0o600)
	if err := f.Set(uniSwitchID); err == nil || readFile(path) != odd {
		t.Fatalf("a providers map: %v\n%s", err, readFile(path))
	}
}

// Raycast is found by its app on a Mac, before its config folder is made.
func TestRaycastFoundByItsApp(t *testing.T) {
	home := syncHome(t)
	apps := t.TempDir()
	old := appFolders
	appFolders = func() []string { return []string{apps} }
	t.Cleanup(func() { appFolders = old })
	a := raycast(home)
	if a.Detected() {
		t.Fatal("found with no app")
	}
	plist := filepath.Join(apps, "Raycast.app", "Contents", "Info.plist")
	os.MkdirAll(filepath.Dir(plist), 0o755)
	os.WriteFile(plist, []byte(`<plist><dict><key>CFBundleIdentifier</key><string>com.raycast.macos</string></dict></plist>`), 0o644)
	if !a.Detected() {
		t.Fatal("not found by Raycast.app")
	}
}

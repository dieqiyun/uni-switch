package agent

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/pelletier/go-toml/v2"

	"github.com/dieqiyun/uni-switch/backend/internal/catalog"
	"github.com/dieqiyun/uni-switch/backend/internal/edit"
	"github.com/dieqiyun/uni-switch/backend/internal/gateway"
	"github.com/dieqiyun/uni-switch/backend/internal/usage"
)

// testdata/penguin/project_config.toml is the .project_config.toml
// PenguinHarness 0.2.17 wrote itself: its server's first run (the
// command_policy rules, an empty [providers]) and `penguin config model add`
// of a model of the user's own, made its default.
func penguinFixture(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", "penguin", "project_config.toml"))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func penguinOf(t *testing.T) *Agent {
	t.Helper()
	for _, x := range All() {
		if x.ID == "penguin" {
			return x
		}
	}
	t.Fatal("no penguin in All()")
	return nil
}

// penguinFile is what uni-switch's checks read of the file at path.
type penguinFile struct {
	DefaultModel map[string]string         `toml:"default_model"`
	Providers    map[string]map[string]any `toml:"providers"`
	Models       []map[string]any          `toml:"models"`
	Policy       struct {
		Rules []map[string]any `toml:"rules"`
	} `toml:"command_policy"`
}

func readPenguinFile(t *testing.T, path string) penguinFile {
	t.Helper()
	var f penguinFile
	if err := toml.Unmarshal([]byte(readFile(path)), &f); err != nil {
		t.Fatalf("%v:\n%s", err, readFile(path))
	}
	return f
}

// PenguinHarness is wired in its Project's .project_config.toml: uni-switch as
// [providers.uni-switch] (OpenAI Responses, a key naming Penguin), its models
// as [[models]] entries of that provider, and default_model on one. The
// user's models, command policy and the rest stay; Disconnect puts the
// file back as Penguin wrote it.
func TestPenguin(t *testing.T) {
	home := syncHome(t)
	a := penguinOf(t)
	root := filepath.Join(home, ".penguin", "data")
	path := filepath.Join(root, "default_project", ".project_config.toml")
	if a.Name != "PenguinHarness" || a.Icon != "penguin-color" || a.Bin != "penguin" || a.Dir != filepath.Join(home, ".penguin") || a.Path != path || a.Detected() {
		t.Fatalf("%+v", a)
	}
	user := penguinFixture(t)
	writeFile(t, path, user)
	if !a.Detected() || a.Field("model").Get() != "custom/my-own" {
		t.Fatalf("detected %v, model %q", a.Detected(), a.Field("model").Get())
	}
	var vals []string
	for _, o := range a.Field("model").Options(a.Values()) {
		vals = append(vals, o.Value)
	}
	if !slices.Contains(vals, "custom/my-own") || !slices.Contains(vals, "uni-switch/relay/glm-4.6") {
		t.Fatalf("options %v", vals)
	}

	if err := a.Connect(); err != nil {
		t.Fatal(err)
	}
	f := readPenguinFile(t, path)
	if f.DefaultModel["provider"] != "uni-switch" || f.DefaultModel["model_id"] != "relay/glm-4.6" {
		t.Fatalf("default_model %v:\n%s", f.DefaultModel, readFile(path))
	}
	if !strings.HasPrefix(readFile(path), `default_model = { provider = "uni-switch", model_id = "relay/glm-4.6" }`+"\n") {
		t.Fatalf("not spelled as Penguin spells it:\n%s", readFile(path))
	}
	p := f.Providers["uni-switch"]
	if p["base_url"] != gateway.URL()+"/v1" || p["client_type"] != "openai-responses" || p["api_key"] != "uni-switch-penguin" {
		t.Fatalf("provider %v", p)
	}
	var mine, ours []map[string]any
	for _, m := range f.Models {
		if m["provider"] == "uni-switch" {
			ours = append(ours, m)
		} else {
			mine = append(mine, m)
		}
	}
	if len(mine) != 1 || mine[0]["model_id"] != "my-own" || mine[0]["api_key"] != "k1" || len(ours) == 0 {
		t.Fatalf("models:\n%s", readFile(path))
	}
	if ours[0]["model_id"] != "relay/glm-4.6" || ours[0]["context_window"] != int64(204800) || ours[0]["vision"] != false {
		t.Fatalf("entry %v", ours[0])
	}
	// every key is one Penguin's ModelEntry has
	for k := range ours[0] {
		if !slices.Contains([]string{"provider", "model_id", "display_name", "context_window", "vision", "max_tokens", "pricing"}, k) {
			t.Fatalf("%s isn't a key of Penguin's: %v", k, ours[0])
		}
	}
	if len(f.Policy.Rules) != 9 {
		t.Fatalf("command policy: %d rules", len(f.Policy.Rules))
	}
	if c := a.Check(); c != "" {
		t.Fatal(c)
	}
	if got := usage.AgentOf("penguin"); got != "penguin" {
		t.Fatalf("usage: %q", got)
	}

	// the shared key is off, and a sync mends it
	if err := edit.SetTOMLKey(path, "providers.uni-switch", "api_key", gateway.Token); err != nil {
		t.Fatal(err)
	}
	if a.Check() == "" {
		t.Fatal("the shared key reads as Penguin's")
	}
	if err := a.Sync(); err != nil {
		t.Fatal(err)
	}
	if c := a.Check(); c != "" {
		t.Fatal(c)
	}
	// a sync with nothing to change leaves the bytes and the time alone,
	// which Penguin's server would otherwise read again
	was, _ := os.ReadFile(path)
	st, _ := os.Stat(path)
	if err := a.Sync(); err != nil {
		t.Fatal(err)
	}
	if now, _ := os.ReadFile(path); !bytes.Equal(now, was) {
		t.Fatalf("a sync with nothing to change wrote:\n%s", now)
	}
	if st2, _ := os.Stat(path); !st2.ModTime().Equal(st.ModTime()) {
		t.Fatal("a sync with nothing to change touched the file")
	}
	// an entry uni-switch gave is gone: Check says so
	if err := edit.SetTOMLArrayTables(path, "models", "provider", "uni-switch", nil); err != nil {
		t.Fatal(err)
	}
	if a.Check() == "" {
		t.Fatal("a default naming no entry passed")
	}
	if err := a.Sync(); err != nil {
		t.Fatal(err)
	}

	if err := a.Disconnect(); err != nil {
		t.Fatal(err)
	}
	if a.Wired() || a.Field("model").Get() != "custom/my-own" {
		t.Fatalf("still connected: %q", a.Field("model").Get())
	}
	if b := readFile(path); b != user {
		t.Fatalf("not as Penguin wrote it:\n%s", b)
	}
}

// With no config (Penguin installed, its server never run), Connect makes
// one, private as Penguin keeps it, and Disconnect takes it away again.
func TestPenguinRoundTripNoFile(t *testing.T) {
	home := syncHome(t)
	t.Setenv("PENGUIN_HOME", filepath.Join(home, "pg"))
	a := penguin(home)
	for i := 0; i < 2; i++ {
		if err := a.Connect(); err != nil {
			t.Fatal(err)
		}
		if !a.Wired() || a.Check() != "" {
			t.Fatalf("round %d: not connected:\n%s", i, readFile(a.Path))
		}
		if st, err := os.Stat(a.Path); err != nil || runtime.GOOS != "windows" && st.Mode().Perm() != 0o600 {
			t.Fatalf("round %d: %v %v", i, st.Mode(), err)
		}
		if err := a.Disconnect(); err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(a.Path); !os.IsNotExist(err) {
			t.Fatalf("round %d: left:\n%s", i, readFile(a.Path))
		}
	}
}

// What Penguin's own writer does while it is connected stands: it writes
// the whole file back (smol-toml), a model the user added included, and
// a sync and Disconnect keep that model.
func TestPenguinKeepsWhatPenguinWrote(t *testing.T) {
	home := syncHome(t)
	a := penguin(home)
	writeFile(t, a.Path, penguinFixture(t))
	if err := a.Connect(); err != nil {
		t.Fatal(err)
	}
	// as `penguin config model add custom/another` leaves it: one more
	// entry after uni-switch's
	b := readFile(a.Path) + "\n[[models]]\nprovider = \"custom\"\nmodel_id = \"another\"\napi_key = \"k2\"\n"
	writeFile(t, a.Path, b)
	if err := a.Sync(); err != nil {
		t.Fatal(err)
	}
	if got := readFile(a.Path); got != b {
		t.Fatalf("sync changed what was current:\n%s", got)
	}
	if err := a.Disconnect(); err != nil {
		t.Fatal(err)
	}
	f := readPenguinFile(t, a.Path)
	var ids []string
	for _, m := range f.Models {
		ids = append(ids, m["provider"].(string)+"/"+m["model_id"].(string))
	}
	if !slices.Equal(ids, []string{"custom/my-own", "custom/another"}) || f.Providers["uni-switch"] != nil || f.DefaultModel["model_id"] != "my-own" {
		t.Fatalf("after Disconnect:\n%s", readFile(a.Path))
	}
	if strings.Contains(readFile(a.Path), "\n\n\n") {
		t.Fatalf("blank lines left:\n%s", readFile(a.Path))
	}
}

// $PENGUIN_HOME moves the data root, $PENGUIN_PROJECT_ID the Project, as
// Penguin's own resolveRoot and CLI read them; a Project that isn't a
// folder's name is default_project. A distro's Penguin is at its home.
func TestPenguinRootAndProject(t *testing.T) {
	home := syncHome(t)
	own := filepath.Join(home, "own")
	t.Setenv("PENGUIN_HOME", own)
	t.Setenv("PENGUIN_PROJECT_ID", "work")
	if a := penguin(home); a.Path != filepath.Join(own, "work", ".project_config.toml") || a.Dir != home {
		t.Fatalf("%s %s", a.Path, a.Dir)
	}
	for _, bad := range []string{"..", "../x", `a\b`, " "} {
		t.Setenv("PENGUIN_PROJECT_ID", bad)
		if a := penguin(home); a.Path != filepath.Join(own, "default_project", ".project_config.toml") {
			t.Fatalf("%q: %s", bad, a.Path)
		}
	}
	t.Setenv("PENGUIN_HOME", "")
	t.Setenv("PENGUIN_PROJECT_ID", "")
	if a := penguin(home); a.Dir != filepath.Join(home, ".penguin") {
		t.Fatalf("default: %s", a.Dir)
	}
	t.Setenv("PENGUIN_HOME", own)
	root := t.TempDir()
	d := distro{Name: "Ubuntu", Root: root, Home: "/home/me", Has: map[string]bool{"dir:.penguin": true}, Mirrored: true, Running: true}
	a := wslAgent(wslKindOf("penguin"), d)
	if a.ID != "penguin@wsl:Ubuntu" || a.Path != filepath.Join(root, "home", "me", ".penguin", "data", "default_project", ".project_config.toml") {
		t.Fatalf("%+v", a)
	}
}

// A model's entry carries what Penguin's Models page records: its name,
// window, reply cap, whether it reads images and its price in Penguin's
// three buckets, input not read from a cache being cache_write.
func TestPenguinBlocks(t *testing.T) {
	bs := penguinBlocks([]catalog.Model{{ID: "p/m", Name: `M "1"`, Context: 1000, Output: 500, Images: true,
		Price: &catalog.Price{Input: 3, Output: 15, CacheRead: 0.3}}})
	var f penguinFile
	if err := toml.Unmarshal([]byte(bs[0]), &f); err != nil {
		t.Fatalf("%v:\n%s", err, bs[0])
	}
	m := f.Models[0]
	pr, _ := m["pricing"].(map[string]any)
	if m["display_name"] != `M "1"` || m["context_window"] != int64(1000) || m["max_tokens"] != int64(500) || m["vision"] != true ||
		pr["unit"] != "usd_per_mtok" || pr["cache_write"] != float64(3) || pr["cache_read"] != 0.3 || pr["output"] != float64(15) {
		t.Fatalf("%v\n%s", m, bs[0])
	}
}

// Its updates: npm's package, or for install.sh's tarball in ~/.penguin
// its own `penguin update --yes`, which Windows' Penguin refuses.
func TestPenguinUpdate(t *testing.T) {
	self := cliSpecs["penguin"].self
	got := self("/home/me/.local/bin/penguin", "/home/me/.penguin/bin/penguin")
	if runtime.GOOS == "windows" {
		if got != nil {
			t.Fatalf("windows: %q", got)
		}
	} else if !slices.Equal(got, []string{"/home/me/.local/bin/penguin", "update", "--yes"}) {
		t.Fatalf("tarball: %q", got)
	}
	if got := self("/usr/local/bin/penguin", "/usr/local/lib/node_modules/@prismshadow/penguin-cli/dist/bin.js"); got != nil {
		t.Fatalf("npm taken for the tarball: %q", got)
	}
	if !slices.Equal(cliSpecs["penguin"].npm, []string{"@prismshadow/penguin-cli"}) {
		t.Fatal(cliSpecs["penguin"].npm)
	}
	for goos, want := range map[string]string{"darwin": "curl -fsSL https://penguin.ooo/install.sh | sh", "linux": "curl -fsSL https://penguin.ooo/install.sh | sh",
		"windows": "irm https://penguin.ooo/install.ps1 | iex"} {
		if c := vendorInstall["penguin"](goos); len(c) != 1 || c[0].Command != want {
			t.Fatalf("%s: %+v", goos, c)
		}
	}
}

// Setting the model to none while on uni-switch's goes back to the user's own
// default if Penguin still has it, else leaves none; uni-switch's entries,
// provider and a vision model among them go either way.
func TestPenguinModelNone(t *testing.T) {
	home := syncHome(t)
	a := penguin(home)
	writeFile(t, a.Path, penguinFixture(t))
	m := a.Field("model")
	for _, gone := range []bool{false, true} {
		if err := m.Set("uni-switch/relay/glm-4.6"); err != nil {
			t.Fatal(err)
		}
		if err := edit.SetTOMLTop(a.Path, edit.KV{Path: "vision_model", Value: edit.Raw(`{ provider = "uni-switch", model_id = "relay/glm-4.6" }`)}); err != nil {
			t.Fatal(err)
		}
		if gone {
			if err := edit.SetTOMLArrayTables(a.Path, "models", "provider", "custom", nil); err != nil {
				t.Fatal(err)
			}
		}
		if err := m.Set(""); err != nil {
			t.Fatal(err)
		}
		want := "custom/my-own"
		if gone {
			want = ""
		}
		b := readFile(a.Path)
		if m.Get() != want || strings.Contains(b, "uni-switch") {
			t.Fatalf("gone %v: %q:\n%s", gone, m.Get(), b)
		}
	}
	if err := m.Set("nope"); err == nil {
		t.Fatal("a value with no provider was taken")
	}
}

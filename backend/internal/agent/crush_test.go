package agent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/edit"
)

// crushHome is effortHome with Crush's data folder in the sandbox too, and
// the path of the data file Crush reads there: its GlobalConfigData, which
// is ~/.local/share/crush/crush.json, or %LOCALAPPDATA%\crush\crush.json on
// Windows.
func crushHome(t *testing.T) (home, data string) {
	t.Helper()
	home = effortHome(t)
	t.Setenv("XDG_DATA_HOME", "")
	t.Setenv("LOCALAPPDATA", filepath.Join(home, "AppData", "Local"))
	data = filepath.Join(home, ".local", "share", "crush", "crush.json")
	if runtime.GOOS == "windows" {
		data = filepath.Join(home, "AppData", "Local", "crush", "crush.json")
	}
	return home, data
}

// Crush saves what its own model picker chooses to its data file, and reads
// that after crush.json, so a pick there wins over one in crush.json. Once
// somebody has picked a model in Crush (its first run asks for one), the
// model uni-switch sets has to go where Crush reads it last, and what uni-switch
// shows has to be what Crush will use.
func TestCrushPickWhereCrushReadsIt(t *testing.T) {
	home, data := crushHome(t)
	cr := crush(home, filepath.Join(home, ".config"))
	writeFile(t, data, `{"models":{"large":{"model":"m-mine","provider":"mine"}}}`)

	large := cr.Field("model")
	if got := large.Get(); got != "mine/m-mine" {
		t.Fatalf("large reads %q, not the pick Crush uses", got)
	}
	if err := large.Set("uni-switch/deepseek/pro"); err != nil {
		t.Fatal(err)
	}
	if p, _ := edit.GetJSON(data, "models.large.provider"); p != "uni-switch" {
		t.Fatalf("Crush still picks %q from its data file:\n%s", p, readFile(data))
	}
	if m, _ := edit.GetJSON(data, "models.large.model"); m != "deepseek/pro" {
		t.Fatalf("data file model %q:\n%s", m, readFile(data))
	}
	if got := large.Get(); got != "uni-switch/deepseek/pro" {
		t.Fatalf("large reads %q", got)
	}
	// the provider stays where it was, with the library's MCP servers
	if _, ok := edit.GetJSON(cr.Path, "providers.uni-switch.base_url"); !ok {
		t.Fatalf("no uni-switch provider in %s:\n%s", cr.Path, readFile(cr.Path))
	}
	if cr.Check() != "" {
		t.Fatalf("check: %s", cr.Check())
	}

	// the effort is kept with the pick
	effort := cr.Field("effort")
	if err := effort.Set("high"); err != nil {
		t.Fatal(err)
	}
	if e, _ := edit.GetJSON(data, "models.large.reasoning_effort"); e != "high" || effort.Get() != "high" {
		t.Fatalf("effort %q:\n%s", effort.Get(), readFile(data))
	}

	small := cr.Field("small")
	if err := small.Set("uni-switch/deepseek/flash"); err != nil {
		t.Fatal(err)
	}
	// unset, a pick is gone from both files, and the provider goes with
	// the last pick on it
	if err := large.Set(""); err != nil {
		t.Fatal(err)
	}
	if large.Get() != "" || effort.Get() != "" {
		t.Fatalf("large %q, effort %q after unset:\n%s", large.Get(), effort.Get(), readFile(data))
	}
	if _, ok := edit.GetJSON(cr.Path, "providers.uni-switch"); !ok {
		t.Fatal("the provider went while small still uses it")
	}
	if err := small.Set(""); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{cr.Path, data} {
		if _, ok := edit.GetJSON(f, "models.small"); ok {
			t.Fatalf("a small pick is left in %s:\n%s", f, readFile(f))
		}
	}
	if _, ok := edit.GetJSON(cr.Path, "providers.uni-switch"); ok {
		t.Fatalf("the provider is left with no pick on it:\n%s", readFile(cr.Path))
	}
}

// A pick uni-switch wrote to crush.json before it wrote Crush's data file still
// reads as set where Crush has picked nothing itself, and one Crush's own
// pick hides reads as that pick, since it is the one Crush uses.
func TestCrushPickInCrushJSON(t *testing.T) {
	home, data := crushHome(t)
	cr := crush(home, filepath.Join(home, ".config"))
	if cr.Path == data {
		t.Skip("crush.json is Crush's data file here")
	}
	writeFile(t, cr.Path, `{"providers":{"uni-switch":{"base_url":"x"}},"models":{"large":{"provider":"uni-switch","model":"deepseek/pro"}}}`)
	if got := cr.Field("model").Get(); got != "uni-switch/deepseek/pro" {
		t.Fatalf("large reads %q", got)
	}
	writeFile(t, data, `{"models":{"large":{"model":"m-mine","provider":"mine"}}}`)
	if got := cr.Field("model").Get(); got != "mine/m-mine" {
		t.Fatalf("large reads %q, not the pick Crush uses", got)
	}
}

// Crush's picker saves the whole model object in its data file: the model
// and the settings it picked for that model. A model uni-switch picks takes the
// whole object's place, so none of those settings ride along to it (a
// max_tokens meant for another model would go upstream with uni-switch's), and
// uni-switch's effort row doesn't show an effort uni-switch never set.
func TestCrushPickReplacesCrushsModelObject(t *testing.T) {
	home, data := crushHome(t)
	cr := crush(home, filepath.Join(home, ".config"))
	writeFile(t, data, `{
  "models": {
    "large": {"model":"gpt-5","provider":"openai","reasoning_effort":"medium","max_tokens":128000,"think":true,"temperature":0.2,"top_p":0.9,"provider_options":{"x":1}},
    "small": {"model":"gpt-5-mini","provider":"openai","max_tokens":4096,"think":true}
  },
  "providers": {"openai": {"api_key": "sk-keep"}},
  "recent_models": {"large": [{"model":"gpt-5","provider":"openai"}]}
}`)

	if err := cr.Field("model").Set("uni-switch/deepseek/pro"); err != nil {
		t.Fatal(err)
	}
	if err := cr.Field("small").Set("uni-switch/deepseek/flash"); err != nil {
		t.Fatal(err)
	}
	for _, typ := range []string{"large", "small"} {
		raw, _ := edit.GetJSON(data, "models."+typ)
		var obj map[string]any
		if err := json.Unmarshal([]byte(raw), &obj); err != nil {
			t.Fatalf("models.%s: %v\n%s", typ, err, readFile(data))
		}
		if len(obj) != 2 || obj["provider"] != "uni-switch" {
			t.Fatalf("models.%s after uni-switch's pick: %s", typ, raw)
		}
	}
	if e := cr.Field("effort").Get(); e != "" {
		t.Fatalf("effort row shows %q, which uni-switch never set", e)
	}
	// the rest of the data file is Crush's and stays
	if k, _ := edit.GetJSON(data, "providers.openai.api_key"); k != "sk-keep" {
		t.Fatalf("api key gone:\n%s", readFile(data))
	}
	if _, ok := edit.GetJSON(data, "recent_models.large"); !ok {
		t.Fatalf("recent_models gone:\n%s", readFile(data))
	}

	// an effort uni-switch set stays with its next pick on uni-switch
	if err := cr.Field("effort").Set("high"); err != nil {
		t.Fatal(err)
	}
	if err := cr.Field("model").Set("uni-switch/deepseek/max"); err != nil {
		t.Fatal(err)
	}
	if e := cr.Field("effort").Get(); e != "high" {
		t.Fatalf("uni-switch's own effort %q after a new pick:\n%s", e, readFile(data))
	}

	// once Crush's picker has saved its own object again, uni-switch's next pick
	// drops all of it, the effort Crush saved too
	writeFile(t, data, `{"models":{"large":{"model":"deepseek/pro","provider":"uni-switch","reasoning_effort":"low","max_tokens":64000}}}`)
	if err := cr.Field("model").Set("uni-switch/deepseek/max"); err != nil {
		t.Fatal(err)
	}
	if raw, _ := edit.GetJSON(data, "models.large"); strings.Contains(raw, "max_tokens") || strings.Contains(raw, "reasoning_effort") {
		t.Fatalf("Crush's settings ride along: %s", raw)
	}
}

// Crush keeps API keys in its data file, so when uni-switch is first to make it
// the file is readable by its owner only.
func TestCrushDataFileMadePrivate(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("no unix permission bits")
	}
	home, data := crushHome(t)
	cr := crush(home, filepath.Join(home, ".config"))
	if err := cr.Field("model").Set("uni-switch/deepseek/pro"); err != nil {
		t.Fatal(err)
	}
	st, err := os.Stat(data)
	if err != nil {
		t.Fatal(err)
	}
	if st.Mode().Perm() != 0o600 {
		t.Fatalf("data file mode %v", st.Mode().Perm())
	}
}

// Crush in a WSL distro has the same data file over its crush.json, in the
// distro's home: <distro home>/.local/share/crush/crush.json, whatever
// Windows' own XDG_DATA_HOME or LOCALAPPDATA say. A pick made in uni-switch goes
// there and is read back from there.
func TestCrushInWSLPicksInTheDistrosDataFile(t *testing.T) {
	home, _ := crushHome(t)
	t.Setenv("XDG_DATA_HOME", filepath.Join(home, "windows-data"))
	root := t.TempDir()
	d := distro{Name: "Ubuntu", Home: "/home/me", Root: root, Running: true, Mirrored: true}
	cr := crushIn(d.place("crush@wsl:Ubuntu"))
	data := d.local("/home/me/.local/share/crush/crush.json")
	if want := d.local("/home/me/.config/crush/crush.json"); cr.Path != want {
		t.Fatalf("config %s, want %s", cr.Path, want)
	}
	writeFile(t, data, `{"models":{"large":{"model":"m-mine","provider":"mine"}}}`)

	large := cr.Field("model")
	if got := large.Get(); got != "mine/m-mine" {
		t.Fatalf("large reads %q, not the pick in the distro's data file", got)
	}
	if err := large.Set("uni-switch/deepseek/pro"); err != nil {
		t.Fatal(err)
	}
	if p, _ := edit.GetJSON(data, "models.large.provider"); p != "uni-switch" {
		t.Fatalf("the distro's Crush still picks %q:\n%s", p, readFile(data))
	}
	if got := large.Get(); got != "uni-switch/deepseek/pro" {
		t.Fatalf("large reads %q", got)
	}
	for _, other := range []string{
		filepath.Join(home, "windows-data", "crush", "crush.json"),
		filepath.Join(home, ".local", "share", "crush", "crush.json"),
		filepath.Join(home, "AppData", "Local", "crush", "crush.json"),
	} {
		if _, err := os.Stat(other); err == nil {
			t.Fatalf("the distro's pick went to this machine's %s", other)
		}
	}
}

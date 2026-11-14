package agent

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/gateway"
	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// newMaxUser is the `app` row as NewMax 1.1.22 saves it: JSON.stringify of
// its settings, compact, a provider of the user's own, their order and
// their pick.
const newMaxUser = `{"providerId":"deepseek","model":"deepseek-chat","theme":"dark","language":"zh-CN","providers":[{"id":"deepseek","name":"DeepSeek","baseUrl":"https://api.deepseek.com/anthropic","apiKey":"sk-u","apiKeys":["sk-u"],"apiKeyRotationEnabled":false,"apiKeyField":"ANTHROPIC_AUTH_TOKEN","apiFormat":"anthropic","models":["deepseek-chat"],"enabledModels":["deepseek-chat"],"enabled":true,"canDelete":true}],"providerOrder":["default","deepseek"],"settingsConfig":{"env":{"FOO":"1"}},"autoUpdate":true}`

// newMaxDB makes NewMax's database at path as it does on its first start:
// WAL, its settings table, and the row app when one is given.
func newMaxDB(t *testing.T, path, app string) {
	t.Helper()
	os.MkdirAll(filepath.Dir(path), 0o755)
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, q := range []string{`PRAGMA journal_mode = WAL`, `CREATE TABLE IF NOT EXISTS settings (key TEXT PRIMARY KEY, value TEXT NOT NULL)`, `INSERT INTO settings (key, value) VALUES ('window', '{"w":1200}')`} {
		if _, err := db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	if app != "" {
		if _, err := db.Exec(`INSERT INTO settings (key, value) VALUES ('app', ?)`, app); err != nil {
			t.Fatal(err)
		}
	}
}

// newMaxRow is the row app as it is now, and whether there is one.
func newMaxRow(t *testing.T, path string) (string, bool) {
	t.Helper()
	b, had, err := newMaxReadApp(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b), had
}

// newMaxuni-switch is uni-switch's entry in the row app, checked as NewMax's
// mergeProviders reads one.
func newMaxUniSwitch(t *testing.T, app string) (e newMaxEntry, raw map[string]json.RawMessage) {
	t.Helper()
	var doc struct {
		Providers []json.RawMessage `json:"providers"`
	}
	if err := json.Unmarshal([]byte(app), &doc); err != nil {
		t.Fatalf("not JSON NewMax reads: %v\n%s", err, app)
	}
	for _, p := range doc.Providers {
		var id struct{ ID string }
		json.Unmarshal(p, &id)
		if id.ID == newMaxID {
			json.Unmarshal(p, &e)
			json.Unmarshal(p, &raw)
			return e, raw
		}
	}
	t.Fatalf("no uni-switch entry in %s", app)
	return
}

// NewMax (newmax.cc) lists the providers of its settings row in its model
// picker. uni-switch adds itself as "uni-switch-gateway" on the gateway's Anthropic
// Messages with the key naming NewMax, keeps every other byte of the row,
// writes nothing when nothing changed, keeps the user's name, off and
// NewMax's own fields on its entry through a sync, and off gives back the
// row it found, byte for byte.
func TestNewMaxProvider(t *testing.T) {
	home := syncHome(t)
	t.Setenv("NEWMAX_USER_DATA_DIR", "")
	if err := provider.Save(provider.Provider{ID: "deepseek", Name: "DeepSeek", Chat: "https://api.deepseek.com/v1", Key: "k", Models: []string{"pro", "flash"}}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { newMaxRunning = func() bool { return false } })
	newMaxRunning = func() bool { return false }

	a := newMax(home)
	if !slices.ContainsFunc(All(), func(x *Agent) bool { return x.ID == "newmax" && x.Name == "NewMax" }) {
		t.Fatal("NewMax is not among the agents")
	}
	if a.Path != filepath.Join(home, ".newmax", "newmax.db") {
		t.Fatalf("path %s", a.Path)
	}
	newMaxDB(t, a.Path, newMaxUser)
	if !a.Detected() {
		t.Fatal("not found with its data folder")
	}
	f := a.Field("provider")
	if f.Get() != "" {
		t.Fatalf("get: %q", f.Get())
	}
	if err := f.Set(uniSwitchID); err != nil {
		t.Fatal(err)
	}
	if f.Get() != uniSwitchID {
		t.Fatalf("get after set: %q", f.Get())
	}

	gw := gateway.URL()
	app, _ := newMaxRow(t, a.Path)
	e, _ := newMaxUniSwitch(t, app)
	key := agentKeyAt("newmax", gw)
	if e.BaseURL != gw || strings.HasSuffix(e.BaseURL, "/v1") || e.APIKey != key || !slices.Equal(e.APIKeys, []string{key}) ||
		e.APIFormat != "anthropic" || e.APIKeyField != "ANTHROPIC_AUTH_TOKEN" || !e.Enabled || e.Name != "uni-switch" {
		t.Errorf("entry: %+v", e)
	}
	var want []string
	for _, m := range uniSwitchModels("newmax") {
		want = append(want, m.ID)
	}
	if len(want) < 2 || !slices.Equal(e.Models, want) || !slices.Equal(e.EnabledModels, want) {
		t.Errorf("models %v enabled %v, want %v", e.Models, e.EnabledModels, want)
	}
	// the rest of the row as it was, uni-switch's entry after the user's and
	// its id after theirs
	if !strings.HasPrefix(app, newMaxUser[:strings.Index(newMaxUser, `}],"providerOrder"`)+1]+`,{"id":"uni-switch-gateway",`) ||
		!strings.HasSuffix(app, `],"providerOrder":["default","deepseek","uni-switch-gateway"],"settingsConfig":{"env":{"FOO":"1"}},"autoUpdate":true}`) {
		t.Errorf("the row around uni-switch's entry changed:\n%s\n%s", newMaxUser, app)
	}

	// nothing changed: nothing written
	if err := a.Sync(); err != nil {
		t.Fatal(err)
	}
	if err := f.Set(uniSwitchID); err != nil {
		t.Fatal(err)
	}
	if again, _ := newMaxRow(t, a.Path); again != app {
		t.Errorf("rewritten with nothing changed:\n%s\n%s", app, again)
	}

	// renamed, turned off and tested in NewMax, a model gone from the
	// list: a sync keeps the user's and NewMax's, and puts the model back
	at := strings.Index(app, `{"id":"uni-switch-gateway",`)
	mine := strings.Replace(app[at:], `{"id":"uni-switch-gateway","name":"uni-switch",`, `{"id":"uni-switch-gateway","name":"My uni-switch","modelCapabilities":{"x":{"vision":true}},`, 1)
	mine = strings.Replace(mine, `"enabled":true,"canDelete":true`, `"enabled":false,"canDelete":true`, 1)
	mine = strings.Replace(mine, `"models":["`+want[0]+`",`, `"models":[`, 1)
	edited := app[:at] + mine
	if edited == app || strings.Count(edited, "My uni-switch") != 1 {
		t.Fatalf("fixture: %s", edited)
	}
	newMaxSetRow(t, a.Path, edited)
	if err := a.Sync(); err != nil {
		t.Fatal(err)
	}
	app, _ = newMaxRow(t, a.Path)
	e, raw := newMaxUniSwitch(t, app)
	if e.Name != "My uni-switch" || e.Enabled || string(raw["modelCapabilities"]) != `{"x":{"vision":true}}` {
		t.Errorf("the user's name, off and NewMax's fields not kept: %s", app)
	}
	if !slices.Equal(e.Models, want) {
		t.Errorf("models after sync %v, want %v", e.Models, want)
	}

	// picked in NewMax, then off: uni-switch's entry and id go, the pick goes
	// back to NewMax's default, and the rest is the row it found
	newMaxSetRow(t, a.Path, strings.Replace(app, `{"providerId":"deepseek","model":"deepseek-chat",`, `{"providerId":"uni-switch-gateway","model":"`+want[1]+`",`, 1))
	if err := f.Set(""); err != nil {
		t.Fatal(err)
	}
	if f.Get() != "" {
		t.Fatalf("get after off: %q", f.Get())
	}
	got, _ := newMaxRow(t, a.Path)
	if wantOff := strings.Replace(newMaxUser, `{"providerId":"deepseek","model":"deepseek-chat",`, `{"providerId":"default","model":"default",`, 1); got != wantOff {
		t.Errorf("after off:\n%s\nwant\n%s", got, wantOff)
	}
	// and on then off, untouched since, is the row byte for byte
	newMaxSetRow(t, a.Path, newMaxUser)
	if err := f.Set(uniSwitchID); err != nil {
		t.Fatal(err)
	}
	if err := f.Set(""); err != nil {
		t.Fatal(err)
	}
	if got, _ := newMaxRow(t, a.Path); got != newMaxUser {
		t.Errorf("on then off:\n%s\nwant\n%s", got, newMaxUser)
	}
	// a sync of an entry that isn't there writes none
	if err := a.Sync(); err != nil {
		t.Fatal(err)
	}
	if got, _ := newMaxRow(t, a.Path); got != newMaxUser {
		t.Errorf("a sync off wrote:\n%s", got)
	}
}

func newMaxSetRow(t *testing.T, path, app string) {
	t.Helper()
	if err := newMaxEdit(path, func([]byte, bool) ([]byte, error) { return []byte(app), nil }); err != nil {
		t.Fatal(err)
	}
}

// NewMax started once and left at its defaults has no row app: uni-switch
// makes one with its entry alone (NewMax lays its defaults and presets
// under it), and off takes the row out again. Before NewMax has made its
// database at all, uni-switch says to open it once.
func TestNewMaxMakesItsRow(t *testing.T) {
	home := syncHome(t)
	t.Setenv("NEWMAX_USER_DATA_DIR", "")
	t.Cleanup(func() { newMaxRunning = func() bool { return false } })
	newMaxRunning = func() bool { return false }
	a := newMax(home)
	f := a.Field("provider")

	if err := f.Set(uniSwitchID); !errors.Is(err, errNewMaxNoDB) {
		t.Fatalf("no database: %v", err)
	}
	if _, err := os.Stat(a.Path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("a database made: %v", err)
	}

	newMaxDB(t, a.Path, "")
	if err := f.Set(uniSwitchID); err != nil {
		t.Fatal(err)
	}
	app, had := newMaxRow(t, a.Path)
	if !had || !strings.HasPrefix(app, `{"providers":[{"id":"uni-switch-gateway",`) || !strings.HasSuffix(app, `}],"providerOrder":["default","uni-switch-gateway"]}`) {
		t.Fatalf("row made: %s", app)
	}
	newMaxUniSwitch(t, app)
	if err := f.Set(""); err != nil {
		t.Fatal(err)
	}
	if app, had := newMaxRow(t, a.Path); had {
		t.Errorf("row left after off: %s", app)
	}
	// NewMax's other rows stay
	db, closeDB, err := openNewMaxForTest(a.Path)
	if err != nil {
		t.Fatal(err)
	}
	defer closeDB()
	var w string
	if err := db.QueryRow(`SELECT value FROM settings WHERE key = 'window'`).Scan(&w); err != nil || w != `{"w":1200}` {
		t.Errorf("window row: %q %v", w, err)
	}
}

func openNewMaxForTest(path string) (*sql.DB, func(), error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, nil, err
	}
	return db, func() { db.Close() }, nil
}

// A row NewMax wouldn't read as its settings — not JSON, providers that
// aren't a list — is left alone, and nothing is changed while NewMax runs,
// as it writes its settings back over uni-switch's when one changes.
func TestNewMaxLeavesAlone(t *testing.T) {
	home := syncHome(t)
	t.Setenv("NEWMAX_USER_DATA_DIR", "")
	t.Cleanup(func() { newMaxRunning = func() bool { return false } })
	newMaxRunning = func() bool { return false }
	a := newMax(home)
	f := a.Field("provider")
	newMaxDB(t, a.Path, "")
	for _, bad := range []string{`{"providers":`, `[]`, `{"providers":{"id":"x"}}`, `{"providers":[],"providerOrder":"default"}`} {
		newMaxSetRow(t, a.Path, bad)
		if err := f.Set(uniSwitchID); err == nil {
			t.Errorf("%s: no error", bad)
		}
		if got, _ := newMaxRow(t, a.Path); got != bad {
			t.Errorf("%s: written %s", bad, got)
		}
		if f.Get() != "" {
			t.Errorf("%s: read as on", bad)
		}
	}

	newMaxSetRow(t, a.Path, newMaxUser)
	newMaxRunning = func() bool { return true }
	if err := f.Set(uniSwitchID); err != errNewMaxRunning {
		t.Fatalf("set while running: %v", err)
	}
	if got, _ := newMaxRow(t, a.Path); got != newMaxUser {
		t.Errorf("written while running: %s", got)
	}
	newMaxRunning = func() bool { return false }
	if err := f.Set(uniSwitchID); err != nil {
		t.Fatal(err)
	}
	on, _ := newMaxRow(t, a.Path)
	newMaxRunning = func() bool { return true }
	if err := f.Set(""); err != errNewMaxRunning {
		t.Fatalf("off while running: %v", err)
	}
	if got, _ := newMaxRow(t, a.Path); got != on {
		t.Errorf("written while running: %s", got)
	}
}

// NewMax's data folder is $NEWMAX_USER_DATA_DIR when it names one, else
// ~/.newmax, on every system.
func TestNewMaxDir(t *testing.T) {
	home := syncHome(t)
	t.Setenv("NEWMAX_USER_DATA_DIR", "")
	if got := newMax(home).Dir; got != filepath.Join(home, ".newmax") {
		t.Errorf("default: %s", got)
	}
	d := filepath.Join(home, "nm-data")
	t.Setenv("NEWMAX_USER_DATA_DIR", d)
	if a := newMax(home); a.Dir != d || a.Path != filepath.Join(d, "newmax.db") {
		t.Errorf("set: %s %s", a.Dir, a.Path)
	}
	t.Setenv("NEWMAX_USER_DATA_DIR", "rel")
	if got := newMax(home).Dir; got != filepath.Join(home, ".newmax") {
		t.Errorf("relative: %s", got)
	}
}

// NewMax keeps 20 models of a provider (a primary and 19 back-ups); uni-switch
// writes the first 20, so what it wrote and what NewMax saves back agree.
func TestNewMaxModelCap(t *testing.T) {
	syncHome(t)
	var ms []string
	for i := range 30 {
		ms = append(ms, fmt.Sprintf("m%02d", i))
	}
	if err := provider.Save(provider.Provider{ID: "many", Name: "Many", Chat: "https://api.example/v1", Key: "k", Models: ms}); err != nil {
		t.Fatal(err)
	}
	if n := len(uniSwitchModels("newmax")); n <= newMaxMaxModels {
		t.Fatalf("fixture: %d models", n)
	}
	e := newMaxEntryFor(gateway.URL())
	if len(e.Models) != newMaxMaxModels || !slices.Equal(e.EnabledModels, e.Models) {
		t.Errorf("%d models, %d enabled", len(e.Models), len(e.EnabledModels))
	}
}

// NewMax runs, for the refusal, when its program does: the Mac app's
// executable wherever the bundle is (a dmg's volume name has spaces), its
// helpers, the Linux build's newmax; a shell working in a folder named
// newmax, or a file of that name in an editor, is not NewMax.
func TestNewMaxProcess(t *testing.T) {
	match := func(cmd string) bool {
		for _, p := range newMaxProc {
			if regexp.MustCompile(p).MatchString(cmd) {
				return true
			}
		}
		return false
	}
	for _, c := range []struct {
		cmd  string
		want bool
	}{
		{"/Applications/NewMax.app/Contents/MacOS/NewMax", true},
		{"/Volumes/NewMax 1.1.22-arm64/NewMax.app/Contents/MacOS/NewMax --remote-debugging-port=1", true},
		{"/Applications/NewMax.app/Contents/Frameworks/NewMax Helper (Renderer).app/Contents/MacOS/NewMax Helper (Renderer) --type=renderer", true},
		{"/opt/NewMax/newmax --no-sandbox", true},
		{"/bin/zsh -c cd /Users/me/projects/newmax && go test ./...", false},
		{"vim /home/me/newmax notes.txt", false},
		{"/usr/bin/newmaxd", false},
	} {
		if got := match(c.cmd); got != c.want {
			t.Errorf("%q: running %v, want %v", c.cmd, got, c.want)
		}
	}
}

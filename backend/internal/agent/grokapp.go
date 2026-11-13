package agent

// Grok App (RongleCat/grok-app, Tauri) is a desktop front end for the grok
// CLI, which it starts over ACP. Shared mode, the default, uses ~/.grok,
// the same home as Grok Build. Independent mode, which Use on a custom
// provider turns on, uses agent-home. The Library writes that home's
// AGENTS.md, [mcp_servers] and skills/ (and Grok App still reads
// ~/.agents/skills). Its models are its own, though: its composer lists
// only Grok Build's official catalog
// (models_cache.json), never a [model."<id>"] table of config.toml, and a
// model of the user's choosing is a "provider channel" of its Settings ›
// Providers page, a [model.<id>] table with the App's own keys in
// agent-home/config.toml under its data folder. That file is the CLI's
// config.toml while a channel is on: [models] default naming a channel is
// what the App takes as the route, and it then starts the CLI with
// GROK_HOME at agent-home, switching itself to its independent mode
// (grok-app's providers.rs, ensure_independent_for_custom_route).
//
// So uni-switch adds itself as the channel "uni-switch", written as the App writes
// one (upsert_custom_provider): the gateway as base_url with Grok App's own
// key, chat completions, app_models a JSON list of uni-switch's models with each
// one's context window, images and effort levels, so Accounts › Custom
// providers lists it and the composer can offer the models and their
// efforts. It does not set [models] default. That key is the route, and
// making it "uni-switch" takes the App over: the App then drops its own sign-in
// and the session no longer runs. The route stays whatever the App had.
// Pressing Use on the provider in the App is what switches the route, and
// that choice is the App's. A uni-switch that already set the route puts the
// old one back once, on the next sync or connect. A model the user picks in
// the App's composer since is theirs and stays through a sync.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"

	"github.com/dieqiyun/uni-switch/backend/internal/appdir"
	"github.com/dieqiyun/uni-switch/backend/internal/edit"
)

// grokAppChannel is uni-switch's channel's table in the App's config.toml.
const grokAppChannel = "model." + uniSwitchID

// what Grok App says after a change (notice.go)
var noticeGrokApp = newNotice("uni-switch is a custom provider in Grok App's Settings › Account. A chat can pick uni-switch's models in its model picker, and Grok App stays on its current route until you press Use on uni-switch there.")

// grokAppReleased marks that this uni-switch has already put back a route an
// older one set to uni-switch. One restore: a Use the user presses after that
// stays until they disconnect.
const grokAppReleased = "grok-app:released-route"

// grokAppOS is the system grokAppDir looks for the App's data on; a test
// sets it.
var grokAppOS = runtime.GOOS

// grokAppDir is the App's data folder: $GROK_APP_HOME, else where the
// directories crate's ProjectDirs("com", "grokapp", "grok-app") puts its
// data (grok-app's paths.rs app_data_root) — on a Mac ~/Library/Application
// Support/com.grokapp.grok-app, on Windows %APPDATA%\grokapp\grok-app\data,
// on Linux $XDG_DATA_HOME/grok-app. The App's README names ~/.grok-app on
// Linux and %APPDATA%\grokapp\grok-app on Windows, and the App falls back to
// ~/.grok-app (%APPDATA%\grok-app) itself, so those are looked for too; the
// first that is there wins, else the App's own default.
func grokAppDir(home, goos string) string {
	if d := strings.TrimSpace(appdir.Getenv("GROK_APP_HOME")); d != "" {
		return d
	}
	var dirs []string
	switch goos {
	case "darwin":
		dirs = []string{filepath.Join(home, "Library", "Application Support", "com.grokapp.grok-app"), filepath.Join(home, ".grok-app")}
	case "windows":
		app := appdir.Getenv("APPDATA")
		if app == "" {
			app = filepath.Join(home, "AppData", "Roaming")
		}
		dirs = []string{filepath.Join(app, "grokapp", "grok-app", "data"), filepath.Join(app, "grokapp", "grok-app"), filepath.Join(app, "grok-app")}
	default:
		data := appdir.Getenv("XDG_DATA_HOME")
		if data == "" {
			data = filepath.Join(home, ".local", "share")
		}
		dirs = []string{filepath.Join(data, "grok-app"), filepath.Join(home, ".grok-app")}
	}
	for _, d := range dirs {
		if isDir(d) {
			return d
		}
	}
	return dirs[0]
}

// grokAppEffort is one effort level of a channel's model, as the App's
// ProviderEffortEntry reads it.
type grokAppEffort struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	IsDefault bool   `json:"isDefault"`
}

// grokAppModel is one entry of a channel's app_models, as the App's
// ProviderModelEntry reads it.
type grokAppModel struct {
	ID             string          `json:"id"`
	Name           string          `json:"name"`
	ContextWindow  int             `json:"contextWindow,omitempty"`
	SupportsVision *bool           `json:"supportsVision,omitempty"`
	Efforts        []grokAppEffort `json:"efforts,omitempty"`
}

// grokAppWant is uni-switch's models as the App is told them, was the
// app_models the channel has now. Grok Build has no effort "none" of a
// channel's: the levels are the model's others, and the one the composer
// starts on is the default the user marked in the App's Providers page
// while the model has it, else medium (the middle one without it).
func grokAppWant(was string) []grokAppModel {
	var before []grokAppModel
	_ = json.Unmarshal([]byte(was), &before) // a list not there or not the App's: no defaults kept
	kept := map[string]string{}
	for _, m := range before {
		for _, e := range m.Efforts {
			if e.IsDefault {
				kept[m.ID] = e.ID
			}
		}
	}
	var out []grokAppModel
	for _, m := range sharedModels("grok-app") {
		e := grokAppModel{ID: m.ID, Name: m.Name, ContextWindow: m.Context}
		if e.Name == "" {
			e.Name = m.ID
		}
		if m.Images {
			yes := true
			e.SupportsVision = &yes
		}
		var levels []string
		for _, l := range m.Efforts {
			if l != "none" {
				levels = append(levels, l)
			}
		}
		if len(levels) > 0 {
			def := keptEffort(kept[m.ID], levels, zcodeDefaultLevel(levels))
			for _, l := range levels {
				e.Efforts = append(e.Efforts, grokAppEffort{ID: l, Name: l, IsDefault: l == def})
			}
		}
		out = append(out, e)
	}
	return out
}

// grokAppRead is the App's config.toml's [models] default and uni-switch's
// channel's keys, nil when there is none. A file that doesn't parse is an
// error, never taken as empty.
func grokAppRead(path string) (def string, ch map[string]string, err error) {
	models, err := edit.GetTOMLTable(path, "models")
	if err != nil {
		return "", nil, err
	}
	ch, err = edit.GetTOMLTable(path, grokAppChannel)
	return models["default"], ch, err
}

func grokApp(home string) *Agent {
	dir := grokAppDir(home, grokAppOS)
	path := filepath.Join(dir, "agent-home", "config.toml")
	keyDefault := "grok-app:" + path + ":default"
	at := here(home)
	key := func() string { return agentKeyAt("grok-app", at.gw()) }
	get := func() string {
		_, ch, _ := grokAppRead(path)
		if ch != nil && ch["model"] != "" {
			return uniSwitchID + "/" + ch["model"]
		}
		def, _, _ := grokAppRead(path)
		return def
	}
	// setDefault names the route the App starts on, the key taken out for "".
	// uni-switch calls it only to put a route back, never to name itself.
	setDefault := func(v string) error {
		if v == "" {
			return edit.DelTOMLKey(path, "models", "default")
		}
		return edit.SetTOMLKey(path, "models", "default", v)
	}
	// releaseRoute puts back a route an older uni-switch set to itself, once.
	// The App stops working when that route is uni-switch. A route the user
	// picks in the App after this stays.
	releaseRoute := func() error {
		if stashShared()[grokAppReleased] == "1" {
			return nil
		}
		def, _, err := grokAppRead(path)
		if err != nil {
			return err
		}
		if def == uniSwitchID {
			if err := setDefault(unstash(keyDefault)); err != nil {
				return err
			}
		}
		stash(map[string]string{grokAppReleased: "1"})
		return nil
	}
	// wire writes uni-switch's channel, model, if given, its model, and
	// nothing when the file has it all already. The channel's own extras
	// (context window, images, efforts) are the model's, as the App copies
	// them onto the channel when a model is picked in its composer.
	wire := func(model string) error {
		_, ch, err := grokAppRead(path)
		if err != nil {
			return err
		}
		want := grokAppWant(ch["app_models"])
		if len(want) == 0 {
			return nil
		}
		if model == "" {
			model = ch["model"]
		}
		i := slices.IndexFunc(want, func(m grokAppModel) bool { return m.ID == model })
		if i < 0 {
			// a model uni-switch no longer has: the channel moves to its first
			i = 0
		}
		picked := want[i]
		list, _ := json.Marshal(want)
		efforts := "[]"
		if len(picked.Efforts) > 0 {
			b, _ := json.Marshal(picked.Efforts)
			efforts = string(b)
		}
		// key, value, and whether it is written bare (a TOML bool or
		// integer, as the App and Grok Build read it) rather than a string
		type kv struct {
			k, v string
			bare bool
		}
		kvs := []kv{
			{"model", picked.ID, false},
			{"base_url", at.v1(), false},
			{"name", "uni-switch", false},
			{"api_key", key(), false},
			{"api_backend", "chat_completions", false},
			{"app_provider_mode", "generic", false},
			{"app_models", string(list), false},
			{"app_efforts", efforts, false},
			{"supports_reasoning_effort", strconv.FormatBool(len(picked.Efforts) > 0), true},
		}
		var gone []string
		if picked.SupportsVision != nil && *picked.SupportsVision {
			kvs = append(kvs, kv{"app_supports_vision", "true", false})
		} else {
			gone = append(gone, "app_supports_vision")
		}
		if picked.ContextWindow > 0 {
			kvs = append(kvs, kv{"context_window", strconv.Itoa(picked.ContextWindow), true})
		} else {
			gone = append(gone, "context_window")
		}
		same := ch != nil
		for _, x := range kvs {
			if v, ok := ch[x.k]; !ok || v != x.v {
				same = false
			}
		}
		for _, k := range gone {
			if _, ok := ch[k]; ok {
				same = false
			}
		}
		if same {
			return nil
		}
		for _, x := range kvs {
			var v any = x.v
			if x.bare {
				v = edit.Raw(x.v)
			}
			if err := edit.SetTOMLKey(path, grokAppChannel, x.k, v); err != nil {
				return err
			}
		}
		for _, k := range gone {
			if err := edit.DelTOMLKey(path, grokAppChannel, k); err != nil {
				return err
			}
		}
		return nil
	}
	// rememberRoute keeps the App's route so a later disconnect can put it
	// back if the route was uni-switch. An empty route can't be stashed (the
	// stash drops an empty value), and uni-switch's own route is never remembered
	// over the one it replaced.
	rememberRoute := func() error {
		def, _, err := grokAppRead(path)
		if err != nil || def == "" || def == uniSwitchID || stashShared()[keyDefault] != "" {
			return err
		}
		stash(map[string]string{keyDefault: def})
		return nil
	}
	// unwire takes uni-switch's channel out. The route is touched only when it
	// still names uni-switch, which is a route that doesn't run: put back the
	// one remembered, or clear it. Any other route is the App's and stays.
	unwire := func() error {
		def, _, err := grokAppRead(path)
		if err != nil {
			return err
		}
		if def == uniSwitchID {
			if err := setDefault(unstash(keyDefault)); err != nil {
				return err
			}
		} else {
			forget(keyDefault)
		}
		return edit.SetTOMLTablesMatching(path, []string{grokAppChannel}, []string{grokAppChannel + "."}, nil)
	}
	return atomic(&Agent{
		ID: "grok-app", Name: "Grok App", Icon: "grok", Aliases: []string{"grokapp"}, Spelled: prefixed,
		Dir: dir, Path: path,
		Notice: func() string {
			if Running(`(^|/)grok-app( |$)`) {
				return noticeGrokApp.String()
			}
			return ""
		},
		Sync: func() error {
			// Nothing of uni-switch's in the App: nothing to keep, nothing
			// written. A route an older uni-switch left on itself is still
			// put back, since the App doesn't run on it.
			def, ch, err := grokAppRead(path)
			if err != nil || (ch == nil && def != uniSwitchID) {
				return err
			}
			if err := rememberRoute(); err != nil {
				return err
			}
			if err := releaseRoute(); err != nil {
				return err
			}
			if ch == nil {
				return nil
			}
			return wire("")
		},
		Check: func() string {
			def, ch, err := grokAppRead(path)
			if err != nil {
				return err.Error()
			}
			if ch == nil {
				if def == uniSwitchID {
					return "Grok App's route is uni-switch, but its [" + grokAppChannel + "] (config.toml) is gone, so it no longer reaches uni-switch"
				}
				return ""
			}
			return wiringOff("Grok App", path, func(k string) (string, bool) { v, ok := ch[k]; return v, ok },
				"base_url", at.v1(), "api_key", key())
		},
		Fields: []Field{{
			// the model on uni-switch's custom provider, not the route the App
			// starts on. One of uni-switch's models is uni-switch/<provider>/<model>
			// written into the channel, and never the route: the App's own
			// route stays in [models] default until the user presses Use in
			// Settings › Account. One of the App's own routes picked here
			// is written as the route, with uni-switch's channel taken out.
			Key: "model", Label: "model",
			Get: get,
			Set: func(v string) error {
				if _, _, err := grokAppRead(path); err != nil {
					return err
				}
				if ref, ok := strings.CutPrefix(v, uniSwitchID+"/"); ok && isUniSwitch(ref) {
					if err := rememberRoute(); err != nil {
						return err
					}
					if err := releaseRoute(); err != nil {
						return err
					}
					return wire(ref)
				}
				if v == "" {
					return unwire()
				}
				// one of the App's own routes: uni-switch's channel goes, the
				// way picking another provider leaves uni-switch elsewhere, and
				// the route is the one picked. Disconnect comes here to put
				// back the route the App had before.
				def, ch, err := grokAppRead(path)
				if err != nil {
					return err
				}
				if ch != nil {
					if err := unwire(); err != nil {
						return err
					}
					if def, _, err = grokAppRead(path); err != nil {
						return err
					}
				}
				if def == v {
					return nil
				}
				return setDefault(v)
			},
			Options: func(cur map[string]string) []Option {
				return withVia(grokAppOwn(path, keyDefault, cur["model"]), "grok-app", uniSwitchID+"/")
			},
		}},
	}, path)
}

// grokAppOwn is the App's own routes for the picker: its official one
// ("grok", Grok Build's sign-in), the channels the user added, and the one
// it was on before uni-switch.
func grokAppOwn(path, keyDefault, cur string) []Option {
	var out []Option
	add := func(v, label, direct string) {
		if v == "" || v == uniSwitchID || prefixed(v) || slices.ContainsFunc(out, func(o Option) bool { return o.Value == v }) {
			return
		}
		o := Option{Value: v, Icon: "xai", Direct: direct}
		if label != "" && label != v {
			o.Label = label
		}
		out = append(out, o)
	}
	add("grok", "Grok (official)", "xAI")
	if tables, err := edit.TOMLTables(path); err == nil {
		for _, t := range tables {
			id, ok := strings.CutPrefix(t, "model.")
			if !ok {
				continue
			}
			if u, err := unquoteTOML(id); err == nil {
				id = u
			}
			kv, _ := edit.GetTOMLTable(path, t)
			if kv["base_url"] == "" {
				continue
			}
			direct := hostOf(kv["base_url"])
			add(id, kv["name"], direct)
		}
	}
	add(stashShared()[keyDefault], "", "")
	add(cur, "", "")
	return group("Grok App", out)
}

// unquoteTOML is a table name's quoted part without its quotes.
func unquoteTOML(s string) (string, error) {
	if len(s) >= 2 && (s[0] == '"' || s[0] == '\'') && s[len(s)-1] == s[0] {
		return s[1 : len(s)-1], nil
	}
	return s, os.ErrInvalid
}

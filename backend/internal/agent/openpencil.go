package agent

// OpenPencil (ZSeven-W/openpencil, the openpencil-desktop app; @JC_be_better
// on X) is a design canvas whose chat runs models through "builtin agents"
// of the user's own: an Anthropic or OpenAI-compatible endpoint, a key and
// a list of models, each listed in its chat's model picker. It keeps them
// in one settings.json in the platform's config folder, dirs::config_dir()
// joined with openpencil: ~/Library/Application Support/openpencil on a
// Mac, $XDG_CONFIG_HOME/openpencil (~/.config/openpencil) on Linux,
// %APPDATA%\openpencil on Windows (op-editor-host-core's settings_io.rs,
// settings_path). Its fields (SettingsPayload, BuiltinAgentPayload,
// settings_payload.rs, main at 40902a3f and v0.8.4):
//
//	{"version":1, …,
//	 "builtin_agents":[{"id":"uni-switch","preset":"custom","display_name":"uni-switch",
//	   "kind":"openai-compat","api_key":"uni-switch-openpencil",
//	   "model":"<first model>","models":["<model>",…],
//	   "base_url":"http://127.0.0.1:3425/v1","enabled":true}], …}
//
// version is required (no serde default): a file without it fails to parse,
// is set aside as settings.json.corrupt-<time>, and OpenPencil then refuses
// to save at all; a version other than 1 has the whole file ignored. So
// uni-switch writes version 1 in a file it makes, and leaves alone a file whose
// version it doesn't know. kind openai-compat posts to base_url +
// /chat/completions, so base_url is the gateway's /v1. models is the list
// (at most 64, written only when there are two or more, the first again as
// model); v0.8.4 knows only model and reads past models, so it lists the
// first model alone.
//
// OpenPencil reads the file once, when it starts, and writes the whole of
// it back from what it holds when a setting changes and when it quits
// (app_handler.rs, settings_io::save). A change uni-switch makes while it runs
// is written over when it quits, so uni-switch refuses one then, where it can
// tell (Running can't on Windows, where the notice says so instead).
//
// The model is picked in OpenPencil's own chat picker
// (builtin:uni-switch:<model>), not in a file, so what uni-switch sets is whether
// its models are in that picker: one entry of builtin_agents, id uni-switch
// (OpenPencil numbers its own builtin-N, and reads a picker value up to its
// second colon). The user's other entries and settings stay, and so do the
// name and the on/off they give uni-switch's entry in OpenPencil's settings.
// Its User-Agent isn't known, so its requests are told apart by their key.
//
// open-pencil/open-pencil, the other app of the name, keeps its providers in
// its webview's localStorage, no file uni-switch can write: it reaches uni-switch
// through an agent uni-switch wires (Pi, an ACP agent) or a base URL typed in.

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"github.com/tidwall/gjson"

	"github.com/dieqiyun/uni-switch/backend/internal/appdir"
	"github.com/dieqiyun/uni-switch/backend/internal/edit"
	"github.com/dieqiyun/uni-switch/backend/internal/gateway"
)

// openPencilApp is OpenPencil's macOS bundle, as its release builds it
// (scripts/bundle-macos.sh).
var openPencilApp = desktopApp{id: "com.zseven-w.openpencil", names: []string{"OpenPencil.app"}}

// openPencilMaxModels is how many models OpenPencil keeps for one entry
// (MAX_BUILTIN_AGENT_MODELS); it drops the rest when it next saves.
const openPencilMaxModels = 64

// openPencilProc is OpenPencil's process, its binary's name on every system
// (the .app's CFBundleExecutable, the .deb's and AppImage's, the .exe).
var openPencilProc = []string{`(^|/)openpencil-desktop( |$)`}

var noticeOpenPencil = newNotice("OpenPencil reads its settings when it starts and writes them all back when it quits — quit OpenPencil before changing this, then open it and pick a uni-switch model in its chat's model picker.")

// errOpenPencilRunning is a change refused while OpenPencil runs.
var errOpenPencilRunning = errors.New("OpenPencil writes its settings back when it quits, over what uni-switch writes now: quit OpenPencil, then try again")

func openPencil(home, cfg string) *Agent {
	dir := openPencilDir(home, cfg)
	path := filepath.Join(dir, "settings.json")
	write := func(on bool) error {
		if !on {
			return openPencilUnwire(path)
		}
		return openPencilWire(path, openPencilEntry(gateway.URL()))
	}
	return atomic(&Agent{
		ID: "openpencil", Name: "OpenPencil", Icon: "generic", Aliases: []string{"open-pencil", "openpencil-desktop"},
		Dir: dir, Path: path,
		// its settings folder once it has run, ~/.openpencil once its op
		// CLI or MCP server has, else its app. No command: op is its CLI's
		// name, and other tools' too
		detect: func() bool {
			return isDir(dir) || isDir(filepath.Join(home, ".openpencil")) || macBundle(openPencilApp) || openPencilWindowsApp()
		},
		Notice: func() string {
			if openPencilWired(path) && Running(openPencilProc...) {
				return noticeOpenPencil.String()
			}
			return ""
		},
		// a sync while it runs is written as well: a quit puts back what it
		// read, and the next sync puts uni-switch's models back again
		Sync: func() error {
			if !openPencilWired(path) {
				return nil
			}
			return write(true)
		},
		// its picker lists the models picked for it (#1508)
		ListsModels: true,
		Fields: []Field{{
			Key: "provider", Label: "provider",
			Get: func() string {
				if openPencilWired(path) {
					return uniSwitchID
				}
				return ""
			},
			Set: func(v string) error {
				if openPencilRunning() {
					return errOpenPencilRunning
				}
				return write(v != "")
			},
			Options: func(map[string]string) []Option {
				return []Option{{Value: uniSwitchID, Label: "uni-switch", Icon: "uni-switch", Note: "every uni-switch model in OpenPencil's chat model picker"}}
			},
		}},
	}, path)
}

// openPencilRunning says OpenPencil is open where uni-switch can tell; on
// Windows, where Running can't, no, and the notice asks to quit it first.
var openPencilRunning = func() bool {
	return runtime.GOOS != "windows" && Running(openPencilProc...)
}

// openPencilDir is the folder of OpenPencil's settings.json, as Rust's
// dirs::config_dir() finds the platform's config folder.
func openPencilDir(home, cfg string) string {
	return openPencilDirOn(runtime.GOOS, home, cfg, appdir.Getenv("APPDATA"))
}

// openPencilDirOn is openPencilDir on the system goos, with the
// XDG_CONFIG_HOME folder cfg and %APPDATA% appData.
func openPencilDirOn(goos, home, cfg, appData string) string {
	switch goos {
	case "darwin":
		return filepath.Join(home, "Library", "Application Support", "openpencil")
	case "windows":
		d := appData
		if d == "" {
			d = filepath.Join(home, "AppData", "Roaming")
		}
		return filepath.Join(d, "openpencil")
	}
	return filepath.Join(cfg, "openpencil")
}

// openPencilWindowsApp says OpenPencil's installer has put it where it
// installs it, %ProgramFiles%\OpenPencil (scripts/package-windows.nsi).
func openPencilWindowsApp() bool {
	if runtime.GOOS != "windows" {
		return false
	}
	for _, v := range []string{"ProgramW6432", "ProgramFiles"} {
		if d := appdir.Getenv(v); d != "" && isFile(filepath.Join(d, "OpenPencil", "openpencil-desktop.exe")) {
			return true
		}
	}
	return false
}

// openPencilAgent is an entry of builtin_agents in the order OpenPencil
// writes one.
type openPencilAgent struct {
	ID          string   `json:"id"`
	Preset      string   `json:"preset"`
	DisplayName string   `json:"display_name"`
	Kind        string   `json:"kind"`
	APIKey      string   `json:"api_key"`
	Model       string   `json:"model"`
	Models      []string `json:"models,omitempty"`
	BaseURL     string   `json:"base_url"`
	Enabled     bool     `json:"enabled"`
}

// openPencilEntry is uni-switch's entry for a gateway at gw: its Chat
// Completions, the key that names OpenPencil, and the models picked for it
// as OpenPencil keeps a list (normalize_builtin_models): each once, at most
// 64, the list written only for two or more.
func openPencilEntry(gw string) openPencilAgent {
	var ms []string
	seen := map[string]bool{}
	for _, m := range sharedModels("openpencil") {
		if m.ID == "" || seen[m.ID] || len([]rune(m.ID)) > 256 {
			continue
		}
		seen[m.ID] = true
		ms = append(ms, m.ID)
		if len(ms) == openPencilMaxModels {
			break
		}
	}
	e := openPencilAgent{ID: uniSwitchID, Preset: "custom", DisplayName: "uni-switch", Kind: "openai-compat",
		APIKey: agentKeyAt("openpencil", gw), BaseURL: gw + "/v1", Enabled: true}
	if len(ms) > 0 {
		e.Model = ms[0]
	}
	if len(ms) > 1 {
		e.Models = ms
	}
	return e
}

// openPencilWired says settings.json has uni-switch's entry.
func openPencilWired(path string) bool {
	_, i, _ := openPencilAgents(path)
	return i >= 0
}

// openPencilAgents are the entries of settings.json's builtin_agents as
// written, and the place of uni-switch's among them (-1 for none). A file that
// isn't there or holds nothing has none; one OpenPencil wouldn't read, or
// whose version it doesn't know, is an error, so uni-switch leaves it alone.
func openPencilAgents(path string) (list []json.RawMessage, i int, err error) {
	i = -1
	b, err := edit.Read(path)
	if err != nil {
		return nil, i, err
	}
	if len(bytes.TrimSpace(b)) == 0 {
		return nil, i, nil
	}
	if !json.Valid(b) || !gjson.ParseBytes(b).IsObject() {
		return nil, i, fmt.Errorf("%s isn't JSON OpenPencil reads", path)
	}
	if v := gjson.GetBytes(b, "version"); v.Type != gjson.Number || v.Raw != "1" {
		return nil, i, fmt.Errorf("%s says version %s, not the 1 OpenPencil reads, so uni-switch leaves it alone", path, v.Raw)
	}
	raw := gjson.GetBytes(b, "builtin_agents")
	if !raw.Exists() || raw.Type == gjson.Null {
		return nil, i, nil
	}
	if !raw.IsArray() || json.Unmarshal([]byte(raw.Raw), &list) != nil {
		return nil, i, fmt.Errorf("%s: builtin_agents is not a list", path)
	}
	for j, e := range list {
		if gjson.GetBytes(e, "id").String() == uniSwitchID {
			return list, j, nil
		}
	}
	return list, i, nil
}

// openPencilWire puts uni-switch's entry into settings.json, the file made when
// there is none; an entry there already keeps the name and the on/off the
// user gave it in OpenPencil. The file is written only when the entry says
// something else.
func openPencilWire(path string, want openPencilAgent) error {
	list, i, err := openPencilAgents(path)
	if err != nil {
		return err
	}
	if i >= 0 {
		if n := gjson.GetBytes(list[i], "display_name"); n.Type == gjson.String {
			want.DisplayName = n.String()
		}
		if e := gjson.GetBytes(list[i], "enabled"); e.IsBool() {
			want.Enabled = e.Bool()
		}
	}
	w, err := json.Marshal(want)
	if err != nil {
		return err
	}
	if i >= 0 && sameJSON(string(list[i]), want) {
		return nil
	}
	if i >= 0 {
		list[i] = w
	} else {
		list = append(list, w)
	}
	if _, has := edit.GetJSON(path, "version"); !has {
		return edit.SetJSON(path, edit.KV{Path: "version", Value: 1}, edit.KV{Path: "builtin_agents", Value: list})
	}
	return edit.SetJSON(path, edit.KV{Path: "builtin_agents", Value: list})
}

// openPencilUnwire takes uni-switch's entry out of settings.json, and the file
// when nothing else is left in it: the version and an empty list are what
// uni-switch wrote in a file OpenPencil hasn't saved yet.
func openPencilUnwire(path string) error {
	list, i, err := openPencilAgents(path)
	if err != nil || i < 0 {
		return err
	}
	list = append(list[:i], list[i+1:]...)
	if len(list) == 0 {
		var rest map[string]json.RawMessage
		if b, err := edit.Read(path); err == nil && json.Unmarshal(b, &rest) == nil {
			delete(rest, "version")
			delete(rest, "builtin_agents")
			if len(rest) == 0 {
				return os.Remove(path)
			}
		}
	}
	return edit.SetJSON(path, edit.KV{Path: "builtin_agents", Value: list})
}

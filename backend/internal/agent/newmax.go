package agent

// NewMax (newmax.cc, 牛马AI; jwaitq on Discord) is a desktop agent app on
// the Claude Agent SDK. It keeps its settings in its own SQLite database,
// newmax.db in its data folder: $NEWMAX_USER_DATA_DIR, else ~/.newmax on
// every system (configureAppDataPaths in its main bundle, v1.1.22). The
// row `app` of its table settings (key TEXT PRIMARY KEY, value TEXT NOT
// NULL) is JSON.stringify of all its settings, and their providers are a
// list in it, keys in the clear:
//
//	{…, "providerId":"default", "model":"default",
//	 "providers":[…, {"id":"uni-switch-gateway","name":"uni-switch",
//	   "baseUrl":"http://127.0.0.1:3425","apiKey":"uni-switch-newmax",
//	   "apiKeys":["uni-switch-newmax"],"apiKeyRotationEnabled":false,
//	   "apiKeyField":"ANTHROPIC_AUTH_TOKEN","apiFormat":"anthropic",
//	   "models":[…],"enabledModels":[…],"enabled":true,"canDelete":true}],
//	 "providerOrder":["default","uni-switch-gateway"], …}
//
// That entry is the one NewMax's own "import from uni-switch" writes for a
// provider of providers.json (mapProvider, applyCcSwitchProviderImport):
// an Anthropic base URL without /v1 (it posts to baseUrl + /v1/messages,
// through the Claude Code its SDK runs, as ANTHROPIC_BASE_URL and
// ANTHROPIC_AUTH_TOKEN), at most 20 models (MAX_MODELS: a primary and 19
// back-ups; it lists enabledModels, the first 20, in its model picker), and
// the id added to providerOrder. A provider is listed in the model picker
// whether or not providerOrder has it, so the model is picked there and the
// field is whether uni-switch's models are in it (ListsModels). The id is
// uni-switch's own: NewMax's built-in "uni-switch" preset is "uni-switch", and the ones
// its import makes are "uni-switch-<id>%3A<protocol>", so neither is touched.
//
// NewMax reads the row when it starts and writes it whole from what it
// holds whenever a setting changes, so a change uni-switch makes while it runs
// is lost the next time it saves: uni-switch refuses one then, where it can
// tell (Running can't on Windows, where the notice says so instead). The
// row is edited in place, in one transaction: uni-switch's entry and its id in
// providerOrder go in and out, and the rest of the row stays byte for byte.
// The user's name for uni-switch's entry, its on/off and the fields NewMax adds
// to it (capabilities it tested) stay through a sync.

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/tidwall/gjson"

	"github.com/dieqiyun/uni-switch/backend/internal/appdir"
	"github.com/dieqiyun/uni-switch/backend/internal/gateway"
	"github.com/dieqiyun/uni-switch/backend/internal/sessions"
	_ "github.com/dieqiyun/uni-switch/backend/internal/sqlitedb"
)

// newMaxApp is NewMax's macOS bundle.
var newMaxApp = desktopApp{id: "cc.newmax.desktop", names: []string{"NewMax.app"}}

// newMaxID is uni-switch's provider in NewMax's providers.
const newMaxID = "uni-switch-gateway"

// newMaxMaxModels is how many models NewMax keeps in a provider's picker
// list (MAX_MODELS).
const newMaxMaxModels = 20

// newMaxProc is NewMax's process: the .app's CFBundleExecutable NewMax, the
// Linux build's newmax. The program itself, the first word of its command
// line, or the .app's executable wherever the bundle is: a shell in a
// folder named newmax ("cd ~/newmax && …") is not NewMax.
var newMaxProc = []string{`^([^ ]*/)?(NewMax|newmax)( |$)`, `NewMax\.app/Contents/MacOS/NewMax( |$)`}

var noticeNewMax = newNotice("NewMax keeps its settings in memory while it runs and writes them all back when one changes — quit NewMax before changing this, then open it and pick a uni-switch model in its model picker.")

// errNewMaxRunning is a change refused while NewMax runs.
var errNewMaxRunning = errors.New("NewMax writes its settings back over what uni-switch writes now: quit NewMax, then try again")

// errNewMaxNoDB is a change asked for before NewMax made its database.
var errNewMaxNoDB = errors.New("NewMax hasn't made its settings yet: open NewMax once, quit it, then try again")

func newMax(home string) *Agent {
	dir := newMaxDir(home)
	path := filepath.Join(dir, "newmax.db")
	write := func(on bool) error {
		if !on {
			return newMaxEdit(path, newMaxUnwire)
		}
		e := newMaxEntryFor(gateway.URL())
		return newMaxEdit(path, func(app []byte, had bool) ([]byte, error) { return newMaxWire(app, had, e) })
	}
	return &Agent{
		ID: "newmax", Name: "NewMax", Icon: "generic", Aliases: []string{"newmax-ai", "newmaxai"},
		Dir: dir, Path: path,
		detect: func() bool { return isDir(dir) || macBundle(newMaxApp) },
		Notice: func() string {
			if newMaxWired(path) && Running(newMaxProc...) {
				return noticeNewMax.String()
			}
			return ""
		},
		// a sync while it runs is written as well: NewMax's next save
		// puts back what it holds, and the next sync uni-switch's models
		Sync: func() error {
			if !newMaxWired(path) {
				return nil
			}
			return write(true)
		},
		ListsModels: true,
		Fields: []Field{{
			Key: "provider", Label: "provider",
			Get: func() string {
				if newMaxWired(path) {
					return uniSwitchID
				}
				return ""
			},
			Set: func(v string) error {
				if newMaxRunning() {
					return errNewMaxRunning
				}
				return write(v != "")
			},
			Options: func(map[string]string) []Option {
				return []Option{{Value: uniSwitchID, Label: "uni-switch", Icon: "uni-switch", Note: "every uni-switch model in NewMax's model picker"}}
			},
		}},
	}
}

// newMaxRunning says NewMax is open where uni-switch can tell; on Windows,
// where Running can't, no, and the notice asks to quit it first.
var newMaxRunning = func() bool {
	return runtime.GOOS != "windows" && Running(newMaxProc...)
}

// newMaxDir is NewMax's data folder: $NEWMAX_USER_DATA_DIR when it names
// one, else ~/.newmax, on every system.
func newMaxDir(home string) string {
	if d := appdir.Getenv("NEWMAX_USER_DATA_DIR"); d != "" && filepath.IsAbs(d) {
		return d
	}
	return filepath.Join(home, ".newmax")
}

// newMaxEntry is uni-switch's provider, in the order NewMax's import writes
// one.
type newMaxEntry struct {
	ID                    string           `json:"id"`
	Name                  string           `json:"name"`
	BaseURL               string           `json:"baseUrl"`
	APIKey                string           `json:"apiKey"`
	APIKeys               []string         `json:"apiKeys"`
	APIKeyRotationEnabled bool             `json:"apiKeyRotationEnabled"`
	APIKeyField           string           `json:"apiKeyField"`
	APIFormat             string           `json:"apiFormat"`
	Models                []string         `json:"models"`
	EnabledModels         []string         `json:"enabledModels"`
	Enabled               bool             `json:"enabled"`
	CanDelete             bool             `json:"canDelete"`
	ModelContextWindows   map[string]int64 `json:"modelContextWindows,omitempty"`
}

// newMaxEntryFor is uni-switch's provider for a gateway at gw: its Anthropic
// Messages, the key that names NewMax, and the models picked for it, each
// once, at most 20, with the context windows uni-switch knows.
func newMaxEntryFor(gw string) newMaxEntry {
	key := agentKeyAt("newmax", gw)
	e := newMaxEntry{ID: newMaxID, Name: "uni-switch", BaseURL: gw, APIKey: key, APIKeys: []string{key},
		APIKeyField: "ANTHROPIC_AUTH_TOKEN", APIFormat: "anthropic", Models: []string{}, Enabled: true, CanDelete: true}
	seen := map[string]bool{}
	for _, m := range sharedModels("newmax") {
		if m.ID == "" || seen[m.ID] {
			continue
		}
		seen[m.ID] = true
		e.Models = append(e.Models, m.ID)
		if m.Context > 0 {
			if e.ModelContextWindows == nil {
				e.ModelContextWindows = map[string]int64{}
			}
			e.ModelContextWindows[m.ID] = int64(m.Context)
		}
		if len(e.Models) == newMaxMaxModels {
			break
		}
	}
	e.EnabledModels = e.Models
	return e
}

// newMaxReadApp reads the `app` row of the database at path without writing a
// byte beside it: the row, whether there is one, and an error for a
// database that couldn't be read.
func newMaxReadApp(path string) (app []byte, had bool, err error) {
	if _, err := os.Stat(path); err != nil {
		return nil, false, err
	}
	db, closeDB, err := sessions.OpenUntouched(path)
	if err != nil {
		return nil, false, err
	}
	defer closeDB()
	var v string
	switch err := db.QueryRow(`SELECT value FROM settings WHERE key = 'app'`).Scan(&v); {
	case errors.Is(err, sql.ErrNoRows):
		return nil, false, nil
	case err != nil:
		return nil, false, err
	}
	return []byte(v), true, nil
}

// newMaxWired says NewMax's settings have uni-switch's provider.
func newMaxWired(path string) bool {
	app, had, err := newMaxReadApp(path)
	if err != nil || !had {
		return false
	}
	_, i, err := newMaxProviders(app)
	return err == nil && i >= 0
}

// newMaxProviders are the entries of the row's providers as written, and
// the place of uni-switch's among them (-1 for none). A row that isn't a JSON
// object, or whose providers aren't a list, is an error: NewMax would fall
// back to its defaults on it, and uni-switch leaves it alone.
func newMaxProviders(app []byte) (list []json.RawMessage, i int, err error) {
	i = -1
	if !json.Valid(app) || !gjson.ParseBytes(app).IsObject() {
		return nil, i, errors.New("NewMax's settings aren't JSON it reads, so uni-switch leaves them alone")
	}
	raw := gjson.GetBytes(app, "providers")
	if !raw.Exists() || raw.Type == gjson.Null {
		return nil, i, nil
	}
	if !raw.IsArray() || json.Unmarshal([]byte(raw.Raw), &list) != nil {
		return nil, i, errors.New("NewMax's providers aren't a list, so uni-switch leaves them alone")
	}
	for j, e := range list {
		if gjson.GetBytes(e, "id").String() == newMaxID {
			return list, j, nil
		}
	}
	return list, i, nil
}

// newMaxEdit changes the `app` row of the database at path with fn, in one
// transaction that waits on NewMax's own: fn gets the row (nil for none)
// and gives the new one, nil to take the row out, or the same bytes to
// leave it as it is.
func newMaxEdit(path string, fn func(app []byte, had bool) ([]byte, error)) error {
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		return errNewMaxNoDB
	} else if err != nil {
		return err
	}
	u := url.URL{Scheme: "file", Path: filepath.ToSlash(path), RawQuery: "_pragma=busy_timeout(5000)&_txlock=immediate"}
	if len(u.Path) >= 2 && u.Path[1] == ':' {
		u.Path = "/" + u.Path
	}
	db, err := sql.Open("sqlite", u.String())
	if err != nil {
		return err
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	ctx := context.Background()
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("NewMax's settings: %w", err)
	}
	defer tx.Rollback()
	var v string
	had := true
	switch err := tx.QueryRowContext(ctx, `SELECT value FROM settings WHERE key = 'app'`).Scan(&v); {
	case errors.Is(err, sql.ErrNoRows):
		had = false
	case err != nil:
		return fmt.Errorf("NewMax's settings: %w", err)
	}
	var app []byte
	if had {
		app = []byte(v)
	}
	out, err := fn(app, had)
	if err != nil {
		return err
	}
	switch {
	case out == nil && !had, out != nil && had && bytes.Equal(out, app):
		return nil
	case out == nil:
		_, err = tx.ExecContext(ctx, `DELETE FROM settings WHERE key = 'app'`)
	default:
		_, err = tx.ExecContext(ctx, `INSERT INTO settings (key, value) VALUES ('app', ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value`, string(out))
	}
	if err != nil {
		return fmt.Errorf("NewMax's settings: %w", err)
	}
	return tx.Commit()
}

// newMaxWire puts uni-switch's provider into the row app and its id into
// providerOrder, a row made when there is none (NewMax lays its defaults
// under what a row has, its preset providers too). An entry there already
// keeps the name and the on/off the user gave it, and the fields NewMax
// added to it; the row is given back as it was when nothing would change.
func newMaxWire(app []byte, had bool, want newMaxEntry) ([]byte, error) {
	if !had {
		app = []byte("{}")
	}
	list, i, err := newMaxProviders(app)
	if err != nil {
		return nil, err
	}
	b, err := json.Marshal(want)
	if err != nil {
		return nil, err
	}
	var entry []byte
	if i < 0 {
		entry = b
	} else {
		// what uni-switch says, over the entry NewMax keeps
		var set []jsonKV
		gjson.ParseBytes(b).ForEach(func(k, v gjson.Result) bool {
			switch k.String() {
			case "id", "name", "enabled", "canDelete":
			default:
				set = append(set, jsonKV{k.String(), []byte(v.Raw)})
			}
			return true
		})
		entry = jsonSetKeys(list[i], set)
	}
	if i >= 0 && bytes.Equal(entry, list[i]) {
		entry = nil
	}
	if entry != nil {
		if i >= 0 {
			list[i] = entry
		} else {
			list = append(list, entry)
		}
		app = jsonSetKeys(app, []jsonKV{{"providers", joinRaw(list)}})
	}
	order, err := newMaxOrder(app)
	if err != nil {
		return nil, err
	}
	if !containsString(order, newMaxID) {
		if len(order) == 0 {
			order = []string{"default"}
		}
		ob, _ := json.Marshal(append(order, newMaxID))
		app = jsonSetKeys(app, []jsonKV{{"providerOrder", ob}})
	}
	return app, nil
}

// newMaxUnwire takes uni-switch's provider out of the row app, its id out of
// providerOrder, and a pick of it back to NewMax's default; a row left
// with nothing but what uni-switch made goes.
func newMaxUnwire(app []byte, had bool) ([]byte, error) {
	if !had {
		return nil, nil
	}
	list, i, err := newMaxProviders(app)
	if err != nil {
		return nil, err
	}
	if i >= 0 {
		list = append(list[:i], list[i+1:]...)
		app = jsonSetKeys(app, []jsonKV{{"providers", joinRaw(list)}})
	}
	order, err := newMaxOrder(app)
	if err != nil {
		return nil, err
	}
	if containsString(order, newMaxID) {
		var left []string
		for _, id := range order {
			if id != newMaxID {
				left = append(left, id)
			}
		}
		if left == nil {
			left = []string{}
		}
		ob, _ := json.Marshal(left)
		app = jsonSetKeys(app, []jsonKV{{"providerOrder", ob}})
	}
	if gjson.GetBytes(app, "providerId").String() == newMaxID {
		app = jsonSetKeys(app, []jsonKV{{"providerId", []byte(`"default"`)}, {"model", []byte(`"default"`)}})
	}
	// the row uni-switch made: its empty list and NewMax's default order
	var rest map[string]json.RawMessage
	if json.Unmarshal(app, &rest) == nil && len(rest) == 2 && len(list) == 0 &&
		sameJSON(string(rest["providerOrder"]), []string{"default"}) {
		if _, ok := rest["providers"]; ok {
			return nil, nil
		}
	}
	return app, nil
}

// newMaxOrder is the row's providerOrder; none is empty.
func newMaxOrder(app []byte) ([]string, error) {
	raw := gjson.GetBytes(app, "providerOrder")
	if !raw.Exists() || raw.Type == gjson.Null {
		return nil, nil
	}
	var order []string
	if !raw.IsArray() || json.Unmarshal([]byte(raw.Raw), &order) != nil {
		return nil, errors.New("NewMax's providerOrder isn't a list of names, so uni-switch leaves it alone")
	}
	return order, nil
}

func containsString(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// jsonKV is a key of a JSON object and its value, as JSON.
type jsonKV struct {
	key string
	raw []byte
}

// jsonSetKeys is the JSON object obj with each key of set given its value:
// one there already in its place, a new one after the rest. Every other
// byte of obj stays as it is.
func jsonSetKeys(obj []byte, set []jsonKV) []byte {
	out := append([]byte(nil), obj...)
	for _, kv := range set {
		r := gjson.GetBytes(out, gjsonEscape(kv.key))
		if r.Exists() && r.Index > 0 && string(out[r.Index:r.Index+len(r.Raw)]) == r.Raw {
			out = append(append(append([]byte(nil), out[:r.Index]...), kv.raw...), out[r.Index+len(r.Raw):]...)
			continue
		}
		k, _ := json.Marshal(kv.key)
		end := bytes.LastIndexByte(out, '}')
		head := bytes.TrimRight(out[:end], " \t\r\n")
		sep := []byte(",")
		if bytes.HasSuffix(head, []byte("{")) {
			sep = nil
		}
		var b []byte
		b = append(b, head...)
		b = append(b, sep...)
		b = append(b, k...)
		b = append(b, ':')
		b = append(b, kv.raw...)
		b = append(b, out[end:]...)
		out = b
	}
	return out
}

// gjsonEscape is key as a gjson path of one key.
func gjsonEscape(key string) string {
	var b strings.Builder
	for _, c := range key {
		if strings.ContainsRune(`.*?|#@\!=<>%"`, c) {
			b.WriteByte('\\')
		}
		b.WriteRune(c)
	}
	return b.String()
}

// joinRaw is list as one JSON list, written as JSON.stringify writes it.
func joinRaw(list []json.RawMessage) []byte {
	return append(append([]byte("["), bytes.Join(func() [][]byte {
		out := make([][]byte, len(list))
		for i, e := range list {
			out[i] = e
		}
		return out
	}(), []byte(","))...), ']')
}

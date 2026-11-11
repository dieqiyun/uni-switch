package agent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/edit"
	"github.com/dieqiyun/uni-switch/backend/internal/gateway"
)

// asideAccountsJSON is ~/.aside/accounts.json as fjrtdk pasted it on #1499
// (the e-mail was redacted there as "...").
const asideAccountsJSON = `{"currentAccountId": 1, "accounts": [
  {"id": 1, "email": "...", "provider": "google", "mode": "cloud"},
  {"id": 0, "provider": "anonymous", "mode": "local"},
  {"id": 2, "provider": "anonymous", "mode": "local"}]}`

// The settings each account had on #1499: u1's default is uni-switch's
// group/spacebunny, read back through `aside repl --account u1`; u0's is
// the Antigravity model uni-switch kept writing because it only knew u0.
const (
	asideU0Settings = `{"defaultModel":{"provider":"antigravity","modelId":"gemini-3.8-flash-tiered","thinkingLevel":"medium","fastMode":false},"theme":"dark"}`
	asideU1Settings = `{"defaultModel":{"provider":"google","modelId":"gemini-3-pro","thinkingLevel":"high","fastMode":false},"theme":"light"}`
	asideU2Settings = `{"defaultModel":{"provider":"openai","modelId":"gpt-5","thinkingLevel":"low"}}`
)

// asideAccountsHome lays out ~/.aside as on #1499 (u/0, u/1, u/2, each
// with settings.json, models.json, credentials.json and state.db, beside
// accounts.json and cli/) in a sandbox HOME, with a fake Aside runtime that
// answers for whichever account it is asked about. calls lists the
// accounts the runtime was asked about.
func asideAccountsHome(t *testing.T) (home string, calls *[]string) {
	t.Helper()
	home = syncHome(t)
	aside := filepath.Join(home, ".aside")
	writeFile(t, filepath.Join(aside, "accounts.json"), asideAccountsJSON)
	writeFile(t, filepath.Join(aside, "cli", "update-check.json"), `{}`)
	for id, settings := range []string{asideU0Settings, asideU1Settings, asideU2Settings} {
		dir := asideAccountDir(here(home), id)
		writeFile(t, filepath.Join(dir, "settings.json"), settings)
		writeFile(t, filepath.Join(dir, "models.json"), `{"providers":{"native":{"apiKey":"fixture","baseUrl":"https://native.invalid/v1","models":[{"id":"m"}]}},"keepMe":"yes"}`)
		writeFile(t, filepath.Join(dir, "credentials.json"), `{}`)
		writeFile(t, filepath.Join(dir, "state.db"), "")
	}
	calls = &[]string{}
	settingsOf := func(account string) string {
		id, ok := asideID(json.RawMessage(`"` + account + `"`))
		if !ok {
			t.Fatalf("account %q", account)
		}
		return filepath.Join(asideAccountDir(here(home), id), "settings.json")
	}
	oldRead, oldSet := asideRead, asideSet
	asideRead = func(account string) (map[string]json.RawMessage, error) {
		*calls = append(*calls, account)
		var s map[string]json.RawMessage
		err := json.Unmarshal([]byte(readFile(settingsOf(account))), &s)
		return s, err
	}
	asideSet = func(account, expr string) error {
		*calls = append(*calls, account)
		return asideFakeSet(settingsOf(account), expr)
	}
	t.Cleanup(func() { asideRead, asideSet = oldRead, oldSet })
	return home, calls
}

func asideFile(home string, id int, name string) string {
	return filepath.Join(asideAccountDir(here(home), id), name)
}

func sameJSONText(t *testing.T, got, want string) bool {
	t.Helper()
	var g, w any
	if json.Unmarshal([]byte(got), &g) != nil || json.Unmarshal([]byte(want), &w) != nil {
		return false
	}
	return reflect.DeepEqual(g, w)
}

func onlyAccount(t *testing.T, calls []string, want string) {
	t.Helper()
	for _, c := range calls {
		if c != want {
			t.Fatalf("Aside was asked about %s; want only %s (%v)", c, want, calls)
		}
	}
}

// #1499: with u1 active in Aside, uni-switch configured u0. It now follows the
// account Aside has active, and leaves the other accounts alone.
func TestAsideFollowsTheActiveAccount(t *testing.T) {
	home, calls := asideAccountsHome(t)
	u0 := readFile(asideFile(home, 0, "settings.json")) + readFile(asideFile(home, 0, "models.json"))
	a := mustFindAside(t)
	if want := asideFile(home, 1, "settings.json"); a.Path != want {
		t.Fatalf("path %s, want %s", a.Path, want)
	}
	if err := a.Pick("model", "uni-switch/relay/glm-4.6"); err != nil {
		t.Fatal(err)
	}
	onlyAccount(t, *calls, "u1")
	if key, _ := edit.GetJSON(asideFile(home, 1, "models.json"), "providers.uni-switch.apiKey"); key != gateway.TokenFor("aside") {
		t.Fatal("u1 has no uni-switch provider")
	}
	if got := mustFindAside(t).Field("model").Get(); got != "uni-switch/relay/glm-4.6" {
		t.Fatalf("u1 model %q", got)
	}
	if readFile(asideFile(home, 0, "settings.json"))+readFile(asideFile(home, 0, "models.json")) != u0 {
		t.Fatal("u0 was written while u1 is active")
	}
	if v, _ := edit.GetJSON(asideFile(home, 1, "models.json"), "keepMe"); v != "yes" {
		t.Fatal("a key uni-switch doesn't own was dropped")
	}
	// `aside account use 0`: uni-switch follows it on its next look
	writeFile(t, filepath.Join(home, ".aside", "accounts.json"), strings.Replace(asideAccountsJSON, `"currentAccountId": 1`, `"currentAccountId": 0`, 1))
	if got := mustFindAside(t).Path; got != asideFile(home, 0, "settings.json") {
		t.Fatalf("after account use 0: %s", got)
	}
}

// An accounts.json that can't be read says nothing about which account is
// active: uni-switch keeps to u0, as before, rather than taking it for none.
func TestAsideUnreadableAccountsKeepU0(t *testing.T) {
	for name, body := range map[string]string{
		"missing":        "",
		"not json":       `{"currentAccountId": 1, "accounts": [`,
		"no active":      `{"accounts": [{"id": 1}]}`,
		"no such folder": `{"currentAccountId": 7, "accounts": [{"id": 7}]}`,
		"negative":       `{"currentAccountId": -1}`,
	} {
		t.Run(name, func(t *testing.T) {
			home, calls := asideAccountsHome(t)
			path := filepath.Join(home, ".aside", "accounts.json")
			if body == "" {
				os.Remove(path)
			} else {
				writeFile(t, path, body)
			}
			a := mustFindAside(t)
			if a.Path != asideFile(home, 0, "settings.json") {
				t.Fatalf("path %s", a.Path)
			}
			if err := a.Pick("model", "uni-switch/relay/glm-4.6"); err != nil {
				t.Fatal(err)
			}
			onlyAccount(t, *calls, "u0")
			if body != "" && readFile(path) != body {
				t.Fatal("accounts.json was written")
			}
		})
	}
}

// The account picked on uni-switch (the row's account field, `uni-switch aside
// account u2`) wins over Aside's active one; Default follows Aside again.
func TestAsidePickedAccountOverridesActive(t *testing.T) {
	home, calls := asideAccountsHome(t)
	a := mustFindAside(t)
	f := a.Field("account")
	if f == nil {
		t.Fatal("no account field")
	}
	opts := f.Options(a.Values())
	var values, notes []string
	for _, o := range opts {
		values, notes = append(values, o.Value), append(notes, o.Note)
	}
	if !reflect.DeepEqual(values, []string{"u0", "u1", "u2"}) || !strings.Contains(notes[1], "active in Aside") || strings.Contains(notes[0], "active") {
		t.Fatalf("options %v %v", values, notes)
	}
	if f.Get() != "" {
		t.Fatalf("nothing picked reads %q", f.Get())
	}
	if err := a.Pick("account", "u2"); err != nil {
		t.Fatal(err)
	}
	a = mustFindAside(t)
	if a.Field("account").Get() != "u2" || a.Path != asideFile(home, 2, "settings.json") {
		t.Fatalf("picked u2: %q %s", a.Field("account").Get(), a.Path)
	}
	*calls = nil
	if err := a.Pick("model", "uni-switch/relay/glm-4.6"); err != nil {
		t.Fatal(err)
	}
	onlyAccount(t, *calls, "u2")
	if !sameJSONText(t, readFile(asideFile(home, 1, "settings.json")), asideU1Settings) {
		t.Fatal("the active account was written though u2 is picked")
	}
	if err := a.Pick("account", "u9"); err == nil {
		t.Fatal("an account with no folder was taken")
	}
	if err := a.Pick("account", ""); err != nil {
		t.Fatal(err)
	}
	if a := mustFindAside(t); a.Field("account").Get() != "" || a.Path != asideFile(home, 1, "settings.json") {
		t.Fatal("default does not follow Aside again")
	}
	// one account only: nothing to pick
	writeFile(t, filepath.Join(home, ".aside", "accounts.json"), `{"currentAccountId": 0, "accounts": [{"id": 0, "provider": "anonymous", "mode": "local"}]}`)
	if a := mustFindAside(t); len(a.Field("account").Options(a.Values())) != 0 {
		t.Fatal("a single account offers a choice")
	}
}

// What uni-switch remembers of an account's settings, to put back on
// Disconnect, is that account's: switching accounts never restores one
// account's model into another.
func TestAsideRestorePointsArePerAccount(t *testing.T) {
	home, _ := asideAccountsHome(t)
	accounts := filepath.Join(home, ".aside", "accounts.json")
	use := func(id string) {
		writeFile(t, accounts, strings.Replace(asideAccountsJSON, `"currentAccountId": 1`, `"currentAccountId": `+id, 1))
	}
	use("0")
	if err := mustFindAside(t).Pick("model", "uni-switch/relay/glm-4.6"); err != nil {
		t.Fatal(err)
	}
	use("1")
	if err := mustFindAside(t).Pick("model", "uni-switch/relay/glm-4.6"); err != nil {
		t.Fatal(err)
	}
	if err := mustFindAside(t).Disconnect(); err != nil {
		t.Fatal(err)
	}
	if !sameJSONText(t, readFile(asideFile(home, 1, "settings.json")), asideU1Settings) {
		t.Fatalf("u1 restored to %s", readFile(asideFile(home, 1, "settings.json")))
	}
	if _, ok := edit.GetJSON(asideFile(home, 1, "models.json"), "providers.uni-switch"); ok {
		t.Fatal("u1's provider kept")
	}
	if got, _ := edit.GetJSON(asideFile(home, 0, "settings.json"), "defaultModel.modelId"); got != "relay/glm-4.6" {
		t.Fatalf("u1's disconnect changed u0: %s", got)
	}
	use("0")
	if err := mustFindAside(t).Disconnect(); err != nil {
		t.Fatal(err)
	}
	if !sameJSONText(t, readFile(asideFile(home, 0, "settings.json")), asideU0Settings) {
		t.Fatalf("u0 restored to %s", readFile(asideFile(home, 0, "settings.json")))
	}
}

// u/1/models.json was group-writable on #1499: connecting keeps its mode.
func TestAsideKeepsModelsPermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("no Unix modes")
	}
	home, _ := asideAccountsHome(t)
	models := asideFile(home, 1, "models.json")
	if err := os.Chmod(models, 0o664); err != nil {
		t.Fatal(err)
	}
	if err := mustFindAside(t).Connect(); err != nil {
		t.Fatal(err)
	}
	st, err := os.Stat(models)
	if err != nil {
		t.Fatal(err)
	}
	if st.Mode().Perm() != 0o664 {
		t.Fatalf("mode %v", st.Mode().Perm())
	}
}

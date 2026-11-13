package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/edit"
	"github.com/dieqiyun/uni-switch/backend/internal/provider"
	"github.com/dieqiyun/uni-switch/backend/internal/usage"
	"github.com/tidwall/gjson"
)

// zooCodeHere is Zoo Code in a VS Code User folder under home, the
// extension installed as the Marketplace installs it.
func zooCodeHere(t *testing.T, home string) (*Agent, string) {
	t.Helper()
	dir := filepath.Join(home, "vscode", "User")
	exts := filepath.Join(home, ".vscode", "extensions")
	writeFile(t, filepath.Join(exts, "zoocodeorganization.zoo-code-3.90.0", "package.json"), `{"name":"zoo-code","version":"3.90.0"}`)
	return zooCodeAt(dir, exts), dir
}

// switched on, uni-switch's file is what zoo-code.autoImportSettingsPath names,
// in the shape Zoo Code's importSettingsFromPath takes, its profile on the
// gateway with Zoo Code's key; switched off, settings.json is as it was,
// the user's own import path back, and uni-switch's file gone
func TestZooCode(t *testing.T) {
	home := syncHome(t)
	a, _ := zooCodeHere(t, home)
	settings := `{
	// my editor
	"editor.fontSize": 14,
	"zoo-code.autoImportSettingsPath": "~/zoo-team.json",
}
`
	writeFile(t, a.Path, settings)
	f := a.Field("model")
	if !a.Detected() || a.Wired() || f.Get() != "" || a.Notice() != "" {
		t.Fatalf("before: %v %v %q", a.Detected(), a.Wired(), f.Get())
	}
	if err := a.Connect(); err != nil {
		t.Fatal(err)
	}
	file := zooCodeImportPath(filepath.Dir(a.Path))
	if f.Get() != "uni-switch/relay/glm-4.6" || !a.Wired() || a.Check() != "" || a.Notice() == "" {
		t.Fatalf("connected: %q %v %q %q", f.Get(), a.Wired(), a.Check(), a.Notice())
	}
	if v, _ := edit.GetJSON(a.Path, zooImport); v != file {
		t.Fatalf("import path %q, want %q\n%s", v, file, readFile(a.Path))
	}
	for _, keep := range []string{"// my editor", `"editor.fontSize": 14`} {
		if !strings.Contains(readFile(a.Path), keep) {
			t.Fatalf("settings lost %q:\n%s", keep, readFile(a.Path))
		}
	}
	b := readFile(file)
	if !gjson.Valid(b) {
		t.Fatalf("not JSON:\n%s", b)
	}
	// what providerProfilesSchema and the openai provider's schema need:
	// a current profile that is one of apiConfigs, and a modelInfo with
	// contextWindow and supportsPromptCache
	for k, want := range map[string]string{
		"providerProfiles.currentApiConfigName":                                            "uni-switch",
		"providerProfiles.apiConfigs.uni-switch.apiProvider":                               "openai",
		"providerProfiles.apiConfigs.uni-switch.openAiBaseUrl":                             gatewayV1(),
		"providerProfiles.apiConfigs.uni-switch.openAiApiKey":                              agentKey("zoo-code"),
		"providerProfiles.apiConfigs.uni-switch.openAiModelId":                             "relay/glm-4.6",
		"providerProfiles.apiConfigs.uni-switch.openAiCustomModelInfo.contextWindow":       "204800",
		"providerProfiles.apiConfigs.uni-switch.openAiCustomModelInfo.supportsPromptCache": "false",
	} {
		if got := gjson.Get(b, k).String(); got != want {
			t.Errorf("%s = %q, want %q", k, got, want)
		}
	}
	if gjson.Get(b, "globalSettings").Exists() {
		t.Fatalf("uni-switch's import sets Zoo Code's own settings:\n%s", b)
	}
	// the catalog gives glm-4.6 no output limit: Zoo Code's own default
	// stands, not a 0 that would cap every reply
	if r := gjson.Get(b, "providerProfiles.apiConfigs.uni-switch.openAiCustomModelInfo.maxTokens"); r.Exists() {
		t.Fatalf("maxTokens %s with no output limit known", r.Raw)
	}

	// another model: the file follows, the setting stays uni-switch's
	if err := provider.Save(provider.Provider{ID: "relay", Name: "Relay", Key: "k", Chat: "http://127.0.0.1:1/v1", Models: []string{"glm-4.6", "other"}}); err != nil {
		t.Fatal(err)
	}
	if err := a.Apply("model", "uni-switch/relay/other"); err != nil {
		t.Fatal(err)
	}
	if f.Get() != "uni-switch/relay/other" {
		t.Fatalf("other: %q\n%s", f.Get(), readFile(file))
	}

	// the key in the file written over: Sync writes it again, as Zoo Code
	// reads the file on each start
	writeFile(t, file, strings.ReplaceAll(readFile(file), agentKey("zoo-code"), "stale"))
	if c := a.Check(); !strings.Contains(c, "uni-switch-import.json") {
		t.Fatalf("check: %q", c)
	}
	if err := a.Sync(); err != nil {
		t.Fatal(err)
	}
	if c := a.Check(); c != "" {
		t.Fatalf("synced, check: %q\n%s", c, readFile(file))
	}

	if err := a.Disconnect(); err != nil {
		t.Fatal(err)
	}
	if readFile(a.Path) != settings || a.Wired() || f.Get() != "" {
		t.Fatalf("disconnected: %v %q\n%s", a.Wired(), f.Get(), readFile(a.Path))
	}
	if _, err := os.Stat(file); !os.IsNotExist(err) {
		t.Fatalf("uni-switch's import file left: %v", err)
	}
}

// with no settings.json: made, and emptied of uni-switch's setting again
func TestZooCodeFresh(t *testing.T) {
	home := syncHome(t)
	a, _ := zooCodeHere(t, home)
	if err := a.Connect(); err != nil {
		t.Fatal(err)
	}
	if a.Field("model").Get() != "uni-switch/relay/glm-4.6" || a.Check() != "" {
		t.Fatalf("fresh: %q\n%s", a.Field("model").Get(), readFile(a.Path))
	}
	if err := a.Disconnect(); err != nil {
		t.Fatal(err)
	}
	if v, ok := edit.GetJSON(a.Path, zooImport); ok || a.Wired() {
		t.Fatalf("disconnected: %q\n%s", v, readFile(a.Path))
	}
}

// a VS Code profile reads its own settings.json: the setting goes in each
// profile's, one made later is given it on Sync, and each is put back
func TestZooCodeProfiles(t *testing.T) {
	home := syncHome(t)
	a, dir := zooCodeHere(t, home)
	work := filepath.Join(dir, "profiles", "-5f1c2a")
	writeFile(t, filepath.Join(dir, "globalStorage", "storage.json"), `{"userDataProfiles": [{"location": "-5f1c2a", "name": "Work"}]}`)
	workSettings := "{\n\t// work\n\t\"zoo-code.autoImportSettingsPath\": \"/srv/zoo.json\"\n}\n"
	writeFile(t, filepath.Join(work, "settings.json"), workSettings)
	if err := a.Connect(); err != nil {
		t.Fatal(err)
	}
	if v, _ := edit.GetJSON(filepath.Join(work, "settings.json"), zooImport); v != zooCodeImportPath(dir) {
		t.Fatalf("work profile's import path %q", v)
	}
	writeFile(t, filepath.Join(dir, "globalStorage", "storage.json"), `{"userDataProfiles": [{"location": "-5f1c2a", "name": "Work"}, {"location": "new1", "name": "New"}]}`)
	writeFile(t, filepath.Join(dir, "profiles", "new1", "keybindings.json"), "[]\n")
	if err := a.Sync(); err != nil {
		t.Fatal(err)
	}
	newSettings := filepath.Join(dir, "profiles", "new1", "settings.json")
	if v, _ := edit.GetJSON(newSettings, zooImport); v != zooCodeImportPath(dir) {
		t.Fatalf("new profile's import path %q", v)
	}
	if err := a.Disconnect(); err != nil {
		t.Fatal(err)
	}
	if readFile(filepath.Join(work, "settings.json")) != workSettings {
		t.Fatalf("work profile not as it was:\n%s", readFile(filepath.Join(work, "settings.json")))
	}
	if _, ok := edit.GetJSON(newSettings, zooImport); ok {
		t.Fatalf("new profile kept uni-switch's import path:\n%s", readFile(newSettings))
	}
}

// listed only with the extension installed; its requests name it
func TestZooCodeDetectAndUA(t *testing.T) {
	home := syncHome(t)
	dir := filepath.Join(home, "vscode", "User")
	writeFile(t, filepath.Join(dir, "settings.json"), "{}\n")
	exts := filepath.Join(home, ".vscode", "extensions")
	writeFile(t, filepath.Join(exts, "rooveterinaryinc.roo-cline-3.30.0", "package.json"), "{}")
	if zooCodeAt(dir, exts).Detected() {
		t.Fatal("detected without Zoo Code installed")
	}
	writeFile(t, filepath.Join(exts, "zoocodeorganization.zoo-code-3.90.0", "package.json"), "{}")
	if !zooCodeAt(dir, exts).Detected() {
		t.Fatal("not detected with Zoo Code installed")
	}
	if got := usage.AgentOf("ZooCode/3.90.0"); got != "zoo-code" {
		t.Fatalf("AgentOf = %q", got)
	}
}

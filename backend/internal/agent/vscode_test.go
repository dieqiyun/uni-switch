package agent

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/edit"
	"github.com/dieqiyun/uni-switch/backend/internal/provider"
	"github.com/dieqiyun/uni-switch/backend/internal/usage"
	"github.com/tidwall/gjson"
	"github.com/tidwall/jsonc"
)

// VS Code's chat: switched on, uni-switch's group joins the user's in
// chatLanguageModels.json, its token in each model's Authorization header
// and no apiKey (VS Code reads that from its secret storage only), and a new
// chat starts on the model picked; switched off, both files are as they were
func TestVSCode(t *testing.T) {
	home := syncHome(t)
	dir := filepath.Join(home, "vscode", "User")
	a := vscodeAt(dir)
	settings := `{
	// my editor
	"editor.fontSize": 14,
	"chat.defaultModel": "gpt-5",
	"chat.agent.enabled": true,
}
`
	groups := `// models I added
[
	{
		"name": "Ollama",
		"vendor": "ollama", // local
		"url": "http://localhost:11434"
	},
]
`
	writeFile(t, a.Path, settings)
	lm := filepath.Join(dir, "chatLanguageModels.json")
	writeFile(t, lm, groups)
	f := a.Field("model")
	if f.Get() != "gpt-5" || !a.Detected() || a.Wired() {
		t.Fatalf("before: %q %v", f.Get(), a.Wired())
	}
	if err := a.Connect(); err != nil {
		t.Fatal(err)
	}
	if f.Get() != "uni-switch/relay/glm-4.6" || !a.Wired() || a.Check() != "" {
		t.Fatalf("connected: %q %v %q", f.Get(), a.Wired(), a.Check())
	}
	if v, _ := edit.GetJSON(a.Path, vscodeDefault); v != "relay/glm-4.6" {
		t.Fatalf("chat.defaultModel %q\n%s", v, readFile(a.Path))
	}
	for _, keep := range []string{"// my editor", `"editor.fontSize": 14`, `"chat.agent.enabled": true`} {
		if !strings.Contains(readFile(a.Path), keep) {
			t.Fatalf("settings lost %q:\n%s", keep, readFile(a.Path))
		}
	}
	var list []map[string]any
	if err := json.Unmarshal(jsonc.ToJSON([]byte(readFile(lm))), &list); err != nil || len(list) != 2 || list[0]["vendor"] != "ollama" {
		t.Fatalf("groups: %v %v\n%s", err, list, readFile(lm))
	}
	for _, keep := range []string{"// models I added", "// local"} {
		if !strings.Contains(readFile(lm), keep) {
			t.Fatalf("groups lost %q:\n%s", keep, readFile(lm))
		}
	}
	g, _ := edit.GetJSONItem(lm, vscodeGroup)
	for k, want := range map[string]string{
		"vendor": "customendpoint", "name": "uni-switch", "apiType": "chat-completions",
		"models.0.id": "relay/glm-4.6", "models.0.url": gatewayV1() + "/chat/completions",
		"models.0.toolCalling": "true", "models.0.contextWindow": "204800",
		"models.0.requestHeaders.Authorization": "Bearer uni-switch",
	} {
		if got := gjson.Get(g, k).String(); got != want {
			t.Errorf("%s = %q, want %q", k, got, want)
		}
	}
	if gjson.Get(g, "apiKey").Exists() || gjson.Get(g, "models.0.apiKey").Exists() || gjson.Get(g, "url").Exists() {
		t.Fatalf("a key or a discovery url: %s", g)
	}
	if n := gjson.Get(g, "models.0.maxOutputTokens").Int(); n <= 0 || n > 204800/4 {
		t.Fatalf("output %d", n)
	}

	// a model added to the catalog is listed once synced
	if err := provider.Save(provider.Provider{ID: "relay", Name: "Relay", Key: "k", Chat: "http://127.0.0.1:1/v1", Models: []string{"glm-4.6", "other"}}); err != nil {
		t.Fatal(err)
	}
	if err := a.Sync(); err != nil {
		t.Fatal(err)
	}
	if g, _ := edit.GetJSONItem(lm, vscodeGroup); gjson.Get(g, "models.#").Int() != 2 {
		t.Fatalf("not synced: %s", g)
	}

	// the gateway's URL changed under it: Check says so
	b := readFile(lm)
	writeFile(t, lm, strings.ReplaceAll(b, gatewayV1(), "http://127.0.0.1:9/v1"))
	if c := a.Check(); !strings.Contains(c, "chatLanguageModels.json") {
		t.Fatalf("check: %q", c)
	}
	writeFile(t, lm, b)

	// one of VS Code's own picked: uni-switch's models stay in its list
	if err := a.Apply("model", "auto"); err != nil {
		t.Fatal(err)
	}
	if f.Get() != "auto" || !a.Wired() {
		t.Fatalf("own: %q %v", f.Get(), a.Wired())
	}
	if err := a.Apply("model", "uni-switch/relay/other"); err != nil {
		t.Fatal(err)
	}
	// switched off: back to auto, the last of the user's own, group gone
	if err := a.Disconnect(); err != nil {
		t.Fatal(err)
	}
	if v, _ := edit.GetJSON(a.Path, vscodeDefault); v != "auto" || a.Wired() {
		t.Fatalf("disconnected: %q %v", v, a.Wired())
	}
	if _, ok := edit.GetJSONItem(lm, vscodeGroup); ok {
		t.Fatalf("group left:\n%s", readFile(lm))
	}
}

// on and off again with nothing else changed: both files byte for byte
func TestVSCodeRoundTrip(t *testing.T) {
	home := syncHome(t)
	dir := filepath.Join(home, "vscode", "User")
	a := vscodeAt(dir)
	settings := "{\n\t// mine\n\t\"chat.defaultModel\": \"claude-sonnet-4.5\",\n\t\"files.autoSave\": \"afterDelay\"\n}\n"
	groups := "[\n\t{\n\t\t\"name\": \"Ollama\",\n\t\t\"vendor\": \"ollama\"\n\t}\n]\n"
	lm := filepath.Join(dir, "chatLanguageModels.json")
	writeFile(t, a.Path, settings)
	writeFile(t, lm, groups)
	if err := a.Connect(); err != nil {
		t.Fatal(err)
	}
	if err := a.Disconnect(); err != nil {
		t.Fatal(err)
	}
	if readFile(a.Path) != settings || readFile(lm) != groups {
		t.Fatalf("not as it was:\n%s\n%s", readFile(a.Path), readFile(lm))
	}

	// no files at all: made, and emptied of uni-switch again
	home = syncHome(t)
	dir = filepath.Join(home, "fresh", "User")
	a = vscodeAt(dir)
	lm = filepath.Join(dir, "chatLanguageModels.json")
	if err := a.Connect(); err != nil {
		t.Fatal(err)
	}
	if a.Field("model").Get() != "uni-switch/relay/glm-4.6" || a.Check() != "" {
		t.Fatalf("fresh: %q\n%s", a.Field("model").Get(), readFile(lm))
	}
	if err := a.Disconnect(); err != nil {
		t.Fatal(err)
	}
	if v, ok := edit.GetJSON(a.Path, vscodeDefault); ok || a.Wired() {
		t.Fatalf("fresh disconnected: %q\n%s", v, readFile(a.Path))
	}
}

// a window on one of VS Code's own profiles reads that profile's
// chatLanguageModels.json and settings.json, not the default's: uni-switch's
// group and default model go in each profile's too, a profile that uses the
// default's is left alone, and switched off every file is as it was
// (TJHHHH on Discord: the Agents window listed uni-switch, the side Chat didn't)
func TestVSCodeProfiles(t *testing.T) {
	home := syncHome(t)
	dir := filepath.Join(home, "vscode", "User")
	a := vscodeAt(dir)
	work := filepath.Join(dir, "profiles", "-5f1c2a")
	shared := filepath.Join(dir, "profiles", "3b9e")
	writeFile(t, filepath.Join(dir, "globalStorage", "storage.json"), `{
	"userDataProfiles": [
		{"location": "-5f1c2a", "name": "Work", "icon": "briefcase"},
		{"location": "3b9e", "name": "Shared", "useDefaultFlags": {"settings": true, "languageModels": true}},
		{"location": "gone", "name": "Removed"}
	]
}`)
	workSettings := "{\n\t// work\n\t\"chat.defaultModel\": \"gpt-4.1\"\n}\n"
	workGroups := "[\n\t{\n\t\t\"name\": \"Ollama\",\n\t\t\"vendor\": \"ollama\"\n\t}\n]\n"
	writeFile(t, filepath.Join(work, "settings.json"), workSettings)
	writeFile(t, filepath.Join(work, "chatLanguageModels.json"), workGroups)
	writeFile(t, filepath.Join(shared, "keybindings.json"), "[]\n")

	if err := a.Connect(); err != nil {
		t.Fatal(err)
	}
	wlm := filepath.Join(work, "chatLanguageModels.json")
	g, ok := edit.GetJSONItem(wlm, vscodeGroup)
	if !ok || gjson.Get(g, "models.0.id").String() != "relay/glm-4.6" || !strings.Contains(readFile(wlm), `"ollama"`) {
		t.Fatalf("work profile's models:\n%s", readFile(wlm))
	}
	if v, _ := edit.GetJSON(filepath.Join(work, "settings.json"), vscodeDefault); v != "relay/glm-4.6" {
		t.Fatalf("work profile's chat.defaultModel %q", v)
	}
	for _, f := range []string{"settings.json", "chatLanguageModels.json"} {
		if isFile(filepath.Join(shared, f)) || isFile(filepath.Join(dir, "profiles", "gone", f)) {
			t.Fatalf("wrote %s of a profile that uses the default's, or of none", f)
		}
	}

	// a profile made after: given the group when synced
	writeFile(t, filepath.Join(dir, "globalStorage", "storage.json"), `{"userDataProfiles": [{"location": "-5f1c2a", "name": "Work"}, {"location": "new1", "name": "New"}]}`)
	writeFile(t, filepath.Join(dir, "profiles", "new1", "keybindings.json"), "[]\n")
	if err := a.Sync(); err != nil {
		t.Fatal(err)
	}
	if _, ok := edit.GetJSONItem(filepath.Join(dir, "profiles", "new1", "chatLanguageModels.json"), vscodeGroup); !ok {
		t.Fatal("new profile not given uni-switch's group")
	}

	if err := a.Disconnect(); err != nil {
		t.Fatal(err)
	}
	if readFile(filepath.Join(work, "settings.json")) != workSettings || readFile(wlm) != workGroups {
		t.Fatalf("work profile not as it was:\n%s\n%s", readFile(filepath.Join(work, "settings.json")), readFile(wlm))
	}
	if _, ok := edit.GetJSONItem(filepath.Join(dir, "profiles", "new1", "chatLanguageModels.json"), vscodeGroup); ok {
		t.Fatal("new profile kept uni-switch's group")
	}
}

// its requests come with GitHubCopilotChat/<version>
func TestVSCodeUA(t *testing.T) {
	if got := usage.AgentOf("GitHubCopilotChat/0.69.0"); got != "vscode" {
		t.Fatalf("%q", got)
	}
}

// VS Code's Agents window runs Codex under the name vscode_agent_host,
// its User-Agent and originator: its calls are VS Code's, not a row of
// their own named vscode_agent_host (justruman on Discord). VS Code's
// Copilot-proxied Codex and Claude names are not it.
func TestVSCodeAgentHostUA(t *testing.T) {
	for _, ua := range []string{"vscode_agent_host/0.163.0 (Mac OS 26.2.0; arm64) unknown (vscode_agent_host; 0.1.0)", "vscode_agent_host"} {
		if got := usage.AgentOf(ua); got != "vscode" {
			t.Errorf("%q: %q", ua, got)
		}
	}
}

// VS Code Insiders is its own row (wani on Discord): its User folder is
// "Code - Insiders" beside VS Code's "Code", connected it writes only there,
// and its models send a token of its own, since its chat's User-Agent is
// Stable's; VS Code's files are left alone
func TestVSCodeInsiders(t *testing.T) {
	home := syncHome(t)
	ins, err := Find("vscode-insiders")
	if err != nil {
		t.Fatal(err)
	}
	st, err := Find("vscode")
	if err != nil {
		t.Fatal(err)
	}
	if ins.Name != "VS Code Insiders" || ins.Bin != "code-insiders" || filepath.Base(filepath.Dir(ins.Dir)) != "Code - Insiders" || filepath.Base(ins.Dir) != "User" ||
		filepath.Dir(filepath.Dir(ins.Dir)) != filepath.Dir(filepath.Dir(st.Dir)) || ins.UA != nil {
		t.Fatalf("insiders: %+v\nstable dir %s", ins, st.Dir)
	}
	if !strings.HasPrefix(ins.Dir, home) {
		t.Fatalf("insiders dir %s outside %s", ins.Dir, home)
	}
	for _, n := range []string{"code-insiders", "vs-code-insiders"} {
		if a, err := Find(n); err != nil || a.ID != "vscode-insiders" {
			t.Fatalf("Find(%q): %v %v", n, a, err)
		}
	}
	writeFile(t, ins.Path, "{}\n")
	writeFile(t, st.Path, `{"chat.defaultModel": "gpt-5"}`+"\n")
	if !ins.Detected() {
		t.Fatal("not detected")
	}
	if err := ins.Connect(); err != nil {
		t.Fatal(err)
	}
	g, ok := edit.GetJSONItem(filepath.Join(ins.Dir, "chatLanguageModels.json"), vscodeGroup)
	if !ok || gjson.Get(g, "models.0.requestHeaders.Authorization").String() != "Bearer uni-switch-vscode-insiders" || !ins.Wired() || ins.Check() != "" {
		t.Fatalf("connected: %s %v %q", g, ins.Wired(), ins.Check())
	}
	if readFile(st.Path) != `{"chat.defaultModel": "gpt-5"}`+"\n" || isFile(filepath.Join(st.Dir, "chatLanguageModels.json")) || st.Wired() {
		t.Fatalf("VS Code touched:\n%s", readFile(st.Path))
	}
	// its requests are its own, Stable's still Stable's
	if got := usage.AgentOf("vscode-insiders"); got != "vscode-insiders" {
		t.Fatalf("token: %q", got)
	}
	if got := usage.AgentOf("GitHubCopilotChat/0.69.0"); got != "vscode" {
		t.Fatalf("UA: %q", got)
	}
	if err := ins.Disconnect(); err != nil {
		t.Fatal(err)
	}
	if _, ok := edit.GetJSONItem(filepath.Join(ins.Dir, "chatLanguageModels.json"), vscodeGroup); ok || ins.Wired() {
		t.Fatal("disconnected, uni-switch's group kept")
	}
}

func TestVSCodium(t *testing.T) {
	syncHome(t)
	c, err := Find("vscodium")
	if err != nil {
		t.Fatal(err)
	}
	vs, err := Find("vscode")
	if err != nil {
		t.Fatal(err)
	}
	if c.Name != "VSCodium" || c.Bin != "codium" || c.UA != nil || !strings.Contains(c.Dir, filepath.Join("VSCodium", "User")) {
		t.Fatalf("vscodium: %+v", c)
	}
	for _, alias := range []string{"codium", "vscodium-chat"} {
		a, err := Find(alias)
		if err != nil || a.ID != "vscodium" {
			t.Fatalf("Find(%q): %v %v", alias, a, err)
		}
	}
	writeFile(t, c.Path, "{}\n")
	writeFile(t, vs.Path, `{"chat.defaultModel":"gpt-5"}`+"\n")
	if !c.Detected() {
		t.Fatal("VSCodium not detected")
	}
	if err := c.Connect(); err != nil {
		t.Fatal(err)
	}
	g, ok := edit.GetJSONItem(filepath.Join(c.Dir, "chatLanguageModels.json"), vscodeGroup)
	if !ok || gjson.Get(g, "models.0.requestHeaders.Authorization").String() != "Bearer uni-switch-vscodium" || !c.Wired() || c.Check() != "" {
		t.Fatalf("connected: %s %v %q", g, c.Wired(), c.Check())
	}
	if isFile(filepath.Join(vs.Dir, "chatLanguageModels.json")) || readFile(vs.Path) != `{"chat.defaultModel":"gpt-5"}`+"\n" || vs.Wired() {
		t.Fatalf("VS Code touched:\n%s", readFile(vs.Path))
	}
	if got := usage.AgentOf("vscodium"); got != "vscodium" {
		t.Fatalf("token: %q", got)
	}
	if err := c.Disconnect(); err != nil {
		t.Fatal(err)
	}
	if _, ok := edit.GetJSONItem(filepath.Join(c.Dir, "chatLanguageModels.json"), vscodeGroup); ok || c.Wired() {
		t.Fatal("disconnected, uni-switch's group kept")
	}
}

func TestVSCodiumModelsUseTheirOwnVisibility(t *testing.T) {
	syncHome(t)
	if err := provider.Save(provider.Provider{ID: "relay", Name: "Relay", Key: "k", Chat: "http://127.0.0.1:1/v1", Models: []string{"visible", "hidden"}}); err != nil {
		t.Fatal(err)
	}
	if err := provider.SetHiddenModels("vscodium", []string{"relay/hidden"}); err != nil {
		t.Fatal(err)
	}
	if err := provider.SetHiddenModels("vscode", []string{"relay/visible"}); err != nil {
		t.Fatal(err)
	}
	has := func(k vscodeKind, id string) bool {
		b, err := json.Marshal(vscodeGroupJSON(k, vscodeEndpoint))
		if err != nil {
			t.Fatal(err)
		}
		return gjson.GetBytes(b, "models.#(id==\""+id+"\")").Exists()
	}
	if has(vscodiumKind, "relay/hidden") || !has(vscodiumKind, "relay/visible") {
		t.Fatal("VSCodium did not use its own hidden-model list")
	}
	if !has(vscodeStable, "relay/hidden") || has(vscodeStable, "relay/visible") {
		t.Fatal("VS Code did not retain its own hidden-model list")
	}
}

// copilotChat0481Providers is contributes.languageModelChatProviders of
// the Marketplace's GitHub Copilot Chat 0.48.1, the last one VSCodium can
// install (its configuration schemas left out): an OpenAI Compatible
// provider, no Custom Endpoint.
const copilotChat0481Providers = `[{"vendor": "copilot", "displayName": "Copilot"}, {"vendor": "copilotcli", "displayName": "Copilot CLI", "when": "false"}, {"vendor": "claude-code", "displayName": "Claude Code", "when": "false"}, {"vendor": "anthropic", "displayName": "Anthropic"}, {"vendor": "xai", "displayName": "xAI"}, {"vendor": "gemini", "displayName": "Google"}, {"vendor": "openrouter", "displayName": "OpenRouter"}, {"vendor": "openai", "displayName": "OpenAI"}, {"vendor": "ollama", "displayName": "Ollama"}, {"vendor": "customoai", "when": "productQualityType != 'stable'", "displayName": "OpenAI Compatible"}, {"vendor": "azure", "displayName": "Azure"}]`

// installCopilotChat installs a Copilot Chat of a version declaring
// providers in VSCodium's extensions folder under home, listed in
// extensions.json as VSCodium lists it.
func installCopilotChat(t *testing.T, home, version, providers string) {
	t.Helper()
	exts := filepath.Join(home, ".vscode-oss", "extensions")
	folder := "github.copilot-chat-" + version
	writeFile(t, filepath.Join(exts, folder, "package.json"), `{"name":"copilot-chat","publisher":"GitHub","version":"`+version+`","contributes":{"languageModelChatProviders":`+providers+`}}`)
	writeFile(t, filepath.Join(exts, "extensions.json"), `[{"identifier":{"id":"github.copilot-chat","uuid":"7ec7d6e6-b89e-4cc5-a59b-d6c4d238246f"},"version":"`+version+`","location":{"$mid":1,"path":"/x","scheme":"file"},"relativeLocation":"`+folder+`","metadata":{"source":"gallery"}}]`)
}

func TestVSCodiumCopilotChatOpenAICompatible(t *testing.T) {
	home := syncHome(t)
	if err := provider.Save(provider.Provider{ID: "relay", Name: "Relay", Key: "k", Chat: "http://127.0.0.1:1/v1", Models: []string{"m"}}); err != nil {
		t.Fatal(err)
	}
	installCopilotChat(t, home, "0.48.1", copilotChat0481Providers)
	c, err := Find("vscodium")
	if err != nil {
		t.Fatal(err)
	}
	lm := filepath.Join(c.Dir, "chatLanguageModels.json")
	// the user's own groups
	writeFile(t, lm, `[{"name":"mine","vendor":"customoai","models":[{"id":"x","name":"x","url":"http://h/v1","toolCalling":true,"vision":false,"maxInputTokens":1000,"maxOutputTokens":100}]},{"name":"Theirs","vendor":"customendpoint","models":[]}]`+"\n")
	writeFile(t, c.Path, "{}\n")
	if err := c.Connect(); err != nil {
		t.Fatal(err)
	}
	g, ok := edit.GetJSONItem(lm, vscodeGroupOf(vscodeOAI))
	if !ok {
		t.Fatalf("no customoai group:\n%s", readFile(lm))
	}
	m := gjson.Get(g, `models.#(id=="relay/m")`)
	if m.Get("url").String() != vscodeURL() || m.Get("requestHeaders.x-api-key").String() != "uni-switch-vscodium" ||
		m.Get("requestHeaders.Authorization").Exists() || m.Get("contextWindow").Exists() || gjson.Get(g, "apiKey").Exists() ||
		m.Get("maxInputTokens").Int() <= 0 || m.Get("maxOutputTokens").Int() <= 0 || !m.Get("toolCalling").Bool() {
		t.Fatalf("model: %s", m.Raw)
	}
	if _, ok := edit.GetJSONItem(lm, vscodeGroup); ok {
		t.Fatalf("Custom Endpoint group kept:\n%s", readFile(lm))
	}
	if _, ok := edit.GetJSONItem(lm, map[string]string{"name": "mine", "vendor": "customoai"}); !ok {
		t.Fatalf("user's group lost:\n%s", readFile(lm))
	}
	if _, ok := edit.GetJSONItem(lm, map[string]string{"name": "Theirs", "vendor": "customendpoint"}); !ok {
		t.Fatalf("user's group lost:\n%s", readFile(lm))
	}
	if !c.Wired() || c.Check() != "" {
		t.Fatalf("wired %v, check %q", c.Wired(), c.Check())
	}
	if n := c.Notice(); !strings.Contains(n, "OpenAI Compatible") || !strings.Contains(n, "personal Copilot plan") {
		t.Fatalf("notice: %q", n)
	}
	if got := usage.AgentOf("vscodium"); got != "vscodium" {
		t.Fatalf("token: %q", got)
	}

	// the Custom Endpoint group an older uni-switch wrote: Sync, as at uni-switch's
	// start, writes it for customoai
	if err := edit.DelJSONItem(lm, vscodeGroupOf(vscodeOAI)); err != nil {
		t.Fatal(err)
	}
	if err := edit.SetJSONItem(lm, vscodeGroup, vscodeGroupJSON(vscodiumKind, vscodeEndpoint)); err != nil {
		t.Fatal(err)
	}
	if c.Check() == "" {
		t.Fatal("Check missed a Custom Endpoint group on 0.48.1")
	}
	if err := c.Sync(); err != nil {
		t.Fatal(err)
	}
	if _, ok := edit.GetJSONItem(lm, vscodeGroup); ok || c.Check() != "" {
		t.Fatalf("not rewritten (%q):\n%s", c.Check(), readFile(lm))
	}
	if _, ok := edit.GetJSONItem(lm, vscodeGroupOf(vscodeOAI)); !ok {
		t.Fatalf("no customoai group after Sync:\n%s", readFile(lm))
	}

	// a Copilot Chat with a Custom Endpoint provider: Sync writes that
	installCopilotChat(t, home, "0.50.0", `[{"vendor":"copilot"},{"vendor":"customendpoint"},{"vendor":"customoai"}]`)
	if c.Check() == "" {
		t.Fatal("Check missed the group written for the other provider")
	}
	if err := c.Sync(); err != nil {
		t.Fatal(err)
	}
	if _, ok := edit.GetJSONItem(lm, vscodeGroupOf(vscodeOAI)); ok {
		t.Fatalf("customoai group kept:\n%s", readFile(lm))
	}
	g, ok = edit.GetJSONItem(lm, vscodeGroup)
	if !ok || gjson.Get(g, "models.0.requestHeaders.Authorization").String() != "Bearer uni-switch-vscodium" || c.Check() != "" {
		t.Fatalf("custom endpoint: %s %q", g, c.Check())
	}

	// neither provider: said so
	installCopilotChat(t, home, "0.51.0", `[{"vendor":"copilot"}]`)
	if ch := c.Check(); !strings.Contains(ch, "neither") {
		t.Fatalf("check: %q", ch)
	}
	if n := c.Notice(); !strings.Contains(n, "neither") {
		t.Fatalf("notice: %q", n)
	}

	if err := c.Disconnect(); err != nil {
		t.Fatal(err)
	}
	for _, v := range vscodeVendors {
		if _, ok := edit.GetJSONItem(lm, vscodeGroupOf(v)); ok {
			t.Fatalf("disconnected, %s group kept:\n%s", v, readFile(lm))
		}
	}
	if _, ok := edit.GetJSONItem(lm, map[string]string{"name": "mine", "vendor": "customoai"}); !ok {
		t.Fatalf("user's group lost on disconnect:\n%s", readFile(lm))
	}
}

func TestVSCodeChatInstalledWithoutExtensionsJSON(t *testing.T) {
	dir := t.TempDir()
	if c := vscodeChatOf(filepath.Join(dir, "missing")); !c.none || c.vendor != vscodeEndpoint {
		t.Fatalf("missing: %+v", c)
	}
	writeFile(t, filepath.Join(dir, "github.copilot-chat-0.9.0", "package.json"), `{"contributes":{"languageModelChatProviders":[{"vendor":"customendpoint"}]}}`)
	writeFile(t, filepath.Join(dir, "github.copilot-chat-0.48.1", "package.json"), `{"contributes":{"languageModelChatProviders":`+copilotChat0481Providers+`}}`)
	writeFile(t, filepath.Join(dir, "github.copilot-chat-0.50.0", "package.json"), `{"contributes":{"languageModelChatProviders":[{"vendor":"customendpoint"}]}}`)
	writeFile(t, filepath.Join(dir, ".obsolete"), `{"github.copilot-chat-0.50.0":true}`)
	if c := vscodeChatOf(dir); c.vendor != vscodeOAI || c.version != "0.48.1" {
		t.Fatalf("got %+v", c)
	}
	writeFile(t, filepath.Join(dir, "github.copilot-chat-0.48.1", "package.json"), `{not json`)
	if c := vscodeChatOf(dir); c.vendor != vscodeEndpoint || c.none || c.unsupported {
		t.Fatalf("unreadable: %+v", c)
	}
}

// the reasoning effort picked for a model in Copilot Chat's model picker,
// which VS Code writes into uni-switch's group as
// settings["<model id>"].reasoningEffort, stays through a Sync with nothing
// changed, a Sync after the catalog changed and a model picked in uni-switch
// (chim3744 on Discord: max went out as low, the first level, after each)
func TestVSCodeKeepsThePickedEffort(t *testing.T) {
	home := syncHome(t)
	dir := filepath.Join(home, "vscode", "User")
	a := vscodeAt(dir)
	writeFile(t, a.Path, "{}\n")
	if err := a.Connect(); err != nil {
		t.Fatal(err)
	}
	lm := filepath.Join(dir, "chatLanguageModels.json")
	// what VS Code 1.142 writes on a pick (setModelConfiguration), with a
	// model uni-switch no longer lists picked before
	g, ok := edit.GetJSONItem(lm, vscodeGroup)
	if !ok {
		t.Fatalf("no group:\n%s", readFile(lm))
	}
	var group map[string]any
	if err := json.Unmarshal([]byte(g), &group); err != nil {
		t.Fatal(err)
	}
	group["settings"] = map[string]any{
		"relay/glm-4.6": map[string]any{"reasoningEffort": "max"},
		"relay/gone":    map[string]any{"reasoningEffort": "high"},
	}
	if err := edit.SetJSONItem(lm, vscodeGroup, group); err != nil {
		t.Fatal(err)
	}
	kept := func(when string) {
		t.Helper()
		g, _ := edit.GetJSONItem(lm, vscodeGroup)
		if e := gjson.Get(g, `settings.relay/glm-4\.6.reasoningEffort`).String(); e != "max" {
			t.Fatalf("%s: the picked effort is %q, want max:\n%s", when, e, g)
		}
		if e := gjson.Get(g, "settings.relay/gone.reasoningEffort").String(); e != "high" {
			t.Fatalf("%s: a gone model's effort is %q, want high:\n%s", when, e, g)
		}
	}

	before := readFile(lm)
	if err := a.Sync(); err != nil {
		t.Fatal(err)
	}
	kept("a sync with nothing changed")
	if readFile(lm) != before {
		t.Fatalf("a sync with nothing changed rewrote the file:\n%s\n---\n%s", before, readFile(lm))
	}

	if err := provider.Save(provider.Provider{ID: "relay", Name: "Relay", Key: "k", Chat: "http://127.0.0.1:1/v1", Models: []string{"glm-4.6", "other"}}); err != nil {
		t.Fatal(err)
	}
	if err := a.Sync(); err != nil {
		t.Fatal(err)
	}
	kept("a sync after the catalog changed")
	if g, _ := edit.GetJSONItem(lm, vscodeGroup); gjson.Get(g, "models.#").Int() != 2 {
		t.Fatalf("not synced: %s", g)
	}

	if err := a.Apply("model", "uni-switch/relay/other"); err != nil {
		t.Fatal(err)
	}
	kept("a model picked in uni-switch")

	// a model gone from the catalog still leaves the list
	if err := provider.Save(provider.Provider{ID: "relay", Name: "Relay", Key: "k", Chat: "http://127.0.0.1:1/v1", Models: []string{"glm-4.6"}}); err != nil {
		t.Fatal(err)
	}
	if err := a.Sync(); err != nil {
		t.Fatal(err)
	}
	kept("a sync after a model went")
	if g, _ := edit.GetJSONItem(lm, vscodeGroup); gjson.Get(g, "models.#").Int() != 1 {
		t.Fatalf("a gone model stayed: %s", g)
	}
}

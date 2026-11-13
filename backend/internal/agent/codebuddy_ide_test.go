package agent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// codebuddyIDEPlist is the CodeBuddy IDE's Info.plist as its 4.12.2 macOS
// arm64 build for codebuddy.cn ships it (CodeBuddy CN.app, from
// www.codebuddy.cn/v2/update?platform=ide-darwin-arm64), cut to its first
// keys, in their order and indentation. codebuddy.ai's build
// (CodeBuddy-darwin-arm64-4.12.2.41207131-425ab70e.zip) differs only in the
// names, CodeBuddy, and its bundle id, com.tencent.codebuddy.
const codebuddyIDEPlist = `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
  <dict>
    <key>CFBundleDisplayName</key>
    <string>CodeBuddy CN</string>
    <key>CFBundleExecutable</key>
    <string>Electron</string>
    <key>CFBundleIconFile</key>
    <string>CodeBuddy CN.icns</string>
    <key>CFBundleIdentifier</key>
    <string>com.tencent.codebuddycn</string>
    <key>CFBundleInfoDictionaryVersion</key>
    <string>6.0</string>
    <key>CFBundleName</key>
    <string>CodeBuddy CN</string>
    <key>CFBundlePackageType</key>
    <string>APPL</string>
    <key>CFBundleShortVersionString</key>
    <string>4.12.2</string>
    <key>CFBundleVersion</key>
    <string>4.12.2</string>
  </dict>
</plist>
`

// codebuddyIntlPlist is the same for codebuddy.ai's CodeBuddy.app.
var codebuddyIntlPlist = strings.NewReplacer("CodeBuddy CN", "CodeBuddy", "com.tencent.codebuddycn", "com.tencent.codebuddy").Replace(codebuddyIDEPlist)

// bjzhangwn on X: uni-switch listed CodeBuddy only as its CLI, not the IDE.
// The CodeBuddy IDE, either build, reads the CLI's ~/.codebuddy/models.json
// and needs no CLI, so with only the IDE installed CodeBuddy is listed,
// with the IDE's version and no "CLI not found", and its CLI isn't offered.
func TestCodeBuddyIDEIsCodeBuddy(t *testing.T) {
	judgeFor(t, "darwin")
	home := codebuddyHome(t)
	t.Setenv("PATH", t.TempDir()) // no codebuddy CLI
	apps, userApps := t.TempDir(), t.TempDir()
	was := appFolders
	appFolders = func() []string { return []string{apps, userApps} }
	t.Cleanup(func() { appFolders = was })
	a := codebuddy(home)
	// a name no machine has: proc.FindTool also looks where users' tools go
	a.Bin = "uni-switch-test-gone-codebuddy"
	if a.Name != "CodeBuddy" {
		t.Fatalf("the row is %q, not the CLI's and the IDE's both", a.Name)
	}
	offered := func() bool {
		for _, x := range installsOf([]*Agent{a}, "darwin", true) {
			if x.ID == "codebuddy" {
				return true
			}
		}
		return false
	}

	if a.Detected() {
		t.Fatal("detected with neither the CLI nor the IDE")
	}

	for _, c := range []struct{ dir, name, plist string }{
		{apps, "CodeBuddy CN.app", codebuddyIDEPlist},
		{userApps, "CodeBuddy.app", codebuddyIntlPlist},
	} {
		installApp(t, c.dir, c.name, c.plist)
		if !a.Detected() || a.CLIMissing() || offered() {
			t.Fatalf("%s alone: detected %v, missing %v, offered %v", c.name, a.Detected(), a.CLIMissing(), offered())
		}
		if v, ok := a.CLI(); !ok || v.App != "4.12.2" || v.Version != "" {
			t.Fatalf("%s's version: %+v %v", c.name, v, ok)
		}
		os.RemoveAll(filepath.Join(c.dir, c.name))
	}

	// another app under its name is not CodeBuddy's
	installApp(t, apps, "CodeBuddy.app", strings.Replace(codebuddyIntlPlist, "com.tencent.codebuddy", "com.example.other", 1))
	if a.Detected() || a.HasApp() {
		t.Fatal("another bundle id taken for the CodeBuddy IDE")
	}

	// CodeBuddy Code in a WSL distro is not the IDE's
	if (&Agent{ID: "codebuddy@wsl:Ubuntu", WSL: "Ubuntu", Bin: "codebuddy"}).HasApp() {
		t.Fatal("WSL twin has the IDE")
	}
}

// With only the IDE, connecting writes uni-switch's models where the IDE reads
// them, in the form its loader takes: genie's CustomModelsProductProvider
// (4.12.2's extensions/genie/out/extension/index.js) reads
// <home>/.codebuddy/models.json's "models", keeps an entry only with a
// non-blank string id and url, and takes "availableModels", when it has
// any, as a whitelist of the models it shows (AvailableModelsFilterProvider;
// none is no filter).
func TestCodeBuddyIDEReadsUniSwitchModels(t *testing.T) {
	home := codebuddyHome(t)
	t.Setenv("PATH", t.TempDir())
	apps := t.TempDir()
	was := appFolders
	appFolders = func() []string { return []string{apps} }
	t.Cleanup(func() { appFolders = was })
	installApp(t, apps, "CodeBuddy CN.app", codebuddyIDEPlist)
	a := codebuddy(home)
	a.Bin = "uni-switch-test-gone-codebuddy"
	if err := a.Field("model").Set("uni-switch/deepseek/pro"); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(home, ".codebuddy", "models.json"))
	if err != nil {
		t.Fatal(err)
	}
	// as the IDE's loader reads it
	var file struct {
		Models []struct {
			ID  any `json:"id"`
			URL any `json:"url"`
		} `json:"models"`
		AvailableModels []string `json:"availableModels"`
	}
	if err := json.Unmarshal(b, &file); err != nil {
		t.Fatalf("%v\n%s", err, b)
	}
	var kept []string
	for _, m := range file.Models {
		id, _ := m.ID.(string)
		url, _ := m.URL.(string)
		if strings.TrimSpace(id) != "" && strings.TrimSpace(url) != "" {
			kept = append(kept, id)
		}
	}
	shown := len(file.AvailableModels) == 0 || slices.Contains(file.AvailableModels, "deepseek/pro")
	if len(kept) != 1 || kept[0] != "deepseek/pro" || !shown {
		t.Fatalf("the IDE would load %v, show %v:\n%s", kept, file.AvailableModels, b)
	}
}

package agent

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/catalog"
	"github.com/dieqiyun/uni-switch/backend/internal/gateway"
	"github.com/dieqiyun/uni-switch/backend/internal/provider"
	"github.com/dieqiyun/uni-switch/backend/internal/usage"
)

// Cursor Private Inference (#299) is found by its product.json among the
// apps, beside a regular Cursor of the same app name that isn't it, and its
// row gives the command that starts it on uni-switch, with its own key; its
// requests are its own in the usage, not Cursor's.
func TestCursorLocal(t *testing.T) {
	root := t.TempDir()
	app := func(dir, nameShort, exe string) {
		t.Helper()
		res := filepath.Join(root, dir, "Contents", "Resources", "app")
		if err := os.MkdirAll(res, 0o755); err != nil {
			t.Fatal(err)
		}
		os.WriteFile(filepath.Join(res, "product.json"), []byte(`{"nameShort":"`+nameShort+`","applicationName":"cursor","dataFolderName":".cursor"}`), 0o644)
		if exe != "" {
			os.WriteFile(filepath.Join(root, dir, "Contents", "Info.plist"), []byte("<plist><dict>\n\t<key>CFBundleExecutable</key>\n\t<string>"+exe+"</string>\n</dict></plist>"), 0o644)
		}
	}
	app("Cursor.app", "Cursor", "Cursor")
	if got := findCursorLocal([]string{root, filepath.Join(root, "none")}); got != "" {
		t.Fatalf("a regular Cursor found as it: %q", got)
	}
	app("Cursor Private Inference.app", "Cursor Private Inference", "Cursor PI")
	want := filepath.Join(root, "Cursor Private Inference.app", "Contents", "MacOS", "Cursor PI")
	if got := findCursorLocal([]string{root}); got != want {
		t.Fatalf("found %q, want %q", got, want)
	}
	os.Remove(filepath.Join(root, "Cursor Private Inference.app", "Contents", "Info.plist"))
	if got := findCursorLocal([]string{root}); filepath.Base(got) != "Cursor" || !strings.Contains(got, "Private Inference.app") {
		t.Fatalf("without an Info.plist: %q", got)
	}

	// the command: the gateway's /v1 and the agent's key, the app quoted
	cmd := CursorLocalLaunch("/Applications/It's.app/Contents/MacOS/Cursor", "http://127.0.0.1:3425")
	for _, s := range []string{"CURSOR_LOCAL_AGENT_BASE_URL", "http://127.0.0.1:3425/v1", "CURSOR_LOCAL_AGENT_API_KEY", gateway.TokenFor("cursor-local")} {
		if !strings.Contains(cmd, s) {
			t.Errorf("%q lacks %q", cmd, s)
		}
	}
	if runtime.GOOS != "windows" && !strings.HasSuffix(cmd, ` '/Applications/It'\''s.app/Contents/MacOS/Cursor'`) {
		t.Errorf("app not quoted: %q", cmd)
	}

	// the agent: there and launchable when the app is, else not there
	old := cursorLocalApp
	t.Cleanup(func() { cursorLocalApp = old })
	a, err := Find(CursorLocalID)
	if err != nil {
		t.Fatal(err)
	}
	cursorLocalApp = func() string { return want }
	if !a.detect() || !strings.Contains(a.Launch(), want) {
		t.Errorf("installed: detect %v, launch %q", a.detect(), a.Launch())
	}
	cursorLocalApp = func() string { return "" }
	if a.detect() || a.Launch() != "" {
		t.Error("not installed, yet there")
	}

	if got := usage.AgentOf("cursor-local"); got != CursorLocalID {
		t.Errorf("AgentOf(cursor-local) = %q", got)
	}
	if got := usage.AgentOf("Cursor/3.2"); got != "cursor" {
		t.Errorf("AgentOf(Cursor/3.2) = %q", got)
	}
}

// Its row connects as other agents' do, with no command to run (mamba on
// Discord): uni-switch sets CURSOR_LOCAL_AGENT_BASE_URL and _API_KEY for the
// user, which only this build reads, notes it, sets them again when the
// gateway is served (the Mac's launchd forgets them at a restart, and the
// port may change), and clears them on disconnect.
func TestCursorLocalConnects(t *testing.T) {
	h := t.TempDir()
	t.Setenv("HOME", h)
	t.Setenv("USERPROFILE", h)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(h, ".config"))
	var set []map[string]string
	oldEnv, oldApp := cursorLocalUserEnv, cursorLocalApp
	t.Cleanup(func() { cursorLocalUserEnv, cursorLocalApp = oldEnv, oldApp })
	cursorLocalUserEnv = func(env map[string]string) error { set = append(set, env); return nil }
	cursorLocalApp = func() string { return "/Applications/Cursor Private Inference.app/Contents/MacOS/Cursor" }
	a, err := Find(CursorLocalID)
	if err != nil {
		t.Fatal(err)
	}
	if len(a.Fields) == 0 || a.Wired() {
		t.Fatalf("fields %d, wired %v before connecting", len(a.Fields), a.Wired())
	}
	how, err := a.ConnectHow()
	if err != nil || how.How != "uni-switch" || !a.Wired() {
		t.Fatalf("connect: %+v %v, wired %v", how, err, a.Wired())
	}
	want := map[string]string{"CURSOR_LOCAL_AGENT_BASE_URL": gateway.URL() + "/v1", "CURSOR_LOCAL_AGENT_API_KEY": gateway.TokenFor(CursorLocalID)}
	if len(set) != 1 || set[0]["CURSOR_LOCAL_AGENT_BASE_URL"] != want["CURSOR_LOCAL_AGENT_BASE_URL"] || set[0]["CURSOR_LOCAL_AGENT_API_KEY"] != want["CURSOR_LOCAL_AGENT_API_KEY"] {
		t.Fatalf("set %v, want %v", set, want)
	}
	if a.Launch() == "" {
		t.Error("the command to start it from a shell is gone")
	}

	// served again: set again
	KeepCursorLocalEnv(context.Background())
	if len(set) != 2 || set[1]["CURSOR_LOCAL_AGENT_API_KEY"] != want["CURSOR_LOCAL_AGENT_API_KEY"] {
		t.Fatalf("not set again when served: %v", set)
	}

	if err := a.Disconnect(); err != nil || a.Wired() {
		t.Fatalf("disconnect: %v, wired %v", err, a.Wired())
	}
	if len(set) != 3 || set[2] != nil {
		t.Fatalf("not cleared: %v", set)
	}
	// off, a serve sets nothing
	KeepCursorLocalEnv(context.Background())
	if len(set) != 3 {
		t.Fatalf("set while off: %v", set)
	}
}

// Its effort (#1003), which neither its environment nor, for most models,
// its app can say, is picked in its row among the levels the models it
// lists have, the default first: kept in uni-switch for the gateway to ask, and
// offered only while it is connected and something it lists reasons.
func TestCursorLocalEffort(t *testing.T) {
	syncHome(t)
	oldEnv, oldApp := cursorLocalUserEnv, cursorLocalApp
	t.Cleanup(func() { cursorLocalUserEnv, cursorLocalApp = oldEnv, oldApp })
	cursorLocalUserEnv = func(map[string]string) error { return nil }
	cursorLocalApp = func() string { return "/Applications/Cursor Private Inference.app/Contents/MacOS/Cursor" }
	if err := provider.Save(provider.Provider{ID: "plain", Name: "Plain", Chat: "https://plain.test/v1", Key: "key", Models: []string{"gpt-4o-mini"}}); err != nil {
		t.Fatal(err)
	}
	a, err := Find(CursorLocalID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.ConnectHow(); err != nil || !a.Wired() {
		t.Fatalf("connect: %v, wired %v", err, a.Wired())
	}
	field := func() Field {
		t.Helper()
		for _, f := range a.Fields {
			if f.Key == "effort" {
				return f
			}
		}
		t.Fatal("no effort field")
		return Field{}
	}
	values := func() []string {
		var out []string
		for _, o := range field().Options(a.Values()) {
			out = append(out, o.Value)
		}
		return out
	}
	if got := values(); got != nil {
		t.Fatalf("offered %q with no model that reasons", got)
	}
	if err := provider.Save(provider.Provider{ID: "think", Name: "Think", Chat: "https://think.test/v1", Key: "key", Models: []string{"gpt-5.5", "switch-only"}}); err != nil {
		t.Fatal(err)
	}
	if err := catalog.SaveLive("think", "https://think.test/v1", []catalog.Model{
		{ID: "gpt-5.5", Efforts: []string{"xhigh", "minimal", "low", "medium", "high", "ultra"}},
		{ID: "switch-only", Reasoning: true},
	}); err != nil {
		t.Fatal(err)
	}
	if got := values(); !slices.Equal(got, []string{"", "minimal", "low", "medium", "high", "xhigh"}) {
		t.Fatalf("offered %q", got)
	}
	// a model that reasons with no levels known: the three every vendor takes
	if err := provider.SetHiddenModels(CursorLocalID, []string{"think/gpt-5.5"}); err != nil {
		t.Fatal(err)
	}
	if got := values(); !slices.Equal(got, []string{"", "low", "medium", "high"}) {
		t.Fatalf("with gpt-5.5 hidden, offered %q", got)
	}

	if err := a.Apply("effort", "high"); err != nil {
		t.Fatal(err)
	}
	if got, kept := a.Values()["effort"], provider.AgentEffort(CursorLocalID); got != "high" || kept != "high" {
		t.Fatalf("picked high: shown %q, kept %q", got, kept)
	}
	if err := field().Set("hard"); err == nil {
		t.Fatal("a word that isn't a level was kept")
	}
	if err := a.Apply("effort", ""); err != nil || provider.AgentEffort(CursorLocalID) != "" {
		t.Fatalf("back to the default: %v, kept %q", err, provider.AgentEffort(CursorLocalID))
	}

	// not connected: nothing to pick
	if err := a.Disconnect(); err != nil {
		t.Fatal(err)
	}
	if got := values(); got != nil {
		t.Fatalf("offered %q while not connected", got)
	}
}

// TestCursorLocalFoundWhereInstalled: the build installed outside the apps
// folders (a folder picked in its Windows installer, which Windows' list
// of installed programs names) and its Linux AppImage, as downloaded, are
// found; regular Cursor's AppImage isn't (#1254).
func TestCursorLocalFoundWhereInstalled(t *testing.T) {
	root, elsewhere := t.TempDir(), filepath.Join(t.TempDir(), "D", "Cursor PI")
	// the Windows install's layout, as its 3.24.9 user setup leaves it
	res := filepath.Join(elsewhere, "resources", "app")
	if err := os.MkdirAll(res, 0o755); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(res, "product.json"), []byte(`{"nameShort":"Cursor Private Inference","nameLong":"Cursor Private Inference","applicationName":"cursor","dataFolderName":".cursor","win32DirName":"cursor"}`), 0o644)
	os.WriteFile(filepath.Join(elsewhere, "Cursor Private Inference.exe"), nil, 0o755)
	if got := findCursorLocal([]string{root}); got != "" {
		t.Fatalf("found %q with nothing in the apps folder", got)
	}
	if got := findCursorLocal([]string{root}, filepath.Join(root, "gone"), elsewhere); filepath.Dir(got) != elsewhere {
		t.Fatalf("installed in a folder of the user's: found %q, want a program in %q", got, elsewhere)
	}

	os.WriteFile(filepath.Join(root, "Cursor-3.24.9-x86_64.AppImage"), nil, 0o755)
	if got := findCursorLocal([]string{root}); got != "" {
		t.Fatalf("regular Cursor's AppImage found as it: %q", got)
	}
	img := filepath.Join(root, "Cursor_Private_Inference-3.24.9-x86_64.AppImage")
	os.WriteFile(img, nil, 0o755)
	if got := findCursorLocal([]string{root}); got != img {
		t.Fatalf("its AppImage: found %q, want %q", got, img)
	}
}

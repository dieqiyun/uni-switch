package agent

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/desktopdir"
	"github.com/dieqiyun/uni-switch/backend/internal/edit"
	"github.com/dieqiyun/uni-switch/backend/internal/gateway"
	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// Applying another tool's config is the user's latest routing decision. A
// running uni-switch and a newly started one must both leave that decision alone.
func TestCodexExternalSwitchSurvivesSync(t *testing.T) {
	for _, api := range []bool{false, true} {
		t.Run(map[bool]string{false: "ChatGPT", true: "API"}[api], func(t *testing.T) {
			home, read := codexHome(t, `{"tokens":{"access_token":"x","id_token":"x.e30.x"}}`, ccSwitchCodex)
			a := codex(home)
			if err := a.Fields[0].Set("fake/m1"); err != nil {
				t.Fatal(err)
			}
			if api {
				stash(map[string]string{here(home).key("codex.login"): "api"})
			}
			path := filepath.Join(home, ".codex", "config.toml")
			if err := os.WriteFile(path, []byte(ccSwitchCodex), 0o600); err != nil {
				t.Fatal(err)
			}
			for round := range 3 {
				if err := a.Sync(); err != nil {
					t.Fatal(err)
				}
				if got := read(); got != ccSwitchCodex {
					t.Fatalf("sync %d overwrote the externally applied provider:\n%s", round, got)
				}
				a = codex(home)
			}
			if err := a.Disconnect(); err != nil {
				t.Fatal(err)
			}
			if got := read(); got != ccSwitchCodex {
				t.Fatalf("disconnect overwrote the external provider:\n%s", got)
			}
		})
	}
}

func TestCodexFailoverKeepsExternalBase(t *testing.T) {
	for _, base := range []string{"https://other.example/v1", ""} {
		t.Run(map[bool]string{false: "external relay", true: "direct OpenAI"}[base == ""], func(t *testing.T) {
			claims := func(m map[string]any) string {
				b, _ := json.Marshal(m)
				return "h." + base64.RawURLEncoding.EncodeToString(b) + ".s"
			}
			auth := func(id string) map[string]any {
				return map[string]any{"auth_mode": "chatgpt", "tokens": map[string]any{
					"id_token":      claims(map[string]any{"email": id + "@example.com"}),
					"access_token":  claims(map[string]any{"exp": time.Now().Add(time.Hour).Unix()}),
					"refresh_token": "synthetic-refresh", "account_id": id,
				}}
			}
			me, _ := json.Marshal(auth("first"))
			home, read := codexHome(t, string(me), "model = \"gpt-5.5\"\n")
			logins, _ := json.Marshal([]map[string]any{{"agent": "codex", "user": "second@example.com", "on": true, "seen": time.Now(), "auth": auth("second")}})
			if err := os.WriteFile(filepath.Join(filepath.Dir(provider.Path()), "logins.json"), logins, 0o600); err != nil {
				t.Fatal(err)
			}
			a := codex(home)
			if err := a.Sync(); err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(read(), "openai_base_url") {
				t.Fatal("fixture did not enable account failover")
			}
			path := filepath.Join(home, ".codex", "config.toml")
			var err error
			if base == "" {
				err = edit.DelTOMLTop(path, "openai_base_url")
			} else {
				err = edit.SetTOMLTop(path, edit.KV{Path: "openai_base_url", Value: base})
			}
			if err != nil {
				t.Fatal(err)
			}
			before := read()
			for range 3 {
				if err := a.Sync(); err != nil {
					t.Fatal(err)
				}
				if read() != before {
					t.Fatalf("account failover overwrote the external relay:\n%s", read())
				}
			}
		})
	}
}

// Even when another tool keeps the old model and top-level base URL, its
// selected provider table must never be seized again by background sync.
func TestCodexExternalTableIsNotRetaken(t *testing.T) {
	home, read := codexHome(t, `{"tokens":{"access_token":"x","id_token":"x.e30.x"}}`, ccSwitchCodex)
	a := codex(home)
	if err := a.Fields[0].Set("fake/m1"); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(home, ".codex", "config.toml")
	if err := edit.SetTOMLKey(path, "model_providers.custom", "base_url", "https://other.example/v1"); err != nil {
		t.Fatal(err)
	}
	if err := a.Sync(); err != nil {
		t.Fatal(err)
	}
	table, err := edit.GetTOMLTable(path, "model_providers.custom")
	if err != nil || table["base_url"] != "https://other.example/v1" {
		t.Fatalf("background sync retook the external table: %v\n%s", err, read())
	}
}

func TestClaudeDesktopExternalSwitchNeedsRestart(t *testing.T) {
	home := restartSandbox(t)
	t.Setenv("APPDATA", filepath.Join(home, "AppData", "Roaming"))
	t.Setenv("LOCALAPPDATA", filepath.Join(home, "AppData", "Local"))
	ps := desktopSets(desktopdir.OS, home, os.Getenv)
	wasSets := desktopSetsHere
	desktopSetsHere = func() []desktopPaths { return ps }
	t.Cleanup(func() { desktopSetsHere = wasSets })
	restartOnWindows = true
	start := time.Now().Add(-time.Hour)
	winProcs = func(name string) []winProc {
		if !strings.EqualFold(name, "Claude.exe") {
			return nil
		}
		return []winProc{{pid: 42, parent: 1, image: `C:\Program Files\WindowsApps\Claude_4.29.0.0_x64__pzs8sxrjxfjjc\app\Claude.exe`, start: start}}
	}
	a := claudeDesktop(home)
	if err := a.Field("provider").Set(uniSwitchID); err != nil {
		t.Fatal(err)
	}
	for _, p := range ps {
		desktopWrite(t, p.meta, `{"appliedId":"cc-switch","entries":[{"id":"`+desktopProfileID+`","name":"uni-switch"},{"id":"cc-switch","name":"CC Switch"}]}`)
	}
	if a.Wired() {
		t.Fatal("the external selection still reads as connected")
	}
	if a.AppBehind() == nil {
		t.Fatal("no restart notice while Desktop still runs with the old route")
	}
	start = time.Now().Add(time.Minute)
	if a.AppBehind() != nil {
		t.Fatal("restart notice persisted after Desktop read the new configuration")
	}
	start = time.Now().Add(-time.Minute)
	for _, p := range ps {
		desktopWrite(t, filepath.Join(p.library, "cc-switch.json"), `{"inferenceProvider":"gateway","inferenceGatewayBaseUrl":"https://next.example/v1"}`)
	}
	if a.AppBehind() == nil {
		t.Fatal("no restart notice when another tool reused its applied profile id")
	}
}

func TestClaudeDesktopExternalSwitch(t *testing.T) {
	for _, action := range []string{"state", "sync", "disconnect", "reconnect"} {
		t.Run(action, func(t *testing.T) {
			home, p := desktopSandbox(t)
			// Before connecting, Desktop used an Anthropic sign-in. CC Switch
			// later selects its own gateway, which still needs deploymentMode 3p.
			desktopWrite(t, p.config, `{"deploymentMode":"1p"}`)
			desktopWrite(t, p.config3p, `{"deploymentMode":"1p"}`)
			a := claudeDesktop(home)
			if err := a.Field("provider").Set(uniSwitchID); err != nil {
				t.Fatal(err)
			}
			const other = "00000000-0000-4000-8000-000000157210"
			desktopWrite(t, filepath.Join(p.library, other+".json"), `{"inferenceProvider":"gateway","inferenceGatewayBaseUrl":"http://127.0.0.1:15721/claude-desktop","inferenceGatewayApiKey":"synthetic-cc-switch"}`)
			desktopWrite(t, p.meta, `{"appliedId":"`+other+`","entries":[{"id":"`+desktopProfileID+`","name":"uni-switch"},{"id":"`+other+`","name":"CC Switch"}]}`)
			// Make an old managed profile need seeding: inactive profiles
			// must be left alone even if they are from an earlier version.
			if err := edit.DelJSON(p.prof, "claudeAiImport", "toolSearchEnabled"); err != nil {
				t.Fatal(err)
			}
			before := desktopTree(t, p)
			switch action {
			case "state":
				// A saved tier preference must survive for reconnect without
				// pretending the external gateway is still connected.
				if err := gateway.SetDesktopTier("sonnet", "fake/m1"); err != nil {
					t.Fatal(err)
				}
				if got := a.Field("provider").Get(); got != "" {
					t.Fatalf("CC Switch is applied but provider still reads %q", got)
				}
				if desktopWiredAny([]desktopPaths{p}) {
					t.Fatal("Claude Code still receives uni-switch's Desktop aliases")
				}
				if a.Wired() {
					t.Fatal("a saved tier keeps Desktop connected after an external switch")
				}
			case "sync":
				for range 3 {
					if err := a.Sync(); err != nil {
						t.Fatal(err)
					}
					a = claudeDesktop(home)
				}
				if after := desktopTree(t, p); !reflect.DeepEqual(before, after) {
					t.Fatal("background sync wrote Desktop's inactive configuration")
				}
			case "disconnect":
				if err := a.Field("provider").Set(""); err != nil {
					t.Fatal(err)
				}
				for _, f := range []string{p.config, p.config3p} {
					if mode, _ := edit.GetJSON(f, "deploymentMode"); mode != "3p" {
						t.Errorf("disconnect changed the other gateway's deploymentMode to %q", mode)
					}
				}
				if id, _ := edit.GetJSON(p.meta, "appliedId"); id != other {
					t.Errorf("disconnect changed the other gateway's selection to %q", id)
				}
				if got, _ := os.ReadFile(filepath.Join(p.library, other+".json")); string(got) != before[filepath.Join(p.library, other+".json")] {
					t.Fatal("disconnect changed the other gateway's profile")
				}
			case "reconnect":
				if err := a.Reapply(); err != nil {
					t.Fatal(err)
				}
				if id, _ := edit.GetJSON(p.meta, "appliedId"); id != desktopProfileID || !a.Wired() {
					t.Fatalf("explicit reconnect did not restore uni-switch: appliedId %q", id)
				}
			}
		})
	}
}

package gui

import (
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/agent"
	"github.com/dieqiyun/uni-switch/backend/internal/edit"
	"github.com/dieqiyun/uni-switch/backend/internal/provider"
	appsettings "github.com/dieqiyun/uni-switch/backend/internal/settings"
)

func TestRuntimeUnavailableJSONKeepsMessageAndAction(t *testing.T) {
	for _, action := range []agent.OfflineAction{agent.OfflineStage, agent.OfflineDisconnect, ""} {
		original := &agent.RuntimeUnavailableError{Agent: "aside", Operation: "apply", Offline: action, Cause: fmt.Errorf("fixture unavailable")}
		err := fmt.Errorf("selection: %w", original)
		rec := httptest.NewRecorder()
		fail(rec, err)
		if rec.Code != 400 {
			t.Fatalf("status %d", rec.Code)
		}
		var out map[string]string
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
			t.Fatal(err)
		}
		if out["error"] != err.Error() || out["code"] != "runtime_unavailable" || out["offline"] != string(action) {
			t.Fatalf("shape %+v", out)
		}
	}
	rec := httptest.NewRecorder()
	fail(rec, fmt.Errorf("refusal"))
	var out map[string]string
	json.Unmarshal(rec.Body.Bytes(), &out)
	if _, ok := out["code"]; ok {
		t.Fatal("generic refusal advertised fallback")
	}
}

func TestAsideOfflineDisconnectAPIRequiresCurrentPreview(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
	dir := filepath.Join(home, ".aside", "u", "0")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	settings := filepath.Join(dir, "settings.json")
	initial := `{"defaultModel":{"provider":"native","modelId":"original","thinkingLevel":"high","extra":true},"theme":"dark"}`
	if err := os.WriteFile(settings, []byte(initial), 0600); err != nil {
		t.Fatal(err)
	}
	if err := provider.Save(provider.Provider{ID: "offline-fixture", Key: "fixture", Chat: "http://127.0.0.1:1/v1", Models: []string{"model"}}); err != nil {
		t.Fatal(err)
	}
	h := Handler(nil, nil)
	call := func(method, path, body string) *httptest.ResponseRecorder {
		t.Helper()
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(method, path, strings.NewReader(body)))
		return rec
	}
	unavailablePick := call("POST", "/api/set", `{"agent":"aside","field":"model","value":"offline-fixture/model"}`)
	var pickError map[string]string
	json.Unmarshal(unavailablePick.Body.Bytes(), &pickError)
	if unavailablePick.Code != 400 || pickError["code"] != "runtime_unavailable" || pickError["offline"] != "stage" {
		t.Fatalf("set shape %s", unavailablePick.Body)
	}
	if before, _ := os.ReadFile(settings); string(before) != initial {
		t.Fatal("unavailable set staged without confirmation")
	}
	invalidPick := call("POST", "/api/set", `{"agent":"aside","field":"model","value":"uni-switch/offline-fixture/missing"}`)
	var invalidError map[string]string
	json.Unmarshal(invalidPick.Body.Bytes(), &invalidError)
	if invalidPick.Code != 400 || invalidError["code"] != "" {
		t.Fatalf("invalid set shape %s", invalidPick.Body)
	}
	stage := call("POST", "/api/agents/stage/aside", `{"field":"model","value":"offline-fixture/model"}`)
	if stage.Code != 200 {
		t.Fatalf("stage %s", stage.Body)
	}
	if selection, _ := edit.GetJSON(settings, "defaultModel.provider"); selection != "uni-switch" {
		t.Fatal("stage did not normalize the same catalog ref as set")
	}
	a, err := agent.Find("aside")
	if err != nil {
		t.Fatal(err)
	}
	if err := a.Connect(); err != nil {
		t.Fatal(err)
	}
	record := filepath.Join(appsettings.Dir(), "aside-state.json")
	preview := func() string {
		t.Helper()
		rec := call("GET", "/api/agents/preview/aside", "")
		var out struct {
			Revision string
			Error    string
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
			t.Fatal(err)
		}
		if out.Error != "" || out.Revision == "" {
			t.Fatalf("preview %s", rec.Body)
		}
		return out.Revision
	}
	revision := preview()
	live := call("POST", "/api/agents/disconnect/aside", "{}")
	var unavailable map[string]string
	json.Unmarshal(live.Body.Bytes(), &unavailable)
	if live.Code != 400 || unavailable["code"] != "runtime_unavailable" || unavailable["offline"] != "disconnect" {
		t.Fatalf("live %d %s", live.Code, live.Body)
	}
	before, _ := os.ReadFile(settings)
	if err := edit.SetJSON(settings, edit.KV{Path: "theme", Value: "light"}); err != nil {
		t.Fatal(err)
	}
	stale := call("POST", "/api/agents/disconnect-offline/aside", `{"revision":"`+revision+`"}`)
	if stale.Code != 400 {
		t.Fatalf("stale accepted %s", stale.Body)
	}
	if !a.Wired() {
		t.Fatal("stale preview removed provider")
	}
	if err := edit.WriteAtomic(settings, before); err != nil {
		t.Fatal(err)
	}
	revision = preview()
	recordBefore, err := os.ReadFile(record)
	if err != nil {
		t.Fatal(err)
	}
	if err := edit.SetJSON(record, edit.KV{Path: "fields.model.before.modelId", Value: "changed"}); err != nil {
		t.Fatal(err)
	}
	if rec := call("POST", "/api/agents/disconnect-offline/aside", `{"revision":"`+revision+`"}`); rec.Code != 400 {
		t.Fatal("changed restore point accepted")
	}
	if err := edit.WriteAtomic(record, recordBefore); err != nil {
		t.Fatal(err)
	}
	revision = preview()
	for _, body := range []string{`{}`, `{"revision":"wrong"}`, `{"revision":"` + revision + `","path":"/tmp/client"}`, `{"revision":"` + revision + `","files":[]}`} {
		rec := call("POST", "/api/agents/disconnect-offline/aside", body)
		if rec.Code != 400 {
			t.Fatalf("invalid request accepted %s", body)
		}
	}
	unsupported := call("POST", "/api/agents/disconnect-offline/codex", `{"revision":"x"}`)
	if unsupported.Code != 400 || !strings.Contains(unsupported.Body.String(), "does not support") {
		t.Fatalf("unsupported %s", unsupported.Body)
	}
	offline := call("POST", "/api/agents/disconnect-offline/aside", `{"revision":"`+revision+`"}`)
	if offline.Code != 200 {
		t.Fatalf("offline %d %s", offline.Code, offline.Body)
	}
	if got, _ := edit.GetJSON(settings, "defaultModel.modelId"); got != "original" {
		t.Fatalf("restored %s", got)
	}
	if a.Wired() {
		t.Fatal("offline retained provider")
	}
}

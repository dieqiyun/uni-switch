package agent

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/edit"
)

func TestAsideLegacyRestorePointIsReadWithoutMigrationWrites(t *testing.T) {
	settings, models := asideHome(t)
	if err := edit.SetJSON(settings, edit.KV{Path: "defaultModel.provider", Value: "uni-switch"}, edit.KV{Path: "defaultModel.modelId", Value: "relay/glm-4.6"}); err != nil {
		t.Fatal(err)
	}
	stash(map[string]string{"aside.was": "native/native-model"})
	a := mustFindAside(t)
	if err := a.Connect(); err != nil {
		t.Fatal(err)
	}
	before := readFile(settings) + readFile(models) + readFile(stashPath())
	if _, err := DisconnectPreview(a, "no executable needed"); err != nil {
		t.Fatal(err)
	}
	if readFile(settings)+readFile(models)+readFile(stashPath()) != before {
		t.Fatal("migration preview wrote config")
	}
	if err := a.Disconnect(); err != nil {
		t.Fatal(err)
	}
	if a.Field("model").Get() != "native/native-model" {
		t.Fatal("trusted legacy restore point not used")
	}
}

func TestAsideRuntimeWinsOverSavedModelAndPlanRefusesMismatch(t *testing.T) {
	settings, _ := asideHome(t)
	a := mustFindAside(t)
	if err := a.Apply("model", "uni-switch/relay/glm-4.6"); err != nil {
		t.Fatal(err)
	}
	plan, err := a.Native.Disconnect()
	if err != nil {
		t.Fatal(err)
	}
	asideRead = func(string) (map[string]json.RawMessage, error) {
		var s map[string]json.RawMessage
		_ = json.Unmarshal([]byte(readFile(settings)), &s)
		s["defaultModel"] = json.RawMessage(`{"provider":"native","modelId":"new-choice","thinkingLevel":"high","fastMode":true}`)
		return s, nil
	}
	if err := a.Native.Execute(plan); err == nil || !strings.Contains(err.Error(), "changed") {
		t.Fatal("disconnect ignored runtime mismatch")
	}
}

func TestAsideReadbackConfirmsRestoreSettings(t *testing.T) {
	settings, _ := asideHome(t)
	a := mustFindAside(t)
	if err := a.Apply("model", "uni-switch/relay/glm-4.6"); err != nil {
		t.Fatal(err)
	}
	inner := asideSet
	asideSet = func(account, expr string) error {
		if err := inner(account, expr); err != nil {
			return err
		}
		return edit.SetJSON(settings, edit.KV{Path: "defaultModel.thinkingLevel", Value: "low"})
	}
	if err := a.Disconnect(); err == nil {
		t.Fatal("incorrect restore settings accepted")
	}
}

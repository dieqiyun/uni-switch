package agent

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/edit"
	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

func TestAsideImageCatalogSeparatesGenerationFromVision(t *testing.T) {
	asideHome(t)
	if err := provider.Save(provider.Provider{ID: "art", Name: "Art", Key: "fixture", Chat: "https://art.invalid/v1", Models: []string{"vision-chat", "gpt-image-1"}}); err != nil {
		t.Fatal(err)
	}
	opts := asideImageOptions()
	found := false
	for _, o := range opts {
		if o.Value == "uni-switch/art/vision-chat" {
			t.Fatal("vision model offered as image generator")
		}
		if o.Value == "uni-switch/art/gpt-image-1" {
			found = true
		}
	}
	if !found {
		t.Fatal("image generation endpoint omitted")
	}
	a := mustFindAside(t)
	if err := a.Apply("image", "uni-switch/art/gpt-image-1"); err != nil {
		t.Fatal(err)
	}
	if a.Field("image").Get() != "uni-switch/art/gpt-image-1" {
		t.Fatal("image setting not preserved")
	}
	if err := a.Disconnect(); err != nil {
		t.Fatal(err)
	}
	if a.Field("image").Get() != "" {
		t.Fatal("unset image restore point not restored")
	}
}

func TestAsideRuntimeRefusalDoesNotCommitOwnership(t *testing.T) {
	settings, _ := asideHome(t)
	a := mustFindAside(t)
	before := readFile(settings)
	asideSet = func(string, string) error { return nil }
	if err := a.Apply("fast", "uni-switch/relay/glm-4.6"); err == nil {
		t.Fatal("silently dropped selection was accepted")
	}
	if readFile(settings) != before {
		t.Fatal("refused role changed saved settings")
	}
	c := newAsideConnection(here(""))
	r, err := c.load()
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Fields["fast"].Last) != 0 {
		t.Fatal("unconfirmed role recorded as applied")
	}
}

func TestAsideModelChangesKeepOriginalRestorePoint(t *testing.T) {
	_, _ = asideHome(t)
	if err := provider.Save(provider.Provider{ID: "relay", Name: "Relay", Key: "k", Chat: "http://127.0.0.1:1/v1", Models: []string{"one", "two"}}); err != nil {
		t.Fatal(err)
	}
	a := mustFindAside(t)
	for _, v := range []string{"uni-switch/relay/one", "uni-switch/relay/two"} {
		if err := a.Apply("model", v); err != nil {
			t.Fatal(err)
		}
	}
	if err := a.Disconnect(); err != nil {
		t.Fatal(err)
	}
	if got := a.Field("model").Get(); got != "native/native-model" {
		t.Fatalf("original restore point lost: %s", got)
	}
}

func TestAsideRoleRestoreKeepsDefaultAndOtherRoles(t *testing.T) {
	settings, _ := asideHome(t)
	if err := edit.SetJSON(settings, edit.KV{Path: "modelCategories.fast", Value: json.RawMessage(`{"provider":"native","modelId":"small","thinkingLevel":"low","fastMode":false}`)}); err != nil {
		t.Fatal(err)
	}
	before := readFile(settings)
	a := mustFindAside(t)
	if err := a.Apply("fast", "uni-switch/relay/glm-4.6"); err != nil {
		t.Fatal(err)
	}
	if a.Field("model").Get() != "native/native-model" || a.Field("deep").Get() != "" {
		t.Fatal("role selection changed other fields")
	}
	if err := a.Disconnect(); err != nil {
		t.Fatal(err)
	}
	var got, want any
	_ = json.Unmarshal([]byte(readFile(settings)), &got)
	_ = json.Unmarshal([]byte(before), &want)
	if !equalJSONValues(got, want) {
		t.Fatalf("role restore differs: %s", readFile(settings))
	}
}

func equalJSONValues(a, b any) bool {
	aa, _ := json.Marshal(a)
	bb, _ := json.Marshal(b)
	return string(aa) == string(bb)
}

func TestAsidePlanRejectsChangedFiles(t *testing.T) {
	settings, models := asideHome(t)
	a := mustFindAside(t)
	if err := a.Apply("model", "uni-switch/relay/glm-4.6"); err != nil {
		t.Fatal(err)
	}
	plan, err := a.Native.Disconnect()
	if err != nil {
		t.Fatal(err)
	}
	if err := edit.SetJSON(settings, edit.KV{Path: "theme", Value: "light"}); err != nil {
		t.Fatal(err)
	}
	if err := a.Native.Execute(plan); err == nil {
		t.Fatal("stale plan was executed")
	}
	if _, ok := edit.GetJSON(models, "providers.uni-switch"); !ok {
		t.Fatal("stale plan removed provider")
	}
}

func TestAsideReconnectAndSyncNeverChangeSelections(t *testing.T) {
	settings, models := asideHome(t)
	a := mustFindAside(t)
	if err := a.Connect(); err != nil {
		t.Fatal(err)
	}
	before := readFile(settings)
	if err := edit.SetJSON(models, edit.KV{Path: "providers.uni-switch.baseUrl", Value: "http://127.0.0.1:9/v1"}); err != nil {
		t.Fatal(err)
	}
	bad := readFile(models)
	if err := a.Sync(); err != nil {
		t.Fatal(err)
	}
	if readFile(models) != bad {
		t.Fatal("sync retargeted another gateway")
	}
	if err := a.Reapply(); err != nil {
		t.Fatal(err)
	}
	if readFile(settings) != before || a.Drift() != nil {
		t.Fatal("provider reconnect changed selection or failed to repair")
	}
}

func TestAsideBrokenProviderFileIsNeverOverwritten(t *testing.T) {
	_, models := asideHome(t)
	writeFile(t, models, "{ broken")
	a := mustFindAside(t)
	if err := a.Connect(); err == nil {
		t.Fatal("broken provider file accepted")
	}
	if readFile(models) != "{ broken" {
		t.Fatal("broken file overwritten")
	}
}

func TestAsideSettingsIDsAreQuoted(t *testing.T) {
	asideHome(t)
	a := mustFindAside(t)
	value := `native/x\";throw new Error('injected');`
	if err := a.Apply("model", value); err != nil {
		t.Fatal(err)
	}
	if got := a.Field("model").Get(); got != value {
		t.Fatalf("ID changed to %s", got)
	}
}

func TestAsideRestoreReadbackFailureKeepsProvider(t *testing.T) {
	_, models := asideHome(t)
	a := mustFindAside(t)
	if err := a.Apply("model", "uni-switch/relay/glm-4.6"); err != nil {
		t.Fatal(err)
	}
	inner := asideRead
	reads := 0
	asideRead = func(account string) (map[string]json.RawMessage, error) {
		reads++
		if reads > 1 {
			return nil, errors.New("readback unavailable")
		}
		return inner(account)
	}
	if err := a.Disconnect(); err == nil {
		t.Fatal("unconfirmed restoration reported success")
	}
	if _, ok := edit.GetJSON(models, "providers.uni-switch"); !ok {
		t.Fatal("provider deleted after failed readback")
	}
}

func TestAsideImageFailureDoesNotMarkProviderDisconnected(t *testing.T) {
	settings, _ := asideHome(t)
	a := mustFindAside(t)
	if err := a.Connect(); err != nil {
		t.Fatal(err)
	}
	if err := edit.SetJSON(settings, edit.KV{Path: asideImageKey, Value: json.RawMessage(`{"provider":"uni-switch","modelId":"gone/image"}`)}); err != nil {
		t.Fatal(err)
	}
	a = mustFindAside(t)
	state := a.Native.Read()
	if !a.Wired() || a.Drift() != nil || state.Fields["image"].Status != "unavailable" {
		t.Fatalf("image problem corrupted provider status: %+v", state)
	}
	if !strings.Contains(state.Fields["image"].Detail, "image") {
		t.Fatal("field problem has no explanation")
	}
}

package agent

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/edit"
	"github.com/dieqiyun/uni-switch/backend/internal/gateway"
)

func asideHome(t *testing.T) (string, string) {
	t.Helper()
	syncHome(t)
	dir := asideDir(here(os.Getenv("HOME")))
	settings, models := filepath.Join(dir, "settings.json"), filepath.Join(dir, "models.json")
	writeFile(t, settings, `{"defaultModel":{"provider":"native","modelId":"native-model","thinkingLevel":"high","fastMode":true},"modelCategories":{},"imageGenerationModel":null,"theme":"dark"}`)
	writeFile(t, models, `{"providers":{"native":{"apiKey":"fixture","baseUrl":"https://native.invalid/v1","models":[{"id":"native-model"}]}},"keepMe":"yes"}`)
	oldRead, oldSet := asideRead, asideSet
	asideRead = func(string) (map[string]json.RawMessage, error) {
		var s map[string]json.RawMessage
		err := json.Unmarshal([]byte(readFile(settings)), &s)
		return s, err
	}
	asideSet = func(account, expr string) error {
		if account != "u0" {
			t.Fatalf("account %q", account)
		}
		return asideFakeSet(settings, expr)
	}
	t.Cleanup(func() { asideRead, asideSet = oldRead, oldSet })
	return settings, models
}

// asideFakeSet does to settings what Aside does for uni-switch's repl
// expression.
func asideFakeSet(settings, expr string) error {
	if strings.HasPrefix(expr, "aside.settings.set(") {
		after := strings.TrimPrefix(expr, "aside.settings.set(")
		key, rest, _ := strings.Cut(after, ",")
		var k string
		_ = json.Unmarshal([]byte(key), &k)
		var value json.RawMessage
		if err := json.NewDecoder(strings.NewReader(rest)).Decode(&value); err != nil {
			return err
		}
		return edit.SetJSON(settings, edit.KV{Path: k, Value: value})
	}
	if strings.HasPrefix(expr, "const c=") {
		after := strings.TrimPrefix(expr, "const c=aside.settings.get('modelCategories')||{};")
		if strings.HasPrefix(after, "delete c[") {
			k, _, _ := strings.Cut(strings.TrimPrefix(after, "delete c["), "];")
			var role string
			_ = json.Unmarshal([]byte(k), &role)
			return edit.DelJSON(settings, "modelCategories."+role)
		}
		k, rest, _ := strings.Cut(strings.TrimPrefix(after, "c["), "]=")
		var value json.RawMessage
		if err := json.NewDecoder(strings.NewReader(rest)).Decode(&value); err != nil {
			return err
		}
		var role string
		_ = json.Unmarshal([]byte(k), &role)
		return edit.SetJSON(settings, edit.KV{Path: "modelCategories." + role, Value: value})
	}
	return errors.New("unexpected settings expression")
}

func mustFindAside(t *testing.T) *Agent {
	t.Helper()
	a, err := Find("aside")
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func TestAsideConnectOnlyRegistersProvider(t *testing.T) {
	settings, models := asideHome(t)
	before := readFile(settings)
	a := mustFindAside(t)
	calls := 0
	old := asideSet
	asideSet = func(a, b string) error { calls++; return old(a, b) }
	if err := a.Connect(); err != nil {
		t.Fatal(err)
	}
	if !a.Wired() || readFile(settings) != before || calls != 0 {
		t.Fatalf("connect changed selection: wired=%v calls=%d settings=%s", a.Wired(), calls, readFile(settings))
	}
	if key, _ := edit.GetJSON(models, "providers.uni-switch.apiKey"); key != gateway.TokenFor("aside") {
		t.Fatal("wrong caller credential")
	}
	if err := a.Disconnect(); err != nil {
		t.Fatal(err)
	}
	if a.Wired() || readFile(settings) != before {
		t.Fatal("provider-only disconnect changed selection")
	}
}

func TestAsideSelectedFieldRestoresCompleteSelection(t *testing.T) {
	settings, _ := asideHome(t)
	before := readFile(settings)
	a := mustFindAside(t)
	if err := a.Pick("model", "uni-switch/relay/glm-4.6"); err != nil {
		t.Fatal(err)
	}
	if a.Field("model").Get() != "uni-switch/relay/glm-4.6" {
		t.Fatal("model did not take effect")
	}
	if a.Field("fast").Get() != "" {
		t.Fatal("default selection changed a role")
	}
	if err := a.Disconnect(); err != nil {
		t.Fatal(err)
	}
	var got, want any
	_ = json.Unmarshal([]byte(readFile(settings)), &got)
	_ = json.Unmarshal([]byte(before), &want)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("restore got %s want %s", readFile(settings), before)
	}
}

func TestAsideRejectsDaemonFailureWithoutFileFallback(t *testing.T) {
	settings, _ := asideHome(t)
	a := mustFindAside(t)
	before := readFile(settings)
	asideSet = func(string, string) error { return errors.New("refused") }
	if err := a.Apply("model", "uni-switch/relay/glm-4.6"); err == nil {
		t.Fatal("daemon refusal reported success")
	}
	if readFile(settings) != before {
		t.Fatal("refusal wrote settings behind the daemon")
	}
	if _, ok := appliedLoad()["aside"]; ok {
		t.Fatal("refused change was recorded as applied")
	}
}

func TestAsideNativeSelectionKeepsProviderAndEndsOwnership(t *testing.T) {
	settings, _ := asideHome(t)
	a := mustFindAside(t)
	if err := a.Apply("model", "uni-switch/relay/glm-4.6"); err != nil {
		t.Fatal(err)
	}
	if err := edit.SetJSON(settings, edit.KV{Path: "defaultModel.provider", Value: "native"}, edit.KV{Path: "defaultModel.modelId", Value: "other"}); err != nil {
		t.Fatal(err)
	}
	a = mustFindAside(t)
	if !a.Wired() || a.Drift() != nil {
		t.Fatal("native model disconnected the provider")
	}
	if err := a.Disconnect(); err != nil {
		t.Fatal(err)
	}
	if a.Field("model").Get() != "native/other" {
		t.Fatal("disconnect overwrote the user's newer model")
	}
}

func TestAsideLegacySelectionNeedsRestoreTarget(t *testing.T) {
	settings, models := asideHome(t)
	writeFile(t, settings, `{"defaultModel":{"provider":"uni-switch","modelId":"relay/glm-4.6","thinkingLevel":"high"}}`)
	a := mustFindAside(t)
	if err := a.Connect(); err != nil {
		t.Fatal(err)
	}
	before := readFile(settings) + readFile(models)
	if err := a.Disconnect(); err == nil {
		t.Fatal("disconnect guessed a legacy restore target")
	}
	if readFile(settings)+readFile(models) != before {
		t.Fatal("blocked disconnect changed config")
	}
}

func TestAsideForeignProviderAndSyncAreReadOnly(t *testing.T) {
	settings, models := asideHome(t)
	writeFile(t, models, `{"providers":{"uni-switch":{"apiKey":"someone-else","baseUrl":"https://other.invalid/v1"}}}`)
	before := readFile(settings) + readFile(models)
	a := mustFindAside(t)
	if err := a.Connect(); err == nil {
		t.Fatal("foreign provider overwritten")
	}
	if err := a.Sync(); err != nil {
		t.Fatal(err)
	}
	if readFile(settings)+readFile(models) != before {
		t.Fatal("foreign provider was changed")
	}
}

func TestAsideUnavailableRuntimeCanRegisterProvider(t *testing.T) {
	settings, _ := asideHome(t)
	before := readFile(settings)
	asideRead = func(string) (map[string]json.RawMessage, error) { return nil, errors.New("not running") }
	a := mustFindAside(t)
	if err := a.Connect(); err != nil {
		t.Fatal(err)
	}
	if !a.Wired() {
		t.Fatal("saved provider not registered")
	}
	if err := a.Apply("model", "uni-switch/relay/glm-4.6"); err == nil {
		t.Fatal("unreachable runtime accepted model")
	}
	if readFile(settings) != before {
		t.Fatal("model silently written offline")
	}
}

func TestAsidePreviewIsAPurePlan(t *testing.T) {
	settings, models := asideHome(t)
	a := mustFindAside(t)
	if err := a.Apply("model", "uni-switch/relay/glm-4.6"); err != nil {
		t.Fatal(err)
	}
	record := newAsideConnection(here("")).record
	before := readFile(settings) + readFile(models) + readFile(record)
	asideSet = func(string, string) error { t.Fatal("preview wrote runtime"); return nil }
	asideRead = func(string) (map[string]json.RawMessage, error) { t.Fatal("preview invoked runtime"); return nil, nil }
	for i := 0; i < 3; i++ {
		changes, err := DisconnectPreview(a, "no executable needed")
		if err != nil {
			t.Fatal(err)
		}
		if len(changes) != 3 {
			t.Fatalf("plan changes %v", changes)
		}
	}
	if readFile(settings)+readFile(models)+readFile(record) != before {
		t.Fatal("preview changed live files")
	}
}

func TestAsideDisconnectRetainsProviderOnFailedRestore(t *testing.T) {
	_, models := asideHome(t)
	a := mustFindAside(t)
	if err := a.Apply("model", "uni-switch/relay/glm-4.6"); err != nil {
		t.Fatal(err)
	}
	asideSet = func(string, string) error { return errors.New("restore refused") }
	if err := a.Disconnect(); err == nil {
		t.Fatal("failed restoration reported success")
	}
	if _, ok := edit.GetJSON(models, "providers.uni-switch"); !ok {
		t.Fatal("provider removed before successful restoration")
	}
}

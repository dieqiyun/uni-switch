package agent

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/edit"
	"github.com/dieqiyun/uni-switch/backend/internal/gateway"
)

func TestAsideStageWritesSelectionObjectAndConfirmsPending(t *testing.T) {
	settings, _ := asideHome(t)
	a := mustFindAside(t)
	if err := a.Native.Stage("model", "uni-switch/relay/glm-4.6"); err != nil {
		t.Fatal(err)
	}
	var saved struct {
		Default json.RawMessage `json:"defaultModel"`
	}
	if err := json.Unmarshal([]byte(readFile(settings)), &saved); err != nil {
		t.Fatal(err)
	}
	if asideSelection(saved.Default) != "uni-switch/relay/glm-4.6" {
		t.Fatalf("stage wrote an unusable selection: %s", saved.Default)
	}
	a = mustFindAside(t)
	if got := a.Native.Read().Fields["model"].Status; got != "pendingRestart" {
		t.Fatalf("before live confirmation: %s", got)
	}
	c := newAsideConnection(here(""))
	c.liveSettings()
	if c.readErr != nil {
		t.Fatal(c.readErr)
	}
	a = mustFindAside(t)
	if got := a.Native.Read().Fields["model"].Status; got == "pendingRestart" {
		t.Fatal("matched live read did not clear pending")
	}
}

func TestAsideStageNativeThenUniSwitchRestoresNativeChoice(t *testing.T) {
	asideHome(t)
	a := mustFindAside(t)
	if err := a.Native.Stage("model", "native/other"); err != nil {
		t.Fatal(err)
	}
	if err := a.Native.Stage("model", "uni-switch/relay/glm-4.6"); err != nil {
		t.Fatal(err)
	}
	if err := a.Disconnect(); err != nil {
		t.Fatal(err)
	}
	if got := mustFindAside(t).Field("model").Get(); got != "native/other" {
		t.Fatalf("restored %s", got)
	}
}

func TestAsideStatePassesNeverReadRuntime(t *testing.T) {
	asideHome(t)
	asideRead = func(string) (map[string]json.RawMessage, error) {
		t.Fatal("state read invoked daemon")
		return nil, nil
	}
	for i := 0; i < 3; i++ {
		a := mustFindAside(t)
		a.Values()
		a.Drift()
		a.Wired()
		a.Native.Read()
	}
}

func TestAsideRewireWithoutDaemonOnlyMovesProvider(t *testing.T) {
	t.Setenv("UNI_SWITCH_ADDR", "")
	settings, models := asideHome(t)
	a := mustFindAside(t)
	if err := a.Connect(); err != nil {
		t.Fatal(err)
	}
	before := readFile(settings)
	setPort(t, 3592)
	asideRead = func(string) (map[string]json.RawMessage, error) {
		t.Fatal("rewire read runtime")
		return nil, errors.New("offline")
	}
	asideSet = func(string, string) error { t.Fatal("rewire wrote runtime"); return errors.New("offline") }
	moved, err := Rewire([]*Agent{a})
	if err != nil {
		t.Fatal(err)
	}
	if len(moved) != 1 || moved[0] != "Aside" {
		t.Fatalf("moved %v", moved)
	}
	if url, _ := edit.GetJSON(models, "providers.uni-switch.baseUrl"); url != gateway.URL()+"/v1" {
		t.Fatalf("provider stayed at %s", url)
	}
	if readFile(settings) != before {
		t.Fatal("rewire changed model settings")
	}
}

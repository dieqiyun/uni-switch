package agent

import (
	"encoding/json"
	"errors"
	"testing"
)

func TestAsideOfflineWriteRequiresExplicitStage(t *testing.T) {
	settings, _ := asideHome(t)
	asideRead = func(string) (map[string]json.RawMessage, error) { return nil, errors.New("offline") }
	asideSet = func(string, string) error { return errors.New("offline") }
	a := mustFindAside(t)
	before := readFile(settings)
	if err := a.Apply("model", "uni-switch/relay/glm-4.6"); err == nil {
		t.Fatal("offline apply reported success")
	}
	if readFile(settings) != before {
		t.Fatal("apply silently staged setting")
	}
	if err := a.Native.Stage("model", "uni-switch/relay/glm-4.6"); err != nil {
		t.Fatal(err)
	}
	a = mustFindAside(t)
	if got := a.Native.Read().Fields["model"].Status; got != "pendingRestart" {
		t.Fatalf("stage status %s", got)
	}
	if !a.Wired() {
		t.Fatal("staged provider missing")
	}
}

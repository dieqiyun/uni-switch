package agent

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// Lumens on Discord (v0.1.1233): connecting WorkBuddy said it was
// connected, then its switch went back off and WorkBuddy had none of
// uni-switch's models. WorkBuddy's one field puts uni-switch in its own picker, so
// Connect set it without asking whether any model was left for it: with
// every model taken out of WorkBuddy's list, models.json got none, and the
// row read as not connected. Connect now says why, as it does for an agent
// that picks among the catalog (#1989).
func TestConnectUniSwitchAsProviderWithNoModelLeft(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
	t.Setenv("WORKBUDDY_CONFIG_DIR", "")
	path := filepath.Join(home, ".workbuddy", "models.json")
	os.MkdirAll(filepath.Dir(path), 0o755)
	os.WriteFile(path, []byte(`[{"id":"qwen-local","local":true,"url":"http://127.0.0.1:8080/v1/chat/completions"}]`), 0o600)

	// nothing added in uni-switch yet
	_, err := workbuddy(home).ConnectHow()
	var nm *NoModelsError
	if !errors.As(err, &nm) || nm.Agent != "WorkBuddy" {
		t.Fatalf("no providers: %v", err)
	}

	if err := provider.Save(provider.Provider{ID: "gem", Name: "Gem", Key: "k", Chat: "http://127.0.0.1:1/v1", Models: []string{"gemini-3.8-flash"}}); err != nil {
		t.Fatal(err)
	}
	if err := provider.Save(provider.Provider{ID: "deepseek", Name: "DeepSeek", Key: "k", Chat: "http://127.0.0.1:1/v1", Models: []string{"pro"}}); err != nil {
		t.Fatal(err)
	}
	// shown only a model picked for it whose provider is off
	if _, err := provider.SetModelLists(map[string]provider.ModelList{"workbuddy": {Only: true, Picked: []string{"gem/gemini-3.8-flash"}}}); err != nil {
		t.Fatal(err)
	}
	if err := provider.SetOff("gem", true); err != nil {
		t.Fatal(err)
	}
	_, err = workbuddy(home).ConnectHow()
	var ah *AllHiddenError
	if !errors.As(err, &ah) || ah.Agent != "WorkBuddy" {
		t.Fatalf("every model taken out: %v", err)
	}
	if workbuddyWired(path) {
		t.Fatal("wired with no model")
	}

	if err := provider.SetOff("gem", false); err != nil {
		t.Fatal(err)
	}
	c, err := workbuddy(home).ConnectHow()
	if err != nil || c.How != "uni-switch" {
		t.Fatalf("with a model: %+v %v", c, err)
	}
	if !workbuddyWired(path) {
		t.Fatal("not wired")
	}
}

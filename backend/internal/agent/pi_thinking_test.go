package agent

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/edit"
)

// Changing Pi's thinking level writes defaultThinkingLevel alone, and only
// when it changed. It used to rebuild providers.uni-switch, so a contextWindow
// edited by hand was replaced. Default clears the level and leaves the
// model on uni-switch; picking the model is what removes the provider.
func TestPiThinkingLeavesTheProvider(t *testing.T) {
	home := syncHome(t)
	dir := filepath.Join(home, ".pi", "agent")
	settings := filepath.Join(dir, "settings.json")
	models := filepath.Join(dir, "models.json")
	settingsBody := `{
  "theme": "dark",
  "defaultProvider": "uni-switch",
  "defaultModel": "relay/glm-4.6",
  "defaultThinkingLevel": "low"
}
`
	modelsBody := `{
  "providers": {
    "mine": {"baseUrl": "http://x"},
    "uni-switch": {
      "name": "uni-switch",
      "models": [
        {"id": "relay/glm-4.6", "name": "GLM", "contextWindow": 272000, "reasoning": false}
      ]
    }
  }
}
`
	writeFile(t, settings, settingsBody)
	writeFile(t, models, modelsBody)
	effort := pi(home).Field("effort")

	before, _ := os.Stat(settings)
	if err := effort.Set("low"); err != nil {
		t.Fatal(err)
	}
	if got := readFile(settings); got != settingsBody {
		t.Fatalf("unchanged thinking rewrote settings:\n%s", got)
	}
	if got := readFile(models); got != modelsBody {
		t.Fatalf("unchanged thinking rewrote models:\n%s", got)
	}
	if now, _ := os.Stat(settings); !now.ModTime().Equal(before.ModTime()) {
		t.Fatal("unchanged thinking touched settings.json")
	}

	if err := effort.Set("high"); err != nil {
		t.Fatal(err)
	}
	for k, want := range map[string]string{
		"theme": "dark", "defaultProvider": "uni-switch", "defaultModel": "relay/glm-4.6", "defaultThinkingLevel": "high",
	} {
		if v, _ := edit.GetJSON(settings, k); v != want {
			t.Fatalf("%s = %q\n%s", k, v, readFile(settings))
		}
	}
	if got := readFile(models); got != modelsBody {
		t.Fatalf("thinking change rewrote models:\n%s", got)
	}

	if err := effort.Set(""); err != nil {
		t.Fatal(err)
	}
	if _, ok := edit.GetJSON(settings, "defaultThinkingLevel"); ok {
		t.Fatalf("default left thinking:\n%s", readFile(settings))
	}
	for k, want := range map[string]string{"theme": "dark", "defaultProvider": "uni-switch", "defaultModel": "relay/glm-4.6"} {
		if v, _ := edit.GetJSON(settings, k); v != want {
			t.Fatalf("default changed %s to %q\n%s", k, v, readFile(settings))
		}
	}
	if got := readFile(models); got != modelsBody {
		t.Fatalf("default rewrote models:\n%s", got)
	}

	cleared := readFile(settings)
	clearedAt, _ := os.Stat(settings)
	if err := effort.Set(""); err != nil {
		t.Fatal(err)
	}
	if got := readFile(settings); got != cleared {
		t.Fatalf("default again rewrote settings:\n%s", got)
	}
	if now, _ := os.Stat(settings); !now.ModTime().Equal(clearedAt.ModTime()) {
		t.Fatal("default again touched settings.json")
	}
	if got := readFile(models); got != modelsBody {
		t.Fatalf("default again rewrote models:\n%s", got)
	}
}

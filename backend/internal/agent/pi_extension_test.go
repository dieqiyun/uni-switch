package agent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/catalog"
	"github.com/dieqiyun/uni-switch/backend/internal/edit"
	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// piExtensionHome is a sandbox home with a provider that decides
// (typesafe/jev) and one that draws (gpt-image-2, which takes images too).
func piExtensionHome(t *testing.T) string {
	t.Helper()
	home := syncHome(t)
	// no request reaches these: the extension only names them
	if err := provider.Save(provider.Provider{ID: "mixed", Name: "Mixed", Key: "k", Chat: "http://127.0.0.1:9/v1", Decide: "http://127.0.0.1:9/v1", Models: []string{"deepseek-v4.1-flash", "typesafe/jev"}}); err != nil {
		t.Fatal(err)
	}
	art := provider.Provider{ID: "art", Name: "Art", Responses: "http://127.0.0.1:9/v1", Key: "k"}
	if err := provider.Save(art); err != nil {
		t.Fatal(err)
	}
	if err := catalog.SaveLive("art", art.Responses, []catalog.Model{{ID: "text"}, {ID: "gpt-image-2", Name: "GPT Image 2", Draws: true, Images: true}}); err != nil {
		t.Fatal(err)
	}
	return home
}

type piExtensionData struct {
	BaseURL     string             `json:"baseUrl"`
	APIKey      string             `json:"apiKey"`
	Classifiers []piExtensionModel `json:"classifiers"`
	Images      []piExtensionModel `json:"images"`
}

// readPiExtension is the data in the extension uni-switch wrote at path.
func readPiExtension(t *testing.T, path string) piExtensionData {
	t.Helper()
	src := readFile(path)
	if !strings.HasPrefix(src, piExtensionMark) {
		t.Fatalf("%s is not uni-switch's: %.200s", path, src)
	}
	_, rest, ok := strings.Cut(src, "const UNI_SWITCH = ")
	line, _, _ := strings.Cut(rest, ";\n")
	var d piExtensionData
	if !ok || json.Unmarshal([]byte(line), &d) != nil {
		t.Fatalf("no UNI_SWITCH data in %s: %.400s", path, rest)
	}
	return d
}

func hasModel(ms []piExtensionModel, id string) (piExtensionModel, bool) {
	for _, m := range ms {
		if m.ID == id {
			return m, true
		}
	}
	return piExtensionModel{}, false
}

// Pi's codemode reaches classifier and image models, which models.json
// can't define (wangbinyq on Discord): picking a model of uni-switch's writes
// extensions/uni-switch.ts with uni-switch's decision and image models, and
// taking Pi off uni-switch removes it.
func TestPiExtensionListsDecisionAndImageModels(t *testing.T) {
	home := piExtensionHome(t)
	a := pi(home)
	path := piExtensionPath(a.Dir)
	if err := a.Field("model").Set(uniSwitchID + "/mixed/deepseek-v4.1-flash"); err != nil {
		t.Fatal(err)
	}
	d := readPiExtension(t, path)
	if d.BaseURL != here(home).v1() {
		t.Errorf("baseUrl = %q, want %q", d.BaseURL, here(home).v1())
	}
	if want := mustGet(t, filepath.Join(a.Dir, "models.json"), "providers.uni-switch.apiKey"); d.APIKey != strings.Trim(want, `"`) {
		t.Errorf("apiKey = %q, models.json's is %s", d.APIKey, want)
	}
	if _, ok := hasModel(d.Classifiers, "mixed/typesafe/jev"); !ok {
		t.Errorf("classifiers = %+v, want mixed/typesafe/jev", d.Classifiers)
	}
	if _, ok := hasModel(d.Classifiers, "mixed/deepseek-v4.1-flash"); ok {
		t.Errorf("a chat model listed as a classifier: %+v", d.Classifiers)
	}
	img, ok := hasModel(d.Images, "art/gpt-image-2")
	if !ok {
		t.Fatalf("images = %+v, want art/gpt-image-2", d.Images)
	}
	if strings.Join(img.Input, ",") != "text,image" || img.Name != "GPT Image 2 · Art" {
		t.Errorf("image model = %+v", img)
	}
	if !strings.Contains(readFile(path), `pi.registerProvider("uni-switch-systemone"`) || !strings.Contains(readFile(path), `"typesafe-system-one"`) {
		t.Errorf("extension source: %.300s", readFile(path))
	}

	if err := a.Field("model").Set(""); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("extension left after Pi went off uni-switch: %v", err)
	}
}

// The catalog sync rewrites the extension as uni-switch's models change, and
// removes it once models.json has no provider uni-switch.
func TestPiExtensionFollowsSync(t *testing.T) {
	home := piExtensionHome(t)
	a := pi(home)
	path := piExtensionPath(a.Dir)
	if err := a.Field("model").Set(uniSwitchID + "/mixed/deepseek-v4.1-flash"); err != nil {
		t.Fatal(err)
	}
	// the one provider that draws removed: no image model is listed
	if err := provider.Delete("art"); err != nil {
		t.Fatal(err)
	}
	if err := a.Sync(); err != nil {
		t.Fatal(err)
	}
	if d := readPiExtension(t, path); len(d.Images) != 0 || len(d.Classifiers) == 0 {
		t.Errorf("after the drawing provider went: %+v", d)
	}
	models := filepath.Join(a.Dir, "models.json")
	if err := edit.DelJSON(models, "providers.uni-switch"); err != nil {
		t.Fatal(err)
	}
	if err := a.Sync(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("extension left with no provider uni-switch: %v", err)
	}
}

// A uni-switch.ts the user wrote themselves is theirs: never written over,
// never removed.
func TestPiExtensionLeavesTheUsersOwn(t *testing.T) {
	home := piExtensionHome(t)
	a := pi(home)
	path := piExtensionPath(a.Dir)
	os.MkdirAll(filepath.Dir(path), 0o755)
	own := "// my own uni-switch helpers\nexport default function (pi) {}\n"
	os.WriteFile(path, []byte(own), 0o644)
	if err := a.Field("model").Set(uniSwitchID + "/mixed/deepseek-v4.1-flash"); err != nil {
		t.Fatal(err)
	}
	if err := a.Sync(); err != nil {
		t.Fatal(err)
	}
	if got := readFile(path); got != own {
		t.Errorf("the user's file written over: %.200s", got)
	}
	if err := a.Field("model").Set(""); err != nil {
		t.Fatal(err)
	}
	if got := readFile(path); got != own {
		t.Errorf("the user's file removed or changed: %q", got)
	}
}

// OmO keeps Pi's files but may not load Pi's extensions: it gets none.
func TestPiExtensionPiOnly(t *testing.T) {
	home := piExtensionHome(t)
	a := omo(home)
	if err := a.Field("model").Set(uniSwitchID + "/mixed/deepseek-v4.1-flash"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(piExtensionPath(a.Dir)); !os.IsNotExist(err) {
		t.Errorf("OmO got Pi's extension: %v", err)
	}
}

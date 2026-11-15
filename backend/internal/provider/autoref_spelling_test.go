package provider

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/catalog"
)

// glmCatalog lists GLM-5.3 Flash as models.dev does: glm-5.3-flash under
// most providers, named GLM-5.3-Flash, and Volcengine Ark's dashed
// glm-5-3-flash under one, named otherwise.
const glmCatalog = `{
	  "zai": {"models": {"glm-5.3-flash": {"id":"glm-5.3-flash","name":"GLM-5.3-Flash"}}},
	  "fireworks": {"models": {"glm-5.3-flash": {"id":"glm-5.3-flash","name":"GLM-5.3-Flash"}}},
	  "openrouter": {"models": {"z-ai/glm-5.3-flash": {"id":"z-ai/glm-5.3-flash","name":"GLM-5.3-Flash"}}},
	  "volcengine": {"models": {"glm-5-3-flash": {"id":"glm-5-3-flash","name":"GLM 5.3 Flash"}}}}`

// glmDottedOnly lists it under the dotted spelling alone.
const glmDottedOnly = `{
	  "zai": {"models": {"glm-5.3-flash": {"id":"glm-5.3-flash","name":"GLM-5.3-Flash"}}},
	  "fireworks": {"models": {"glm-5.3-flash": {"id":"glm-5.3-flash","name":"GLM-5.3-Flash"}}}}`

// spellingHome is a sandbox HOME with the models.dev catalog cat, and
// models the providers' own, one provider each, in the order they are
// added.
func spellingHome(t *testing.T, cat string, models ...string) {
	t.Helper()
	isolate(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
	if err := os.MkdirAll(filepath.Dir(catalog.CachePath()), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(catalog.CachePath(), []byte(cat), 0o644); err != nil {
		t.Fatal(err)
	}
	catalog.Reset()
	t.Cleanup(catalog.Reset)
	for i, m := range models {
		id := string(rune('a' + i))
		if err := Save(Provider{ID: id, Name: id, Key: "k", Chat: "http://127.0.0.1:1/v1", Models: []string{m}}); err != nil {
			t.Fatal(err)
		}
	}
}

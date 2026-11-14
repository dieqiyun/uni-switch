package agent

import (
	"path/filepath"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/catalog"
	"github.com/dieqiyun/uni-switch/backend/internal/provider"
	"github.com/dieqiyun/uni-switch/backend/internal/settings"
)

// visionOff has no model describe images to one that can't see them, so an
// agent is told which models take images as they are.
func visionOff(t *testing.T) {
	t.Helper()
	s := settings.Load()
	s.Vision = "off"
	if err := settings.Save(s); err != nil {
		t.Fatal(err)
	}
}

// An agent's model file says a text-only model takes images while a model
// that sees describes them to it, and text only with Vision off (Discord,
// Fate).
func TestDescribedModelTakesImages(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
	if err := provider.Save(provider.Provider{ID: "v", Name: "V", Chat: "https://example.test/v1", Key: "k", Models: []string{"see", "plain"}}); err != nil {
		t.Fatal(err)
	}
	if err := catalog.SaveLive("v", "https://example.test/v1", []catalog.Model{
		{ID: "see", Name: "see", Images: true, ImageInput: imageInputBool(true)},
		{ID: "plain", Name: "plain", ImageInput: imageInputBool(false)},
	}); err != nil {
		t.Fatal(err)
	}
	plain := func() catalog.Model {
		for _, m := range uniSwitchModels("dsh") {
			if m.ID == "v/plain" {
				return m
			}
		}
		t.Fatal("v/plain not listed")
		return catalog.Model{}
	}
	if m := plain(); !m.Images || m.ImageInput == nil || !*m.ImageInput {
		t.Fatalf("with v/see describing, v/plain is %+v", m)
	}
	visionOff(t)
	if m := plain(); m.Images || m.ImageInput == nil || *m.ImageInput {
		t.Fatalf("with Vision off, v/plain is %+v", m)
	}
}

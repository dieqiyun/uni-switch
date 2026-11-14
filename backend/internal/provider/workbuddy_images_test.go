package provider

import (
	"path/filepath"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/catalog"
)

// WorkBuddy AI's GLM models take no images, though its config says every
// model does: on 2026-10-13 a real account's image sent to glm-5.3-flash,
// glm-5.3 and glm-5.2 came back as "[System reminder: the current model
// does not support images. Content filtered. ...]" (Yirasumi on Discord).
// A list saved with WorkBuddy AI's GLM taking images reads text-only, and
// WorkBuddy (China)'s GLM, which reads them, is left as it is.
func TestWorkBuddyAISavedGLMIsTextOnly(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
	yes := true
	saved := []catalog.Model{{ID: "glm-5.3-flash", Images: true, ImageInput: &yes}, {ID: "deepseek-v4.1-flash", Images: true, ImageInput: &yes}}
	for _, id := range []string{WorkBuddyAIID, "workbuddy"} {
		if err := catalog.SaveLive(id, "", saved); err != nil {
			t.Fatal(err)
		}
	}
	want := map[string]map[string]bool{
		WorkBuddyAIID: {"glm-5.3-flash": false, "deepseek-v4.1-flash": true},
		"workbuddy":   {"glm-5.3-flash": true, "deepseek-v4.1-flash": true},
	}
	for pid, w := range want {
		seen := 0
		for _, m := range (Provider{ID: pid}).Available() {
			v, ok := w[m.ID]
			if !ok {
				continue
			}
			seen++
			if m.ImageInput == nil || *m.ImageInput != v || m.Images != v {
				t.Errorf("%s %s: Images %v, ImageInput %v, want %v", pid, m.ID, m.Images, m.ImageInput, v)
			}
		}
		if seen != len(w) {
			t.Errorf("%s: %d of the saved models listed", pid, seen)
		}
	}
}

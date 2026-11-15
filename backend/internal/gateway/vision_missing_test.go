package gateway

import (
	"strings"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/settings"
)

// pickVision saves the Image recognition model the user picked.
func pickVision(t *testing.T, v string) {
	t.Helper()
	st := settings.Load()
	st.Vision = v
	if err := settings.Save(st); err != nil {
		t.Fatal(err)
	}
}

// Image generation's pick is the same: one uni-switch can't find is named, and
// a request that names no model, with nothing else to draw with, says so.
func TestMissingImageGenerationModelIsSaid(t *testing.T) {
	s, _ := eyed(t)
	st := settings.Load()
	st.ImageGen = "gone/gpt-image-2"
	if err := settings.Save(st); err != nil {
		t.Fatal(err)
	}
	if got := DrawerMissing(); got != "gone/gpt-image-2" {
		t.Fatalf("DrawerMissing = %q", got)
	}
	code, _, raw := postImages(t, s, "/v1/images/generations", "application/json", `{"prompt":"a uni-switch"}`)
	if code != 400 || !strings.Contains(raw, "gone/gpt-image-2") || !strings.Contains(raw, "Image generation") {
		t.Fatalf("no drawer: %d %s, want 400 naming the missing gone/gpt-image-2", code, raw)
	}
	st.ImageGen = "off"
	if err := settings.Save(st); err != nil {
		t.Fatal(err)
	}
	if got := DrawerMissing(); got != "" {
		t.Fatalf("DrawerMissing with off = %q", got)
	}
}

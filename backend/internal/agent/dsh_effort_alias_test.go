package agent

import (
	"slices"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/catalog"
)

// TestDshOffersTraeNamedLevels: Our Free Model's plugin lists Trae's
// DeepSeek V4.1 Flash with the levels Trae names, light, high and
// extra_high (its 轻, 高 and 极高). dsh offered high alone, since only
// levels named in pi-ai's words were written (nianshou5 on Discord). Each
// is now offered as the level of dsh's it is, and sent in the model's own
// words; Pi's thinkingLevelMap is built the same way.
func TestDshOffersTraeNamedLevels(t *testing.T) {
	m := catalog.Model{ID: "our-free-model/trae/deepseek-v4.1-flash", Context: 1000000, Output: 64000, Efforts: []string{"light", "high", "extra_high"}}
	levels, got := dshModelLevels(m.Efforts)
	if want := []string{"low", "high", "xhigh"}; !slices.Equal(levels, want) {
		t.Fatalf("dsh levels %v, want %v", levels, want)
	}
	if want := (dshPiLevels{Low: "light", High: "high", XHigh: "extra_high"}); got == nil || *got != want {
		t.Fatalf("dsh reasoningEfforts %+v, want %+v", got, want)
	}
	if e := dshKindConfig(dshKinds[0], []catalog.Model{m}, "", "http://127.0.0.1:3425").Models[0].ReasoningEfforts; e == nil || e.XHigh != "extra_high" || e.Low != "light" {
		t.Fatalf("route entry's reasoningEfforts %+v", e)
	}

	pi := piThinkingLevels(m.Efforts, false)
	for l, want := range map[string]any{"low": "light", "high": "high", "xhigh": "extra_high", "medium": nil, "max": nil, "off": nil, "minimal": nil} {
		if pi[l] != want {
			t.Errorf("Pi's %s = %v, want %v", l, pi[l], want)
		}
	}

	// a model naming pi-ai's own levels is as before, and one naming both
	// spellings is sent its own pi-ai name
	if _, std := dshModelLevels([]string{"none", "low", "high", "max"}); std == nil || *std != (dshPiLevels{Off: "none", Low: "low", High: "high", Max: "max"}) {
		t.Errorf("standard levels %+v", std)
	}
	if e := modelEffort([]string{"light", "low"}, "low"); e != "low" {
		t.Errorf("low with light beside it = %q, want low", e)
	}
	if e := modelEffort([]string{"high"}, "low"); e != "" {
		t.Errorf("a level the model doesn't take = %q, want none", e)
	}
}

package agent

import (
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/catalog"
)

// TestDshMaxReachesOutputEffortModels: pi-ai asks a model on Messages for a
// budget unless it is told to think adaptively, and its budget path clamps
// max to high (16384), so DSH's Max on zcode's GLM-5.3 reached it as high
// (01huadalang on Discord, 10-24). Z.ai's GLM-5.2 and GLM-5.3, which take
// adaptive thinking with their level, are told to think adaptively on
// uni-switch's Anthropic route, which has pi-ai send the level itself; on the
// other routes, for MiniMax (not tried adaptive, #3496) and for a model
// that takes a budget, nothing changes.
func TestDshMaxReachesOutputEffortModels(t *testing.T) {
	for _, tc := range []struct {
		id    string
		route string
		want  bool
	}{
		{"zcode/GLM-5.3", "anthropic-messages", true},
		{"zai/glm-5.2", "anthropic-messages", true},
		{"minimax/MiniMax-M3.1-Flash", "anthropic-messages", false},
		{"anth/claude-sonnet-5", "anthropic-messages", true},
		{"kimi/kimi-k3", "anthropic-messages", false},
		{"zai/glm-4.6", "anthropic-messages", false},
		{"zcode/GLM-5.3", "openai-completions", false},
		{"zcode/GLM-5.3", "openai-responses", false},
	} {
		m := catalog.Model{ID: tc.id, Context: 1000000, Output: 128000, Efforts: []string{"low", "high", "max"}}
		for _, k := range dshKinds {
			if k.api != tc.route {
				continue
			}
			got := dshKindConfig(k, []catalog.Model{m}, tc.route, "http://127.0.0.1:3425").Models[0]
			if adaptive := got.Compat != nil && got.Compat.ForceAdaptiveThinking; adaptive != tc.want {
				t.Errorf("%s on %s: forceAdaptiveThinking %v, want %v", tc.id, tc.route, adaptive, tc.want)
			}
		}
	}
}

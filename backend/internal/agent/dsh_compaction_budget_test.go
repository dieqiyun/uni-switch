package agent

import (
	"fmt"
	"strings"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/catalog"
)

// dshCompactsAt is where dsh's compaction-basic starts compacting a model
// listed with window and maxTokens out, as its resolveCompactSpec works it
// out with the default policy (thresholdRatio 0.8, headroomTokens 65536):
// 0 when it has no budget and fails instead (#2823).
func dshCompactsAt(window, out int) int {
	pressure := window - out - 65536
	if window-out <= 0 || pressure <= 0 {
		return 0
	}
	return min(window*8/10, pressure)
}

// TestDshOutputLeavesCompactionRoom: dsh reserves the maxTokens uni-switch lists
// a model with, and 65536 more, out of its window before it compacts. A
// vendor's reply limit near the window had dsh compact at 41248 of a 500000
// group (#2823) or fail outright on 524288/524288 (LoKi on Discord) and
// 131072/65536; it is held to what leaves dsh half the window
// (window/2 - 65536), or a quarter of the window where that is more, on every
// route and in the older llm-deepseek entry, while a Max output the user set
// stays. DeepSeek's own 1000000/393216 keeps all of its output (#3298).
func TestDshOutputLeavesCompactionRoom(t *testing.T) {
	for _, tc := range []struct {
		name  string
		model catalog.Model
		want  int
	}{
		{"full window", catalog.Model{ID: "space-bunny-free", Context: 524288, Output: 524288}, 196608},
		{"above the window", catalog.Model{ID: "above", Context: 524288, Output: 1048576}, 196608},
		{"group window under its members' output", catalog.Model{ID: "group/smart-split", Context: 500000, Output: 393216}, 184464},
		{"half the window", catalog.Model{ID: "k131", Context: 131072, Output: 65536}, 32768},
		{"within a quarter", catalog.Model{ID: "flash", Context: 1000000, Output: 128000}, 128000},
		{"DeepSeek's window and output", catalog.Model{ID: "deepseek/deepseek-flash", Context: 1000000, Output: 393216}, 393216},
		{"a 200K window", catalog.Model{ID: "ds200", Context: 200000, Output: 393216}, 50000},
		{"a 128K window", catalog.Model{ID: "ds128", Context: 128000, Output: 393216}, 32000},
		{"user output", catalog.Model{ID: "user", Context: 524288, Output: 393216, OutputSet: true}, 393216},
		{"user full window", catalog.Model{ID: "user-full", Context: 524288, Output: 524288, OutputSet: true}, 524288},
		{"unknown window", catalog.Model{ID: "unknown", Output: 128000}, 128000},
		{"unknown output", catalog.Model{ID: "no-output", Context: 524288}, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, k := range dshKinds {
				got := dshKindConfig(k, []catalog.Model{tc.model}, "", "http://127.0.0.1:3425").Models[0]
				if got.MaxTokens != tc.want || got.ContextWindow != tc.model.Context {
					t.Fatalf("%s: contextWindow %d, maxTokens %d; want %d, %d", k.key, got.ContextWindow, got.MaxTokens, tc.model.Context, tc.want)
				}
			}
			lines := strings.Join(dshProviderLines("high", []catalog.Model{tc.model}, "http://127.0.0.1:3425"), "\n")
			if tc.want > 0 && !strings.Contains(lines+"\n", fmt.Sprintf("maxTokens: %d\n", tc.want)) {
				t.Fatalf("llm-deepseek entry, want maxTokens: %d:\n%s", tc.want, lines)
			}
			if tc.want == 0 && strings.Contains(lines, "maxTokens") {
				t.Fatalf("llm-deepseek entry lists a maxTokens for a model of no known output:\n%s", lines)
			}
			if tc.model.Context > 0 && !tc.model.OutputSet {
				// half the window for messages where the window has a
				// half to spare beside the headroom, and never under
				// what a quarter for output leaves
				floor := min(tc.model.Context/2, dshCompactsAt(tc.model.Context, min(tc.model.Output, tc.model.Context/4)))
				if at := dshCompactsAt(tc.model.Context, tc.want); at == 0 || at < floor {
					t.Fatalf("dsh compacts %d/%d at %d tokens, under %d", tc.model.Context, tc.want, at, floor)
				}
			}
		})
	}
}

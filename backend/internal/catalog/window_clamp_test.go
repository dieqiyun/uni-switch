package catalog

import "testing"

// window holds a models.dev row whose input limit sits above its window
// (Cloudflare AI Gateway's gpt-5 at 272K over 128K) to the window, and
// keeps the input limit everywhere else (gpt-5's 272K of its 400K).
func TestWindowClampsInputAboveContext(t *testing.T) {
	for _, tc := range []struct {
		name        string
		context, in int
		want        int
	}{
		{"input below context", 400_000, 272_000, 272_000},
		{"input above context", 128_000, 272_000, 128_000},
		{"input only", 0, 200_000, 200_000},
		{"context only", 1_000_000, 0, 1_000_000},
		// a limit of 512 is none (H20 on Discord): the other answers, or
		// nothing does; gpt-3.5-turbo-instruct's real 4095 stands
		{"context 512", 512, 0, 0},
		{"input 512 under context", 192_000, 512, 192_000},
		{"context 512 over input", 512, 192_000, 192_000},
		{"small but real", 4095, 0, 4095},
	} {
		m := mdModel{}
		m.Limit.Context, m.Limit.Input = tc.context, tc.in
		if got := m.window(); got != tc.want {
			t.Errorf("%s: %d, want %d", tc.name, got, tc.want)
		}
	}
}

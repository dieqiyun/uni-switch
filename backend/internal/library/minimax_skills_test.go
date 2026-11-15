package library

import (
	"path/filepath"
	"testing"
)

// #362: MiniMax Code loads the skills in its data folder, so the library
// gives them there — ~/.minimax/skills, or $MINIMAX_DATA_DIR's — and, with
// no user-wide MCP file of its own, no MCP servers.
func TestMiniMaxCodeSkills(t *testing.T) {
	h := sandbox(t)
	t.Setenv("MINIMAX_DATA_DIR", "")
	write(t, filepath.Join(h, ".minimax/config.yaml"), "")
	tg := targetByID("minimax-code")
	if tg == nil || tg.Skills != filepath.Join(h, ".minimax/skills") || tg.MCP != nil {
		t.Fatalf("%+v", tg)
	}
	d := filepath.Join(h, "mm")
	t.Setenv("MINIMAX_DATA_DIR", d)
	write(t, filepath.Join(d, "config.yaml"), "")
	if tg := targetByID("minimax-code"); tg == nil || tg.Skills != filepath.Join(d, "skills") {
		t.Fatalf("with MINIMAX_DATA_DIR: %+v", tg)
	}
}

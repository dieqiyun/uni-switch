package sessions

import (
	"path/filepath"
	"testing"
)

// Codex says no roots, so the MCP hub asks the folder of the thread a
// call names: its rollout's session_meta cwd, of the thread named and no
// other, nothing for one with no rollout.
func TestCodexThreadCwd(t *testing.T) {
	d := setupCalls(t)
	day := filepath.Join(d.codex, "sessions", "2026", "10", "30")
	const id = "019a1111-2222-7333-8444-555566667777"
	writeLines(t, filepath.Join(day, "rollout-2026-10-30T09-00-00-"+id+".jsonl"),
		`{"timestamp":"`+stamp(0)+`","type":"session_meta","payload":{"id":"`+id+`","cwd":"/work/proj"}}`)
	writeLines(t, filepath.Join(day, "rollout-2026-10-30T09-05-00-019a9999-2222-7333-8444-555566667777.jsonl"),
		`{"timestamp":"`+stamp(0)+`","type":"session_meta","payload":{"id":"019a9999-2222-7333-8444-555566667777","cwd":"/work/other"}}`)
	Reset()
	if got := CodexThreadCwd(id); got != "/work/proj" {
		t.Errorf("CodexThreadCwd = %q, want /work/proj", got)
	}
	for _, none := range []string{"", "019a0000-2222-7333-8444-555566667777"} {
		if got := CodexThreadCwd(none); got != "" {
			t.Errorf("CodexThreadCwd(%q) = %q, want none", none, got)
		}
	}
}

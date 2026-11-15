package sessions

import (
	"path/filepath"
	"testing"
)

// A session Claude Desktop's Code tab ran is kept in Claude Code's own
// folder, and only its lines' entrypoint tells it apart. Usage counts it
// under Desktop, as its calls are (TestCallsDesktop3pCodeTab), not under
// Claude Code (CyberCaius on Discord: Desktop's sessions under Claude
// Code); its key, resume and transcript stay Claude Code's, and the
// gateway's record of it under Desktop is the same session, not another.
func TestStatsDesktopCodeTab(t *testing.T) {
	for _, entry := range []string{"claude-desktop", "claude-desktop-3p"} {
		t.Run(entry, func(t *testing.T) {
			d := setupCalls(t)
			writeLines(t, filepath.Join(d.claude, "projects", "-work-app", "s3p.jsonl"),
				// first an attachment, whose folder and entrypoint are taken
				// without reading it whole (ccWhere)
				`{"parentUuid":null,"isSidechain":false,"attachment":{"type":"skill_listing"},"type":"attachment","uuid":"a1","timestamp":"`+stamp(0)+`","cwd":"/work/app","sessionId":"s3p","entrypoint":"`+entry+`"}`,
				`{"type":"user","timestamp":"`+stamp(0)+`","message":{"role":"user","content":"hello"},"cwd":"/work/app","sessionId":"s3p","entrypoint":"`+entry+`"}`,
				swap(swap(claudeMsg("m1", "claude-haiku-5-5", 1, 2, 3, 4, 1), `"entrypoint":"cli"`, `"entrypoint":"`+entry+`"`), `"sessionId":"sess1"`, `"sessionId":"s3p"`))
			writeLines(t, filepath.Join(d.claude, "projects", "-work-app", "sess1.jsonl"),
				`{"type":"user","timestamp":"`+stamp(0)+`","message":{"role":"user","content":"hi"},"cwd":"/work/app","sessionId":"sess1","entrypoint":"cli"}`,
				claudeMsg("m2", "claude-opus-5-5", 1, 2, 3, 4, 2))

			apps := map[string]string{}
			for _, s := range List(All) {
				if s.Agent != "claude" {
					t.Fatalf("listed under %q: %+v", s.Agent, s)
				}
				apps[s.ID] = s.App
			}
			if len(apps) != 2 || apps["s3p"] != "claude-desktop" || apps["sess1"] != "" {
				t.Fatalf("apps %v", apps)
			}

			st, native := StatsAndNative(0)
			byModel := map[string]string{}
			for _, day := range st.Days {
				for _, u := range day.Usage {
					byModel[u.Model] = u.Agent
				}
			}
			if byModel["claude-haiku-5-5"] != "claude-desktop" || byModel["claude-opus-5-5"] != "claude" {
				t.Fatalf("usage by model's agent %v", byModel)
			}
			sums := map[string]Summary{}
			for _, s := range st.Sessions {
				sums[s.ID] = s
			}
			if s := sums["s3p"]; s.Agent != "claude-desktop" || s.Key != "claude:s3p" {
				t.Fatalf("desktop summary %+v", s)
			}
			if s := sums["sess1"]; s.Agent != "claude" || s.Key != "claude:sess1" {
				t.Fatalf("cli summary %+v", s)
			}
			if !native["claude|s3p"] || !native["claude-desktop|s3p"] || native["claude-desktop|sess1"] {
				t.Fatalf("native %v", native)
			}
			merged := MergeExternal(List(All), []ExternalSession{{Agent: "claude-desktop", ID: "s3p"}, {Agent: "claude-desktop", ID: "sess1"}})
			if len(merged) != 3 {
				t.Fatalf("merged %d: %+v", len(merged), merged)
			}
		})
	}
}

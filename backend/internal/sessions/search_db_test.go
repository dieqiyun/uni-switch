package sessions

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
)

// searchesEveryPart has SearchSession find each word of each text part of
// s's transcript: a session whose conversation isn't in the file its Path
// names (a database, OpenCode's session info) is searched all the same.
// The agent's own system prompt isn't part of it.
func searchesEveryPart(t *testing.T, s Session) {
	t.Helper()
	tr, err := TranscriptOf(s)
	if err != nil || len(tr.Parts) == 0 {
		t.Fatalf("%s %s: no transcript: %v", s.Agent, s.ID, err)
	}
	tried := 0
	for _, p := range tr.Parts {
		if p.Role == "system" {
			t.Fatalf("%s %s: the agent's own prompt is shown as said: %q", s.Agent, s.ID, p.Text)
		}
		if p.Kind != "text" {
			continue
		}
		for _, w := range strings.Fields(p.Text) {
			w = strings.Trim(w, ".,!?\"'()")
			if len(w) < 4 {
				continue
			}
			tried++
			if _, ok, err := SearchSession(context.Background(), s, NeedleOf(w)); !ok || err != nil {
				t.Fatalf("%s %s: %q, said in %q, wasn't found: %v", s.Agent, s.ID, w, p.Text, err)
			}
		}
	}
	if tried == 0 {
		t.Fatalf("%s %s: nothing to look for in %+v", s.Agent, s.ID, tr.Parts)
	}
}

func TestSearchOpenCodeFiles(t *testing.T) {
	setupMore(t)
	searchesEveryPart(t, find(t, List(All), "opencode", ocMain))
}

func TestSearchOpenCodeDatabase(t *testing.T) {
	data, _ := setupMore(t)
	db := ocMakeV2(t, filepath.Join(data, "opencode", "opencode.db"))
	defer db.Close()
	searchesEveryPart(t, find(t, List(All), "opencode", ocMain))
}

func TestSearchCursorLiveChat(t *testing.T) {
	dir := cursorSetup(t)
	makeCursorChats(t, dir)
	ss := List(0)
	for _, id := range []string{curMain, curOther} {
		s, ok := findSession(ss, "cursor", id)
		if !ok {
			t.Fatalf("%s isn't listed", id)
		}
		searchesEveryPart(t, s)
	}
}

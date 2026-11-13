package sessions

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

// Desktop keeps its own name outside the CLI transcript. The session header
// is cliSessionId, including older CLI runs listed in priorCliSessionIds.
func TestDesktopSessionTitles(t *testing.T) {
	setup(t)
	data := t.TempDir()
	old := callDesktopDirs
	callDesktopDirs = func() []string { return []string{data} }
	held := desktopTitlesHeld
	desktopTitlesHeld = 0
	t.Cleanup(func() { callDesktopDirs, desktopTitlesHeld = old, held; Reset() })
	const current = "11111111-aaaa-4000-8000-000000000001"
	const prior = "11111111-aaaa-4000-8000-000000000002"
	const cowork = "11111111-aaaa-4000-8000-000000000003"
	path := filepath.Join(data, "claude-code-sessions", "account", "org", "local_desktop.json")
	writeDesktopTitle(t, path, `{"sessionId":"local_desktop","cliSessionId":"`+current+`","priorCliSessionIds":["`+prior+`"],"title":"检查会话标题","titleSource":"user","isArchived":false}`)
	writeDesktopTitle(t, filepath.Join(data, "local-agent-mode-sessions", "account", "org", "local_cowork.json"), `{"sessionId":"local_cowork","cliSessionId":"`+cowork+`","title":"Archived Cowork title","isArchived":true}`)
	keys := []string{"claude-desktop:" + current, "claude-desktop:" + prior, "claude-desktop:" + cowork, "claude:" + current, "codex:" + current, "claude-desktop:missing"}
	want := map[string]string{keys[0]: "检查会话标题", keys[1]: "检查会话标题", keys[2]: "Archived Cowork title"}
	if got := Titles(keys); !reflect.DeepEqual(got, want) {
		t.Fatalf("Desktop names without transcripts: got %v, want %v", got, want)
	}
	merged := MergeExternal(nil, []ExternalSession{{Agent: "claude-desktop", ID: current}, {Agent: "claude-desktop", ID: prior}, {Agent: "claude", ID: current}, {Agent: "claude-desktop", ID: "missing"}})
	for i, name := range []string{"检查会话标题", "检查会话标题", current, "missing"} {
		if s := merged[i]; s.Title != name || !s.Gateway || !s.ReadOnly || s.Path != "" || s.Resume != "" {
			t.Fatalf("gateway row %d: %+v, want title %q and gateway capabilities", i, s, name)
		}
	}
	// Desktop's saved title comes first; an absent title keeps the gateway's
	// recorded first prompt, including for other agents.
	for _, e := range []ExternalSession{
		{Agent: "claude-desktop", ID: current, Title: "Recorded prompt"},
		{Agent: "claude-desktop", ID: "missing", Title: "Recorded prompt"},
		{Agent: "claude", ID: current, Title: "Recorded prompt"},
	} {
		want := "Recorded prompt"
		if e.Agent == "claude-desktop" && e.ID == current {
			want = "检查会话标题"
		}
		if got := MergeExternal(nil, []ExternalSession{e})[0].Title; got != want {
			t.Fatalf("Desktop title / recorded prompt priority for %+v: %q, want %q", e, got, want)
		}
	}
	// A native Cowork transcript uses Desktop's displayed name too, and
	// still wins over a gateway projection of the same identity.
	writeDesktopTitle(t, filepath.Join(data, "local-agent-mode-sessions", "account", "org", "local_cowork", ".claude", "projects", "session", cowork+".jsonl"), `{"type":"ai-title","aiTitle":"CLI title","sessionId":"`+cowork+`"}`+"\n")
	native, ok := Get("claude-desktop:" + cowork)
	if !ok || native.Title != "Archived Cowork title" || native.Path == "" {
		t.Fatalf("native Desktop session: %+v, found %v", native, ok)
	}
	if got := MergeExternal([]Session{native}, []ExternalSession{{Agent: "claude-desktop", ID: cowork}}); len(got) != 1 || got[0].Gateway || got[0].Path != native.Path {
		t.Fatalf("native Desktop session did not win: %+v", got)
	}
	// Renaming needs no new model call, and an old CLI ID follows the rename.
	writeDesktopTitle(t, path, `{"sessionId":"local_desktop","cliSessionId":"`+current+`","priorCliSessionIds":["`+prior+`"],"title":"Renamed Desktop session"}`)
	if got := Titles(keys); got[keys[0]] != "Renamed Desktop session" || got[keys[1]] != "Renamed Desktop session" {
		t.Fatalf("rename did not reach current and prior IDs: %v", got)
	}
	// A file midway through being written is unknown, not unnamed: its
	// last name stands, and a later good read replaces it.
	writeDesktopTitle(t, path, `{"cliSessionId":"`+current+`","title":`)
	if got := Titles(keys); got[keys[0]] != "Renamed Desktop session" || got[keys[1]] != "Renamed Desktop session" {
		t.Fatalf("a half-written file lost its names: %v", got)
	}
	writeDesktopTitle(t, path, `{"cliSessionId":"`+current+`","title":"Recovered"}`)
	if got := Titles(keys)[keys[0]]; got != "Recovered" {
		t.Fatalf("metadata repair: %q", got)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if got := Titles(keys)[keys[0]]; got != "" {
		t.Fatalf("deleted metadata retained its name: %q", got)
	}
}

func TestDesktopSessionTitleIdentity(t *testing.T) {
	setup(t)
	data, threep := t.TempDir(), t.TempDir()
	old := callDesktopDirs
	callDesktopDirs = func() []string { return []string{data, threep} }
	held := desktopTitlesHeld
	desktopTitlesHeld = 0
	t.Cleanup(func() { callDesktopDirs, desktopTitlesHeld = old, held; Reset() })
	for i, root := range []string{data, threep} {
		name := []string{"Original", "Fork"}[i]
		writeDesktopTitle(t, filepath.Join(root, "claude-code-sessions", "account", "org", "local_session.json"), `{"sessionId":"local_session","cliSessionId":"shared","title":"`+name+`","priorCliSessionIds":["old"],"forkedFromSessionId":"local_parent"}`)
	}
	if got := Titles([]string{"claude-desktop:shared", "claude-desktop:old", "claude-desktop:local_parent"}); len(got) != 0 {
		t.Fatalf("ambiguous or fork-parent IDs were named: %v", got)
	}
	// An explicit current CLI ID takes precedence over another session's old ID.
	writeDesktopTitle(t, filepath.Join(threep, "claude-code-sessions", "account", "org", "local_session.json"), `{"cliSessionId":"old","title":"Current owner"}`)
	if got := Titles([]string{"claude-desktop:old"})["claude-desktop:old"]; got != "Current owner" {
		t.Fatalf("prior ID overrode its current owner: %q", got)
	}
	writeDesktopTitle(t, filepath.Join(threep, "claude-code-sessions", "account", "org", "local_session.json"), `{"cliSessionId":"old","title":""}`)
	if got := Titles([]string{"claude-desktop:old"})["claude-desktop:old"]; got != "" {
		t.Fatalf("an unnamed current owner inherited another session's prior title: %q", got)
	}
}

// Desktop's other handles name the session too, as Desktop's own
// LocalSessionManager counts them: unarchivedCliSessionId is live like
// cliSessionId, preClearCliSessionId is lineage like priorCliSessionIds, and
// a record with no cliSessionId runs under its sessionId without "local_".
func TestDesktopSessionTitleHandles(t *testing.T) {
	setup(t)
	data := t.TempDir()
	old, held := callDesktopDirs, desktopTitlesHeld
	callDesktopDirs, desktopTitlesHeld = func() []string { return []string{data} }, 0
	t.Cleanup(func() { callDesktopDirs, desktopTitlesHeld = old, held; Reset() })
	dir := filepath.Join(data, "claude-code-sessions", "account", "org")
	writeDesktopTitle(t, filepath.Join(dir, "local_a.json"), `{"sessionId":"local_a","cliSessionId":"a-live","unarchivedCliSessionId":"a-unarchived","preClearCliSessionId":"a-cleared","title":"Handles"}`)
	writeDesktopTitle(t, filepath.Join(dir, "local_b0000000-0000-4000-8000-000000000000.json"), `{"sessionId":"local_b0000000-0000-4000-8000-000000000000","title":"No CLI id yet"}`)
	want := map[string]string{"a-live": "Handles", "a-unarchived": "Handles", "a-cleared": "Handles", "a": "Handles",
		"b0000000-0000-4000-8000-000000000000": "No CLI id yet"}
	ids := []string{"a-live", "a-unarchived", "a-cleared", "a", "b0000000-0000-4000-8000-000000000000", "local_a"}
	if got := DesktopTitles(ids); !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

// The Sessions page's stats summaries name a gateway-only Desktop session
// the same as its list does.
func TestDesktopSessionTitleInStats(t *testing.T) {
	setup(t)
	data := t.TempDir()
	old, held := callDesktopDirs, desktopTitlesHeld
	callDesktopDirs, desktopTitlesHeld = func() []string { return []string{data} }, 0
	t.Cleanup(func() { callDesktopDirs, desktopTitlesHeld = old, held; Reset() })
	writeDesktopTitle(t, filepath.Join(data, "local-agent-mode-sessions", "account", "org", "local_s.json"), `{"sessionId":"local_s","cliSessionId":"cli-s","title":"Cowork plan"}`)
	st := MergeExternalStats(Stats{}, nil, []ExternalSession{{Agent: "claude-desktop", ID: "cli-s", Title: "Recorded prompt"}, {Agent: "claude-desktop", ID: "other", Title: "Recorded prompt"}})
	got := map[string]string{}
	for _, s := range st.AllSessions() {
		got[s.ID] = s.Title
	}
	if got["cli-s"] != "Cowork plan" || got["other"] != "Recorded prompt" {
		t.Fatalf("stats titles %v", got)
	}
}

// A list of many native Desktop sessions looks at Desktop's metadata files
// once, not once per session: assemble asks for each session's name.
func TestDesktopTitlesReadOncePerList(t *testing.T) {
	setup(t)
	data := t.TempDir()
	old, held := callDesktopDirs, desktopTitlesHeld
	callDesktopDirs, desktopTitlesHeld = func() []string { return []string{data} }, time.Minute
	t.Cleanup(func() { callDesktopDirs, desktopTitlesHeld = old, held; Reset() })
	const n = 12
	for i := range n {
		id := fmt.Sprintf("cccccccc-0000-4000-8000-%012d", i)
		home := filepath.Join(data, "local-agent-mode-sessions", "account", "org", fmt.Sprintf("local_%d", i))
		writeDesktopTitle(t, home+".json", fmt.Sprintf(`{"sessionId":"local_%d","cliSessionId":"%s","title":"Cowork %d"}`, i, id, i))
		writeDesktopTitle(t, filepath.Join(home, ".claude", "projects", "-work", id+".jsonl"),
			`{"parentUuid":null,"isSidechain":false,"type":"user","message":{"role":"user","content":"hi"},"uuid":"u","timestamp":"2026-09-20T10:00:00.000Z","cwd":"/work","sessionId":"`+id+`"}`+"\n")
	}
	Reset()
	desktopTitles.Lock()
	before := desktopTitles.scans
	desktopTitles.Unlock()
	named := 0
	for _, s := range List(All) {
		if s.Agent == "claude-desktop" && strings.HasPrefix(s.Title, "Cowork ") {
			named++
		}
	}
	desktopTitles.Lock()
	scans := desktopTitles.scans - before
	desktopTitles.Unlock()
	if named != n || scans != 1 {
		t.Fatalf("%d of %d sessions named, metadata looked at %d times, want once", named, n, scans)
	}
}

func writeDesktopTitle(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

package sessions

import (
	"bytes"
	"database/sql"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// cxRollout writes a Codex rollout of the thread id on the provider p, as
// Codex 0.162 starts one, with the time at.
func cxRollout(t *testing.T, home, sub, id, p string, at time.Time) string {
	t.Helper()
	dir := filepath.Join(home, sub, "2026", "10", "21")
	if sub == "archived_sessions" {
		dir = filepath.Join(home, sub)
	}
	os.MkdirAll(dir, 0o755)
	path := filepath.Join(dir, "rollout-2026-10-21T10-00-00-"+id+".jsonl")
	b := `{"timestamp":"2026-10-21T10:00:00.000Z","type":"session_meta","payload":{"id":"` + id + `","timestamp":"2026-10-21T10:00:00.000Z","cwd":"/work/proj","originator":"codex_cli_rs","cli_version":"0.162.1","source":"cli","model_provider":"` + p + `","base_instructions":{"text":"x"}}}` + "\n" +
		`{"timestamp":"2026-10-21T10:00:01.000Z","type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"remember: the build uses make"}]}}` + "\n"
	if err := os.WriteFile(path, []byte(b), 0o644); err != nil {
		t.Fatal(err)
	}
	os.Chtimes(path, at, at)
	return path
}

func cxThreadRows(t *testing.T, home string, rows map[string]string) string {
	t.Helper()
	p := filepath.Join(home, "state_5.sqlite")
	db, err := sql.Open("sqlite", p)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`CREATE TABLE threads (id TEXT PRIMARY KEY, rollout_path TEXT NOT NULL, model_provider TEXT NOT NULL, title TEXT NOT NULL, archived INTEGER NOT NULL DEFAULT 0)`); err != nil {
		t.Fatal(err)
	}
	for id, prov := range rows {
		if _, err := db.Exec(`INSERT INTO threads VALUES (?, 'x', ?, 't', 0)`, id, prov); err != nil {
			t.Fatal(err)
		}
	}
	return p
}

// Codex lists only the threads of the provider it is on now, so the threads
// started while uni-switch was its provider ("uni-switch") were gone from Codex
// once it was off uni-switch (kaizaoji on X). MoveCodexThreads moves every
// thread on one provider to another, in its files and its row, a copy of
// each kept: an archived one too; one Codex wrote to in the last minute
// waits; a thread of another provider (CC Switch's "custom") stays.
func TestMoveCodexThreads(t *testing.T) {
	_, home := manageSetup(t)
	const (
		old      = "019a0000-0000-7000-8000-00000000000a"
		archived = "019a0000-0000-7000-8000-00000000000b"
		hot      = "019a0000-0000-7000-8000-00000000000c"
		theirs   = "019a0000-0000-7000-8000-00000000000d"
	)
	day := time.Now().Add(-24 * time.Hour).Truncate(time.Second)
	oldPath := cxRollout(t, home, "sessions", old, "uni-switch", day)
	arPath := cxRollout(t, home, "archived_sessions", archived, "uni-switch", day)
	hotPath := cxRollout(t, home, "sessions", hot, "uni-switch", time.Now())
	theirPath := cxRollout(t, home, "sessions", theirs, "custom", day)
	db := cxThreadRows(t, home, map[string]string{old: "uni-switch", archived: "uni-switch", hot: "uni-switch", theirs: "custom"})

	moved, err := MoveCodexThreads(home, "uni-switch", "openai")
	if err != nil {
		t.Fatal(err)
	}
	if len(moved) != 2 {
		t.Fatalf("moved %+v, want the two not written to lately", moved)
	}
	for _, mv := range moved {
		if mv.From != "uni-switch" || mv.To != "openai" || len(mv.DB) != 1 || mv.Backup == "" {
			t.Fatalf("moved %+v", mv)
		}
		if _, err := os.Stat(filepath.Join(mv.Backup, "move.json")); err != nil {
			t.Fatalf("no copy kept: %v", err)
		}
	}
	want := map[string]string{oldPath: "openai", arPath: "openai", hotPath: "uni-switch", theirPath: "custom"}
	for p, prov := range want {
		if got := codexMetaProvider(p); got != prov {
			t.Errorf("%s: file on %q, want %q", filepath.Base(p), got, prov)
		}
		if fi, _ := os.Stat(p); p != hotPath && !fi.ModTime().Equal(day) {
			t.Errorf("%s: time %v, want %v kept", filepath.Base(p), fi.ModTime(), day)
		}
	}
	for id, prov := range map[string]string{old: "openai", archived: "openai", hot: "uni-switch", theirs: "custom"} {
		if got, _, _ := threadProvider(t, db, id); got != prov {
			t.Errorf("row %s on %q, want %q", id, got, prov)
		}
	}
	b, _ := os.ReadFile(oldPath)
	if !strings.Contains(string(b), "remember: the build uses make") {
		t.Fatalf("the thread's messages went:\n%s", b)
	}

	// the one written to lately goes once Codex is done with it
	hour := time.Now().Add(-time.Hour)
	os.Chtimes(hotPath, hour, hour)
	if moved, err = MoveCodexThreads(home, "uni-switch", "openai"); err != nil || len(moved) != 1 || moved[0].ID != hot {
		t.Fatalf("again: %+v %v", moved, err)
	}
	if moved, err = MoveCodexThreads(home, "uni-switch", "openai"); err != nil || len(moved) != 0 {
		t.Fatalf("a third time: %+v %v", moved, err)
	}
}

// A Codex folder with no state database, or none of its threads on the
// provider, is left as it is, with nothing kept in the trash.
func TestMoveCodexThreadsNone(t *testing.T) {
	_, home := manageSetup(t)
	day := time.Now().Add(-24 * time.Hour)
	p := cxRollout(t, home, "sessions", "019a0000-0000-7000-8000-00000000000e", "uni-switch", day)
	if moved, err := MoveCodexThreads(home, "uni-switch", "openai"); err != nil || len(moved) != 0 {
		t.Fatalf("no database: %+v %v", moved, err)
	}
	if got := codexMetaProvider(p); got != "uni-switch" {
		t.Fatalf("file on %q", got)
	}
	if _, err := MoveCodexThreads(home, "uni-switch", "uni-switch"); err == nil {
		t.Fatal("a move to the same provider went")
	}
}

// bringBack's move (MoveCodexThreads) leaves a thread a Codex has open as
// it leaves one written to in the last minute (#3751): no error, its
// files and row on the provider they were, no copy kept. The others move,
// and that one does once its Codex has let go. A Codex home without
// thread-writer-locks (a Codex from before them) has none to look at: its
// threads move as before, and none is made there.
func TestMoveCodexThreadsLeavesAnOpenThread(t *testing.T) {
	_, home := manageSetup(t)
	const (
		free = "019a0000-0000-7000-8000-0000000000a1"
		open = "019a0000-0000-7000-8000-0000000000a2"
	)
	day := time.Now().Add(-24 * time.Hour).Truncate(time.Second)
	cxRollout(t, home, "sessions", free, "uni-switch", day)
	openPath := cxRollout(t, home, "sessions", open, "uni-switch", day)
	db := cxThreadRows(t, home, map[string]string{free: "uni-switch", open: "uni-switch"})
	orig, _ := os.ReadFile(openPath)
	_, letGo := holdThread(t, home, open)

	moved, err := MoveCodexThreads(home, "uni-switch", "openai")
	if err != nil || len(moved) != 1 || moved[0].ID != free {
		t.Fatalf("moved %+v, %v; want %s alone", moved, err, free)
	}
	if b, _ := os.ReadFile(openPath); !bytes.Equal(b, orig) {
		t.Fatal("moved while a Codex had it open")
	}
	if got, _, _ := threadProvider(t, db, open); got != "uni-switch" {
		t.Errorf("its row moved to %q, its files didn't", got)
	}
	if kept := providerTrash(t); len(kept) != 1 {
		t.Errorf("copies kept: %v, want the moved thread's alone", kept)
	}

	letGo()
	if moved, err = MoveCodexThreads(home, "uni-switch", "openai"); err != nil || len(moved) != 1 || moved[0].ID != open {
		t.Fatalf("after its Codex let go: %+v %v", moved, err)
	}
	if got := codexMetaProvider(openPath); got != "openai" {
		t.Errorf("file on %q", got)
	}
	if got, _, _ := threadProvider(t, db, open); got != "openai" {
		t.Errorf("row on %q", got)
	}

	old := filepath.Join(t.TempDir(), ".codex")
	p := cxRollout(t, old, "sessions", free, "uni-switch", day)
	cxThreadRows(t, old, map[string]string{free: "uni-switch"})
	if moved, err := MoveCodexThreads(old, "uni-switch", "openai"); err != nil || len(moved) != 1 {
		t.Fatalf("a home without writer locks: %+v %v", moved, err)
	}
	if got := codexMetaProvider(p); got != "openai" {
		t.Errorf("file on %q in a home without writer locks", got)
	}
	if _, err := os.Stat(filepath.Join(old, "thread-writer-locks")); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("thread-writer-locks made where Codex keeps none: %v", err)
	}
}

package sessions

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// cursorTempDir gives the test a temp folder of its own: TMPDIR is
// os.TempDir on Unix, TMP and TEMP on Windows, where a TMPDIR alone left
// TestCursorLeftCopiesSwept's copy in the real temp folder for the next
// test to count.
func cursorTempDir(t *testing.T) {
	t.Helper()
	d := t.TempDir()
	for _, k := range []string{"TMPDIR", "TMP", "TEMP"} {
		t.Setenv(k, d)
	}
}

func cursorTempCopies(t *testing.T) []string {
	t.Helper()
	d, _ := filepath.Glob(filepath.Join(os.TempDir(), "uni-switch-cursor-*"))
	return d
}

// cursorLiveEditor writes the editor's state.vscdb with six chats, as a
// running Cursor holds it: in WAL mode, with its rows still in the -wal and
// the -shm beside it. put writes a row, as Cursor does, until the test ends.
func cursorLiveEditor(t *testing.T) (put func(string, any), ids []string, at time.Time) {
	t.Helper()
	config, err := os.UserConfigDir()
	if err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(config, "Cursor", "User", "globalStorage", "state.vscdb")
	if err := os.MkdirAll(filepath.Dir(p), 0700); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", p)
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { db.Close() })
	for _, q := range []string{"PRAGMA journal_mode=WAL", "PRAGMA wal_autocheckpoint=0", "CREATE TABLE cursorDiskKV (key TEXT PRIMARY KEY, value BLOB)"} {
		if _, err := db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	put = func(k string, v any) {
		b, _ := json.Marshal(v)
		if _, err := db.Exec("INSERT OR REPLACE INTO cursorDiskKV VALUES (?,?)", k, b); err != nil {
			t.Fatal(err)
		}
	}
	at = time.Now().Add(-time.Hour)
	for i := range 6 {
		id := fmt.Sprintf("0000000%d-1111-2222-3333-444444444444", i)
		ids = append(ids, id)
		put("composerData:"+id, map[string]any{"composerId": id, "name": "Chat " + id[:8], "createdAt": at.UnixMilli(), "lastUpdatedAt": at.Add(time.Minute).UnixMilli(), "fullConversationHeadersOnly": []any{map[string]any{"bubbleId": "u"}, map[string]any{"bubbleId": "a"}}})
		put("bubbleId:"+id+":u", map[string]any{"type": 1, "text": "question " + id, "createdAt": at.Format(time.RFC3339Nano)})
		put("bubbleId:"+id+":a", map[string]any{"type": 2, "text": "answer", "createdAt": at.Add(time.Minute).Format(time.RFC3339Nano)})
	}
	if fi, err := os.Stat(p + "-wal"); err != nil || fi.Size() == 0 {
		t.Fatalf("fixture has no live -wal: %v", err)
	}
	if _, err := os.Stat(p + "-shm"); err != nil {
		t.Fatalf("fixture has no -shm: %v", err)
	}
	return put, ids, at
}

// #1988: Cursor's state.vscdb is often several GB. Every listing, count,
// transcript and search read it from a fresh copy in the temp folder, so a
// visit to Sessions wrote it out again and again. Reads of an unchanged
// store share one copy, a change makes one new copy, and nothing is left.
func TestCursorEditorCopiedOncePerChange(t *testing.T) {
	cursorSetup(t)
	cursorTempDir(t)
	// The copy lingers past any pause a loaded runner makes between two
	// reads while copies are counted (a 100ms linger let a slow runner drop
	// it between List and the next visit, and count a third copy); it is
	// cut short only to see the last one removed. The timers read the
	// linger under cursorSnaps' lock, so it is set under it.
	linger := func(d time.Duration) {
		cursorSnaps.Lock()
		cursorSnapLinger = d
		cursorSnaps.Unlock()
	}
	minWas, lingerWas := cursorSnapMin, cursorSnapLinger
	cursorSnapMin = 0
	linger(time.Hour)
	t.Cleanup(func() { cursorSnapMin = minWas; linger(lingerWas) })

	// the copy path: a store with a -wal no running Cursor shares
	inPlaceWas := cursorReadInPlace
	cursorReadInPlace = false
	t.Cleanup(func() { cursorReadInPlace = inPlaceWas })

	put, ids, at := cursorLiveEditor(t)

	// a visit to Sessions: the list, the counts, each chat, a search
	visit := func() {
		ss := List(All)
		Agents()
		for _, id := range ids {
			s, ok := findSession(ss, "cursor", id)
			if !ok {
				t.Fatalf("chat %s not listed", id)
			}
			if tr, err := TranscriptOf(s); err != nil || len(tr.Parts) != 2 {
				t.Fatalf("transcript %+v: %v", tr, err)
			}
			if _, _, err := SearchSession(context.Background(), s, "nothing like this"); err != nil {
				t.Fatal(err)
			}
		}
	}
	before := cursorCopies.Load()
	visit()
	visit()
	if n := cursorCopies.Load() - before; n != 1 {
		t.Fatalf("an unchanged state.vscdb was copied %d times, want 1", n)
	}

	put("composerData:"+ids[0], map[string]any{"composerId": ids[0], "name": "Renamed", "createdAt": at.UnixMilli(), "lastUpdatedAt": at.Add(2 * time.Minute).UnixMilli(), "fullConversationHeadersOnly": []any{map[string]any{"bubbleId": "u"}, map[string]any{"bubbleId": "a"}}})
	if s := find(t, List(All), "cursor", ids[0]); s.Title != "Renamed" {
		t.Fatalf("a change to the store wasn't read: %q", s.Title)
	}
	visit()
	if n := cursorCopies.Load() - before; n != 2 {
		t.Fatalf("copies after one change: %d, want 2", n)
	}

	// one more read lets the copy go with a short linger
	linger(100 * time.Millisecond)
	List(All)

	deadline := time.Now().Add(5 * time.Second)
	for len(cursorTempCopies(t)) > 0 {
		if time.Now().After(deadline) {
			t.Fatalf("copies left in the temp folder: %v", cursorTempCopies(t))
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// #1988 (Echo-Huang0010): beside a running Cursor, a visit to Sessions
// copied a ~10.6 GB state.vscdb to the temp folder each time Cursor had
// written since the last copy. The store is read where it is, as one more
// of the readers Cursor's own processes are: nothing is copied, a row
// Cursor writes is read at once, and Cursor can still write after it. A
// copy an older uni-switch left is still swept.
func TestCursorEditorReadInPlaceWhileLive(t *testing.T) {
	cursorSetup(t)
	cursorTempDir(t)
	minWas := cursorSnapMin
	cursorSnapMin = 0
	t.Cleanup(func() { cursorSnapMin = minWas })

	// a copy an older uni-switch left, from before reads were in place
	left := filepath.Join(os.TempDir(), "uni-switch-cursor-left")
	if err := os.MkdirAll(left, 0700); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-2 * cursorSnapStale)
	if err := os.Chtimes(left, old, old); err != nil {
		t.Fatal(err)
	}
	cursorSnaps.sweep = sync.Once{}

	put, ids, at := cursorLiveEditor(t)
	copies, inPlace := cursorCopies.Load(), cursorInPlace.Load()
	for round := range 3 {
		name := fmt.Sprintf("Round %d", round)
		put("composerData:"+ids[0], map[string]any{"composerId": ids[0], "name": name, "createdAt": at.UnixMilli(), "lastUpdatedAt": at.Add(time.Duration(round+2) * time.Minute).UnixMilli(), "fullConversationHeadersOnly": []any{map[string]any{"bubbleId": "u"}, map[string]any{"bubbleId": "a"}}})
		ss := List(All)
		if s := find(t, ss, "cursor", ids[0]); s.Title != name {
			t.Fatalf("round %d: Cursor's write wasn't read: %q", round, s.Title)
		}
		Agents()
		for _, id := range ids {
			s := find(t, ss, "cursor", id)
			if tr, err := TranscriptOf(s); err != nil || len(tr.Parts) != 2 {
				t.Fatalf("transcript %+v: %v", tr, err)
			}
			if _, _, err := SearchSession(context.Background(), s, "nothing like this"); err != nil {
				t.Fatal(err)
			}
		}
	}
	if n := cursorCopies.Load() - copies; n != 0 {
		t.Fatalf("a live state.vscdb was copied %d times, want 0", n)
	}
	if cursorInPlace.Load() == inPlace {
		t.Fatal("the store was never opened in place")
	}
	if left := cursorTempCopies(t); len(left) > 0 {
		t.Fatalf("copies in the temp folder: %v", left)
	}
}

// A copy left by a uni-switch that was killed mid-read is removed; one a
// running uni-switch made a moment ago is not.
func TestCursorLeftCopiesSwept(t *testing.T) {
	cursorTempDir(t)
	left := filepath.Join(os.TempDir(), "uni-switch-cursor-left")
	fresh := filepath.Join(os.TempDir(), "uni-switch-cursor-fresh")
	for _, d := range []string{left, fresh} {
		if err := os.MkdirAll(d, 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(d, "store.db"), make([]byte, 1024), 0600); err != nil {
			t.Fatal(err)
		}
	}
	old := time.Now().Add(-2 * cursorSnapStale)
	if err := os.Chtimes(left, old, old); err != nil {
		t.Fatal(err)
	}
	sweepCursorSnaps()
	if _, err := os.Stat(left); !os.IsNotExist(err) {
		t.Fatalf("a left copy is still there: %v", err)
	}
	if _, err := os.Stat(fresh); err != nil {
		t.Fatalf("a copy in use was removed: %v", err)
	}
}

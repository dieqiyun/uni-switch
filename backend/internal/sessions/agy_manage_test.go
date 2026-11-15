package sessions

import (
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

// agyConversation writes the files agy keeps of conversation id beside its
// brain folder, shaped as a real agy 1.4.12 left them: its trajectory
// database (SQLite, with the -wal and -shm agy leaves while it has it
// open), its annotations (title:"…" text), and its presence lock (empty).
func agyConversation(t *testing.T, dir, id string) []string {
	t.Helper()
	files := map[string]string{
		filepath.Join("conversations", id+".db"):     "SQLite format 3\x00",
		filepath.Join("conversations", id+".db-wal"): "",
		filepath.Join("conversations", id+".db-shm"): "\x00\x00",
		filepath.Join("annotations", id+".pbtxt"):    `title:"Hello there."`,
		filepath.Join("presence", id+".lock"):        "",
	}
	var out []string
	for rel, body := range files {
		p := filepath.Join(dir, rel)
		os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		out = append(out, p)
	}
	return out
}

// ysm119 on Discord: agy's sessions couldn't be deleted on the Sessions
// page, Codex's could. A conversation goes to the trash as agy's own
// delete (its /resume picker's) removes it: its brain folder, its
// conversations/<id>.db with SQLite's files beside it, and its
// annotations; nothing of another conversation's, and not the summaries
// agy keeps open, whose row agy doesn't list once the files are gone.
// Restore puts them back.
func TestAgyDeleteRestore(t *testing.T) {
	dir := putAgy(t)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(t.TempDir(), "config"))
	agyConversation(t, dir, agyTwoTurns)
	others := agyConversation(t, dir, agyRead)
	others = append(others, filepath.Join(dir, "brain", agyRead), filepath.Join(dir, "conversation_summaries.db"))
	ageTree(dir)
	Reset()

	if !Deletable("agy") {
		t.Fatal("agy not deletable")
	}
	m, ok := findManaged(ListAgent("agy"), agyTwoTurns)
	if !ok || !m.Deletable {
		t.Fatalf("listed as %+v", m)
	}
	conv := filepath.Join(dir, "conversations", agyTwoTurns+".db")
	mine := []string{
		filepath.Join(dir, "brain", agyTwoTurns), conv, conv + "-wal", conv + "-shm",
		filepath.Join(dir, "annotations", agyTwoTurns+".pbtxt"),
	}

	// one being written to is left alone
	now := time.Now()
	os.Chtimes(conv+"-wal", now, now)
	if _, err := Delete("agy", agyTwoTurns); !errors.Is(err, ErrActive) {
		t.Fatalf("a conversation just written: %v", err)
	}
	ageTree(dir)

	// and so is one a running agy has open, however long since it wrote
	if runtime.GOOS != "windows" {
		lock, err := os.Open(filepath.Join(dir, "presence", agyTwoTurns+".lock"))
		if err != nil {
			t.Fatal(err)
		}
		if err := syscallFlock(lock); err != nil {
			t.Fatal(err)
		}
		_, err = Delete("agy", agyTwoTurns)
		lock.Close()
		if !errors.Is(err, ErrActive) {
			t.Fatalf("a conversation agy has open: %v", err)
		}
	}

	tr, err := Delete("agy", agyTwoTurns)
	if err != nil {
		t.Fatal(err)
	}
	if len(tr.Items) != len(mine) {
		t.Fatalf("trashed %+v, want %d items", tr.Items, len(mine))
	}
	for _, p := range mine {
		if _, err := os.Lstat(p); err == nil {
			t.Fatalf("%s is still there", p)
		}
	}
	for _, p := range others {
		if _, err := os.Stat(p); err != nil {
			t.Fatalf("another's %s: %v", p, err)
		}
	}
	Reset()
	if _, ok := findManaged(ListAgent("agy"), agyTwoTurns); ok {
		t.Fatal("still listed after the delete")
	}

	if _, err := Restore(tr.Key); err != nil {
		t.Fatal(err)
	}
	for _, p := range mine {
		if _, err := os.Stat(p); err != nil {
			t.Fatalf("%s isn't back: %v", p, err)
		}
	}
	Reset()
	if back, ok := findManaged(ListAgent("agy"), agyTwoTurns); !ok || back.Title != m.Title {
		t.Fatalf("the restored conversation: %+v", back)
	}
}

// A conversation listed from its row alone (no transcript, #1718) is
// deleted by its conversations/<id>.db, and its row then lists nothing.
func TestAgyDeleteRowOnly(t *testing.T) {
	dir := putAgy(t)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(t.TempDir(), "config"))
	const id = "7d1e2f3a-4b5c-4d6e-8f90-a1b2c3d4e5f6"
	db, err := sql.Open("sqlite", filepath.Join(dir, "conversation_summaries.db"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO conversation_summaries (conversation_id, title, last_modified_time, workspace_uris, last_user_input_time) VALUES (?, 'Older talk', '2026-10-12 04:41:53.5+00:00', '["file:///work/old"]', '2026-10-12 04:41:53.5+00:00')`, id); err != nil {
		t.Fatal(err)
	}
	db.Close()
	agyConversation(t, dir, id)
	ageTree(dir)
	Reset()

	if m, ok := findManaged(ListAgent("agy"), id); !ok || !m.Deletable {
		t.Fatalf("listed as %+v", m)
	}
	tr, err := Delete("agy", id)
	if err != nil {
		t.Fatal(err)
	}
	if len(tr.Items) != 4 {
		t.Fatalf("trashed %+v, want its database, -wal, -shm and annotations", tr.Items)
	}
	Reset()
	if _, ok := findManaged(ListAgent("agy"), id); ok {
		t.Fatal("its row is still listed")
	}
	if _, ok := findManaged(ListAgent("agy"), agyTwoTurns); !ok {
		t.Fatal("another conversation went with it")
	}
}

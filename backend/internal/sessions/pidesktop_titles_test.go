package sessions

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// piDesktopSchema is the sessions table as PI-Desktop's host-core makes it
// (crates/host-core/src/db/schema.rs).
const piDesktopSchema = `CREATE TABLE sessions (
  id          TEXT PRIMARY KEY,
  title       TEXT NOT NULL DEFAULT '',
  project_id  INTEGER,
  provider_id TEXT,
  model_id    TEXT,
  mode        TEXT NOT NULL DEFAULT 'agent',
  title_source TEXT NOT NULL DEFAULT 'legacy'
               CHECK (title_source IN ('legacy', 'default', 'manual', 'generated')),
  source      TEXT,
  deleted_at  INTEGER,
  pinned      INTEGER NOT NULL DEFAULT 0,
  last_seq    INTEGER NOT NULL DEFAULT 0,
  created_at  INTEGER NOT NULL,
  updated_at  INTEGER NOT NULL
)`

// A chat PI-Desktop sends through uni-switch (X-Uni-Switch-Session: its id) goes
// by the title PI-Desktop keeps in pi.sqlite, as Pi's or as PI-Desktop's
// own agent, and not by its id (#1967, dgmico) — read with the title still
// in the write-ahead log, as a running PI-Desktop leaves it, without a
// byte written in PI-Desktop's folder.
func TestPIDesktopTitlesNameGatewaySessions(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("PI_DESKTOP_DATA_DIR", dir)
	held := piDesktopTitlesHeld
	piDesktopTitlesHeld = 0
	t.Cleanup(func() { piDesktopTitlesHeld = held; piDesktopTitles.names, piDesktopTitles.stamp = nil, "" })

	path := filepath.Join(dir, "pi.sqlite")
	db, err := sql.Open("sqlite", "file:"+filepath.ToSlash(path))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1) // one connection, so the log stays open
	for _, q := range []string{"PRAGMA journal_mode=WAL", "PRAGMA wal_autocheckpoint=0", piDesktopSchema,
		`INSERT INTO sessions (id, title, title_source, created_at, updated_at) VALUES ('3970a74b-bc9e-4379-a228-38474a2b6871', '你好啊', 'generated', 1, 2)`,
		`INSERT INTO sessions (id, title, title_source, deleted_at, created_at, updated_at) VALUES ('gone-0001', 'Deleted chat', 'manual', 5, 1, 2)`,
		`INSERT INTO sessions (id, title, title_source, created_at, updated_at) VALUES ('blank-0001', '', 'default', 1, 2)`,
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	if fi, err := os.Stat(path + "-wal"); err != nil || fi.Size() == 0 {
		t.Fatalf("the titles should be in the log, as a running PI-Desktop has them: %v", err)
	}
	before := folderState(t, dir)

	const id = "3970a74b-bc9e-4379-a228-38474a2b6871"
	got := MergeExternal(nil, []ExternalSession{
		{Agent: "pi", ID: id, Last: time.Unix(3, 0)},
		{Agent: "pi-desktop", ID: id, Last: time.Unix(2, 0)},
		{Agent: "claude", ID: id, Last: time.Unix(1, 0)},
		{Agent: "pi", ID: "gone-0001"},
		{Agent: "pi", ID: "blank-0001", Title: "what it was first asked"},
	})
	want := map[string]string{"pi|" + id: "你好啊", "pi-desktop|" + id: "你好啊", "claude|" + id: id, "pi|gone-0001": "gone-0001", "pi|blank-0001": "what it was first asked"}
	for _, s := range got {
		if w := want[s.Agent+"|"+s.ID]; s.Title != w {
			t.Errorf("%s|%s titled %q, want %q", s.Agent, s.ID, s.Title, w)
		}
	}
	st := MergeExternalStats(Stats{}, nil, []ExternalSession{{Agent: "pi-desktop", ID: id, Last: time.Now()}})
	if all := st.AllSessions(); len(all) != 1 || all[0].Title != "你好啊" {
		t.Errorf("stats sessions %+v", all)
	}
	if after := folderState(t, dir); after != before {
		t.Errorf("PI-Desktop's folder changed:\n%s\n→\n%s", before, after)
	}

	// renamed in PI-Desktop: the next read has the new name
	if _, err := db.Exec(`UPDATE sessions SET title = '改过的名字', title_source = 'manual' WHERE id = ?`, id); err != nil {
		t.Fatal(err)
	}
	if got := PIDesktopTitles([]string{id})[id]; got != "改过的名字" {
		t.Errorf("after the rename %q", got)
	}

	// The Routing page asks Titles for agent:id keys (#1293); PI-Desktop's
	// name has to reach pi-desktop:id and pi:id, and nothing else (#1967).
	keys := []string{"pi-desktop:" + id, "pi:" + id, "claude:" + id, "pi:gone-0001", "pi:blank-0001", "codex:" + id}
	titles := Titles(keys)
	for _, k := range []string{"pi-desktop:" + id, "pi:" + id} {
		if titles[k] != "改过的名字" {
			t.Errorf("Titles[%q] = %q, want the PI-Desktop name", k, titles[k])
		}
	}
	for _, k := range []string{"claude:" + id, "pi:gone-0001", "pi:blank-0001", "codex:" + id} {
		if v, ok := titles[k]; ok {
			t.Errorf("Titles[%q] = %q, want no PI-Desktop name", k, v)
		}
	}
}

// folderState is each file of dir with its size and time.
func folderState(t *testing.T, dir string) string {
	t.Helper()
	es, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var s string
	for _, e := range es {
		fi, err := e.Info()
		if err != nil {
			t.Fatal(err)
		}
		s += e.Name() + " " + fi.ModTime().String() + " " + itoa(int(fi.Size())) + "\n"
	}
	return s
}

package sessions

import (
	"bytes"
	"database/sql"
	"database/sql/driver"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/klauspost/compress/zstd"
	"modernc.org/sqlite"

	"github.com/dieqiyun/uni-switch/backend/internal/filelock"
	"github.com/dieqiyun/uni-switch/backend/internal/settings"
)

// codexState makes a Codex state database with a thread's row, as Codex
// 0.159's state_5.sqlite has it (the columns that matter).
func codexStateDB(t *testing.T, home, id, provider string) string {
	t.Helper()
	p := filepath.Join(home, "state_5.sqlite")
	db, err := sql.Open("sqlite", p)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`CREATE TABLE threads (id TEXT PRIMARY KEY, rollout_path TEXT NOT NULL, model_provider TEXT NOT NULL, title TEXT NOT NULL, archived INTEGER NOT NULL DEFAULT 0);
		CREATE INDEX idx_threads_provider ON threads(model_provider);
		INSERT INTO threads VALUES (?, 'x', ?, 'the title', 1), ('other', 'y', ?, 'another', 0)`, id, provider, provider); err != nil {
		t.Fatal(err)
	}
	return p
}

func threadProvider(t *testing.T, db, id string) (provider, title string, archived int) {
	t.Helper()
	d, err := sql.Open("sqlite", db)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	if err := d.QueryRow(`SELECT model_provider, title, archived FROM threads WHERE id = ?`, id).Scan(&provider, &title, &archived); err != nil {
		t.Fatal(err)
	}
	return
}

// A Codex session made with another provider (CC Switch's "custom") is
// hidden from Codex's history once Codex uses uni-switch (#887). The Sessions
// page says which provider each was made with and which Codex uses now;
// MoveCodexProvider changes the provider in its files and in Codex's state
// database and nothing else, keeps a copy of the files first, and moving
// it back gives the files it had, byte for byte.
func TestMoveCodexProvider(t *testing.T) {
	_, codex := manageSetup(t)
	main := filepath.Join(codex, "sessions", "2026", "09", "20", "rollout-2026-09-20T15-48-28-"+mgCX+".jsonl")
	b, err := os.ReadFile(main)
	if err != nil {
		t.Fatal(err)
	}
	orig := bytes.Replace(b, []byte(`"model_provider":"openai"`), []byte(`"model_provider":"custom"`), 1)
	old := time.Now().Add(-24 * time.Hour).Truncate(time.Second)
	os.WriteFile(main, orig, 0o644)
	os.Chtimes(main, old, old)
	seg := filepath.Join(codex, "sessions", "2026", "09", "23", "rollout-2026-09-23T00-27-05-"+mgCX+"_01a0c9f1-3d6f-7f33-b3bd-25c1dd14251b.jsonl")
	segOrig, _ := os.ReadFile(seg)
	os.WriteFile(filepath.Join(codex, "config.toml"), []byte("model = \"gpt-6\"\nmodel_provider = \"uni-switch\"\n\n[model_providers.uni-switch]\nname = \"uni-switch\"\n"), 0o644)
	db := codexStateDB(t, codex, mgCX, "custom")

	m, ok := findManaged(ListAgent("codex"), mgCX)
	if !ok || m.Provider != "custom" || m.UsesProvider != "uni-switch" {
		t.Fatalf("listed %q, uses %q; want custom, uni-switch", m.Provider, m.UsesProvider)
	}

	mv, err := MoveCodexProvider(mgCX, "uni-switch")
	if err != nil {
		t.Fatal(err)
	}
	if mv.From != "custom" || mv.To != "uni-switch" || len(mv.Files) != 1 || mv.Files[0] != main || len(mv.DB) != 1 {
		t.Fatalf("moved %+v", mv)
	}
	now, _ := os.ReadFile(main)
	if want := bytes.Replace(orig, []byte(`"model_provider":"custom"`), []byte(`"model_provider":"uni-switch"`), 1); !bytes.Equal(now, want) {
		t.Fatalf("the file became\n%s", now)
	}
	if fi, _ := os.Stat(main); !fi.ModTime().Equal(old) {
		t.Errorf("its time became %v, want %v", fi.ModTime(), old)
	}
	if b, _ := os.ReadFile(seg); !bytes.Equal(b, segOrig) {
		t.Error("a segment naming no provider was changed")
	}
	if p, title, arch := threadProvider(t, db, mgCX); p != "uni-switch" || title != "the title" || arch != 1 {
		t.Errorf("thread row %q %q %d", p, title, arch)
	}
	if p, _, _ := threadProvider(t, db, "other"); p != "custom" {
		t.Errorf("another thread became %q", p)
	}
	if kept, _ := filepath.Glob(filepath.Join(mv.Backup, "files", "*")); len(kept) != 2 {
		t.Fatalf("backup %v", kept)
	} else if b, _ := os.ReadFile(filepath.Join(mv.Backup, "files", "0-"+filepath.Base(main))); !bytes.Equal(b, orig) {
		t.Error("the backup isn't the file as it was")
	}
	if !strings.HasPrefix(mv.Backup, os.Getenv("XDG_CONFIG_HOME")) || !strings.Contains(mv.Backup, filepath.Join("trash", "codex-provider")) {
		t.Errorf("backup in %s", mv.Backup)
	}
	if m, _ := findManaged(ListAgent("codex"), mgCX); m.Provider != "uni-switch" {
		t.Fatalf("listed as %q after the move", m.Provider)
	}

	// undo: back to custom, the file as it was
	if _, err := MoveCodexProvider(mgCX, "custom"); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(main); !bytes.Equal(b, orig) {
		t.Fatal("moving back didn't give the file it was")
	}
	if p, _, _ := threadProvider(t, db, mgCX); p != "custom" {
		t.Errorf("thread row %q after moving back", p)
	}

	// one being written is left alone
	hot := time.Now()
	os.Chtimes(main, hot, hot)
	if _, err := MoveCodexProvider(mgCX, "uni-switch"); !errors.Is(err, ErrActive) {
		t.Fatalf("err %v, want ErrActive", err)
	}
	if b, _ := os.ReadFile(main); !bytes.Equal(b, orig) {
		t.Fatal("a running session was changed")
	}
	if _, err := MoveCodexProvider(mgCX, "bad id"); err == nil {
		t.Fatal("a provider id with a space was taken")
	}
}

// A rollout the Codex app compressed (.jsonl.zst) is moved too, and stays
// compressed.
func TestMoveCodexProviderPacked(t *testing.T) {
	_, codex := manageSetup(t)
	main := filepath.Join(codex, "sessions", "2026", "09", "20", "rollout-2026-09-20T15-48-28-"+mgCX+".jsonl")
	b, _ := os.ReadFile(main)
	b = bytes.Replace(b, []byte(`"model_provider":"openai"`), []byte(`"model_provider":"custom"`), 1)
	enc, _ := zstd.NewWriter(nil)
	os.WriteFile(main+zstSuffix, enc.EncodeAll(b, nil), 0o644)
	os.Remove(main)
	old := time.Now().Add(-24 * time.Hour)
	os.Chtimes(main+zstSuffix, old, old)

	if m, _ := findManaged(ListAgent("codex"), mgCX); m.Provider != "custom" || m.UsesProvider != "openai" {
		t.Fatalf("listed %q, uses %q", m.Provider, m.UsesProvider)
	}
	mv, err := MoveCodexProvider(mgCX, "openai")
	if err != nil || len(mv.Files) != 1 {
		t.Fatalf("%+v %v", mv, err)
	}
	f, err := os.Open(main + zstSuffix)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	dec, _ := zstd.NewReader(f)
	defer dec.Close()
	var got bytes.Buffer
	if _, err := got.ReadFrom(dec); err != nil {
		t.Fatal(err)
	}
	if want := bytes.Replace(b, []byte(`"model_provider":"custom"`), []byte(`"model_provider":"openai"`), 1); !bytes.Equal(got.Bytes(), want) {
		t.Fatal("the compressed file wasn't moved")
	}
}

// Codex's provider is its profile's when config.toml chooses one.
func TestCodexProviderIn(t *testing.T) {
	d := t.TempDir()
	if p := CodexProviderIn(d); p != "openai" {
		t.Fatalf("no config: %q", p)
	}
	os.WriteFile(filepath.Join(d, "config.toml"), []byte("model_provider = \"custom\"\nprofile = \"m\"\n[profiles.m]\nmodel_provider = \"uni-switch\"\n"), 0o644)
	if p := CodexProviderIn(d); p != "uni-switch" {
		t.Fatalf("profile: %q", p)
	}
}

// Codex keeps places in a paginated rollout by byte (xybio on Discord, see
// TestPortCodexRolloutsKeepsBytePlaces): a move keeps the session_meta
// line's length, padded after its JSON for a shorter id, and a longer id
// is refused with the files as they were; moving back gives the bytes it
// had.
func TestMoveCodexProviderKeepsBytePlaces(t *testing.T) {
	_, codex := manageSetup(t)
	main := filepath.Join(codex, "sessions", "2026", "09", "20", "rollout-2026-09-20T15-48-28-"+mgCX+".jsonl")
	b, _ := os.ReadFile(main)
	orig := bytes.Replace(b, []byte(`"model_provider":"openai"`), []byte(`"model_provider":"custom","history_mode":"paginated"`), 1)
	old := time.Now().Add(-24 * time.Hour).Truncate(time.Second)
	os.WriteFile(main, orig, 0o644)
	os.Chtimes(main, old, old)

	if _, err := MoveCodexProvider(mgCX, "cc"); err != nil {
		t.Fatal(err)
	}
	now, _ := os.ReadFile(main)
	if len(now) != len(orig) {
		t.Fatalf("%d bytes, was %d", len(now), len(orig))
	}
	first := now[:bytes.IndexByte(now, '\n')]
	if !bytes.HasSuffix(first, []byte("}    ")) || !bytes.Contains(first, []byte(`"model_provider":"cc"`)) || !bytes.Equal(now[len(first):], orig[len(first):]) {
		t.Fatalf("the file became\n%s", now)
	}
	if _, err := MoveCodexProvider(mgCX, "a-longer-provider"); err == nil {
		t.Fatal("a longer id was written")
	}
	if b, _ := os.ReadFile(main); !bytes.Equal(b, now) {
		t.Fatal("a refused move changed the file")
	}
	if _, err := MoveCodexProvider(mgCX, "custom"); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(main); !bytes.Equal(b, orig) {
		t.Fatalf("moving back gave\n%s", b)
	}
}

// holdThread holds a thread's writer lock in a Codex home as a Codex that
// has the thread open does (the app-server after thread/resume, a resumed
// TUI): thread-writer-locks/<id>.lock, locked on a descriptor of its own.
// It gives the lock file's path and the Codex letting go, which leaves
// the file, as a Codex that ended leaves it.
func holdThread(t *testing.T, home, id string) (path string, letGo func()) {
	t.Helper()
	locks := filepath.Join(home, "thread-writer-locks")
	if err := os.MkdirAll(locks, 0o755); err != nil {
		t.Fatal(err)
	}
	path = filepath.Join(locks, id+".lock")
	lf, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := filelock.TryLock(lf); !got || err != nil {
		lf.Close()
		t.Fatalf("lock: %v %v", got, err)
	}
	var once sync.Once
	letGo = func() { once.Do(func() { filelock.Unlock(lf); lf.Close() }) }
	t.Cleanup(letGo)
	return path, letGo
}

// providerTrash is what uni-switch's trash keeps of Codex provider moves.
func providerTrash(t *testing.T) []string {
	t.Helper()
	kept, _ := filepath.Glob(filepath.Join(settings.Dir(), "trash", "codex-provider", "*"))
	return kept
}

// #3751: a Codex that has a thread open (the app-server after
// thread/resume, a resumed TUI) holds the thread's writer lock and keeps
// its rollout open to append to. A move put a new file in its place, and
// what that Codex wrote next went to the old one: a turn the upstream
// answered was gone from the rollout, and from what Codex sent after its
// next resume. A thread a Codex holds is left as it was, its files, time
// and row, with no copy kept (ErrActive: the Sessions page says to close
// it in Codex); once the Codex lets go it moves, and the lock file left
// behind goes, as Codex's own guard removes it.
func TestMoveCodexProviderLeavesAnOpenThread(t *testing.T) {
	_, codex := manageSetup(t)
	main := filepath.Join(codex, "sessions", "2026", "09", "20", "rollout-2026-09-20T15-48-28-"+mgCX+".jsonl")
	b, _ := os.ReadFile(main)
	orig := bytes.Replace(b, []byte(`"model_provider":"openai"`), []byte(`"model_provider":"custom"`), 1)
	old := time.Now().Add(-24 * time.Hour).Truncate(time.Second)
	os.WriteFile(main, orig, 0o644)
	os.Chtimes(main, old, old)
	db := codexStateDB(t, codex, mgCX, "custom")
	lock, letGo := holdThread(t, codex, mgCX)

	if _, err := MoveCodexProvider(mgCX, "uni-switch"); !errors.Is(err, ErrActive) {
		t.Fatalf("err %v, want ErrActive", err)
	}
	if b, _ := os.ReadFile(main); !bytes.Equal(b, orig) {
		t.Fatal("moved while a Codex had it open")
	}
	if fi, _ := os.Stat(main); !fi.ModTime().Equal(old) {
		t.Errorf("its time became %v, want %v", fi.ModTime(), old)
	}
	if p, _, _ := threadProvider(t, db, mgCX); p != "custom" {
		t.Errorf("its row moved to %q, its files didn't", p)
	}
	if kept := providerTrash(t); len(kept) != 0 {
		t.Errorf("a copy kept of a thread left as it was: %v", kept)
	}
	if left, _ := filepath.Glob(filepath.Join(filepath.Dir(main), ".uni-switch-*")); len(left) > 0 {
		t.Errorf("left behind: %v", left)
	}

	letGo()
	mv, err := MoveCodexProvider(mgCX, "uni-switch")
	if err != nil || len(mv.Files) != 1 || len(mv.DB) != 1 {
		t.Fatalf("after the Codex let go: %+v %v", mv, err)
	}
	if got := codexMetaProvider(main); got != "uni-switch" {
		t.Fatalf("file on %q", got)
	}
	if p, _, _ := threadProvider(t, db, mgCX); p != "uni-switch" {
		t.Errorf("row on %q", p)
	}
	if _, err := os.Stat(lock); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("the thread's lock file is left: %v", err)
	}
}

// moveProbe is run by uni_switch_move_probe(), which a test's state database
// calls from a trigger as a move writes a thread's row.
var (
	moveProbeOnce sync.Once
	moveProbe     func()
)

// probeRowMove has probe run from inside the state database db as a move
// writes a thread's row, which it does after the thread's files.
func probeRowMove(t *testing.T, db string, probe func()) {
	t.Helper()
	moveProbeOnce.Do(func() {
		sqlite.MustRegisterScalarFunction("uni_switch_move_probe", 0, func(*sqlite.FunctionContext, []driver.Value) (driver.Value, error) {
			if moveProbe != nil {
				moveProbe()
			}
			return nil, nil
		})
	})
	moveProbe = probe
	t.Cleanup(func() { moveProbe = nil })
	d, err := sql.Open("sqlite", db)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	if _, err := d.Exec(`CREATE TRIGGER probe BEFORE UPDATE OF model_provider ON threads BEGIN SELECT uni_switch_move_probe(); END`); err != nil {
		t.Fatal(err)
	}
}

// A move holds the thread as Codex's own archive and delete hold one while
// they move its rollouts (thread-store's acquire_writer_lock): from before
// its files are copied until its row is written. A Codex that opens the
// thread meanwhile is turned away by Codex itself ("already has an active
// writer"), never handed a file about to be put out of its place, and the
// files and the row move together. Looked at from inside the row's write.
func TestMoveCodexProviderHoldsTheThread(t *testing.T) {
	_, codex := manageSetup(t)
	main := filepath.Join(codex, "sessions", "2026", "09", "20", "rollout-2026-09-20T15-48-28-"+mgCX+".jsonl")
	b, _ := os.ReadFile(main)
	old := time.Now().Add(-24 * time.Hour)
	os.WriteFile(main, bytes.Replace(b, []byte(`"model_provider":"openai"`), []byte(`"model_provider":"custom"`), 1), 0o644)
	os.Chtimes(main, old, old)
	db := codexStateDB(t, codex, mgCX, "custom")
	lock := filepath.Join(codex, "thread-writer-locks", mgCX+".lock")
	if err := os.MkdirAll(filepath.Dir(lock), 0o755); err != nil {
		t.Fatal(err)
	}
	var seen []string
	probeRowMove(t, db, func() {
		lf, err := os.OpenFile(lock, os.O_RDWR, 0)
		if err != nil {
			seen = append(seen, "no lock file")
			return
		}
		defer lf.Close()
		if got, _ := filelock.TryLock(lf); got {
			filelock.Unlock(lf)
			seen = append(seen, "free")
		} else {
			seen = append(seen, "held")
		}
		seen = append(seen, "file on "+codexMetaProvider(main))
	})

	if _, err := MoveCodexProvider(mgCX, "uni-switch"); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(seen, ", "); got != "held, file on uni-switch" {
		t.Fatalf("as the row was written: %s; want held, file on uni-switch", got)
	}
	if _, err := os.Stat(lock); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("the thread's lock file is left: %v", err)
	}
}

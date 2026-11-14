package sessions

import (
	"database/sql"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

// cxOther is a thread whose rollout is still there: its catalog row stays.
const cxOther = "01a183bb-6c1e-7a52-9f0e-2b7d5c3e8a41"

// cxExecFile runs a schema file the real app or Codex made.
func cxExecFile(t *testing.T, db *sql.DB, name string) {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", "codex-app", name))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(string(b)); err != nil {
		t.Fatalf("%s: %v", name, err)
	}
}

func cxOpenDB(t *testing.T, path string) *sql.DB {
	t.Helper()
	os.MkdirAll(filepath.Dir(path), 0o755)
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

// cxMakeState makes Codex's state_5.sqlite with the real threads table: a
// row for mgCX at its first rollout, and one for cxOther at a rollout
// that is there.
func cxMakeState(t *testing.T, codex string) {
	t.Helper()
	db := cxOpenDB(t, filepath.Join(codex, "state_5.sqlite"))
	cxExecFile(t, db, "state_5.sql")
	first := filepath.Join(codex, "sessions", "2026", "09", "20", "rollout-2026-09-20T15-48-28-"+mgCX+".jsonl")
	other := filepath.Join(codex, "sessions", "2026", "09", "21", "rollout-2026-09-21T10-00-00-"+cxOther+".jsonl")
	os.MkdirAll(filepath.Dir(other), 0o755)
	if err := os.WriteFile(other, []byte(`{"timestamp":"2026-09-21T10:00:00.000Z","type":"session_meta","payload":{"id":"`+cxOther+`","cwd":"/work/other"}}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, r := range []struct{ id, path string }{{mgCX, first}, {cxOther, other}} {
		if _, err := db.Exec(`INSERT INTO threads (id, rollout_path, created_at, updated_at, source, model_provider, cwd, title, sandbox_policy, approval_mode, cli_version, first_user_message, preview) VALUES (?, ?, 1790524108, 1790524300, 'vscode', 'openai', '/work/app', 'Fix the parser', '{"type":"workspace-write"}', 'on-request', '0.165.0-alpha.1.2', 'Fix the parser', 'Fix the parser')`, r.id, r.path); err != nil {
			t.Fatal(err)
		}
	}
}

// cxMakeCatalog makes the Codex app's catalog under <codex>/sqlite/name
// with the app's own tables: mgCX on the local host and on a ChatGPT host,
// cxOther on the local host, catalog_revision 7. The rows are the shape
// of a real one (Codex app 26.1021).
func cxMakeCatalog(t *testing.T, codex, name string) *sql.DB {
	t.Helper()
	db := cxOpenDB(t, filepath.Join(codex, "sqlite", name))
	cxExecFile(t, db, "catalog.sql")
	for _, q := range []string{
		`INSERT INTO local_thread_catalog_hosts VALUES ('local', 'local'), ('chatgpt:acct', 'chatgpt')`,
		`INSERT INTO local_thread_catalog_metadata VALUES (1, 7)`,
		`INSERT INTO local_thread_catalog_sync_state (host_id, watermark_updated_at, initial_build_complete, observation_sequence, last_full_reconciled_at) VALUES ('local', 1793211692.0, 1, 368, 1788780881932)`,
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	for _, r := range []struct{ host, id string }{{"local", mgCX}, {"chatgpt:acct", mgCX}, {"local", cxOther}} {
		if _, err := db.Exec(`INSERT INTO local_thread_catalog (host_id, thread_id, display_title, source_created_at, source_updated_at, cwd, source_kind, source_detail, model_provider, git_branch, observation_sequence, missing_candidate, thread_source, source_recency_at, pending_observed_title, project_id, conversation_origin, trial_conversation_type, chatgpt_async_status) VALUES (?, ?, '造型试穿自动任务', 1793211496.0, 1793211692.0, '/work/app', 'vscode', NULL, 'openai', NULL, 368, 0, NULL, 1793211496.0, 0, NULL, NULL, NULL, NULL)`, r.host, r.id); err != nil {
			t.Fatal(err)
		}
	}
	return db
}

func cxRow(t *testing.T, db *sql.DB, host, id string) (title string, created float64, ok bool) {
	t.Helper()
	err := db.QueryRow(`SELECT display_title, source_created_at FROM local_thread_catalog WHERE host_id = ? AND thread_id = ?`, host, id).Scan(&title, &created)
	if err == sql.ErrNoRows {
		return "", 0, false
	}
	if err != nil {
		t.Fatal(err)
	}
	return title, created, true
}

func cxRevision(t *testing.T, db *sql.DB) int64 {
	t.Helper()
	var n int64
	if err := db.QueryRow(`SELECT catalog_revision FROM local_thread_catalog_metadata WHERE id = 1`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// Deleting a Codex session takes it out of the Codex app's list too: the
// app lists from its catalog and drops a row only when told the thread is
// gone (zhuddan on Discord, Codex 26.1023). Restore puts the row back.
func TestDeleteForgetsCodexAppCatalog(t *testing.T) {
	_, codex := manageSetup(t)
	cxMakeState(t, codex)
	db := cxMakeCatalog(t, codex, "codex.db")
	tr, err := Delete("codex", mgCX)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, ok := cxRow(t, db, "local", mgCX); ok {
		t.Fatal("the Codex app still lists the deleted conversation")
	}
	if _, _, ok := cxRow(t, db, "chatgpt:acct", mgCX); !ok {
		t.Fatal("another host's row was taken out")
	}
	if _, _, ok := cxRow(t, db, "local", cxOther); !ok {
		t.Fatal("a conversation whose rollout is there was taken out")
	}
	if n := cxRevision(t, db); n != 8 {
		t.Fatalf("catalog_revision %d, want 8", n)
	}
	var threads int
	db2 := cxOpenDB(t, filepath.Join(codex, "state_5.sqlite"))
	db2.QueryRow(`SELECT count(*) FROM threads`).Scan(&threads)
	if threads != 2 {
		t.Fatalf("Codex's state database changed: %d threads", threads)
	}
	if _, err := Restore(tr.Key); err != nil {
		t.Fatal(err)
	}
	title, created, ok := cxRow(t, db, "local", mgCX)
	if !ok || title != "造型试穿自动任务" || created != 1793211496.0 {
		t.Fatalf("restored row %q %v %v", title, created, ok)
	}
	if n := cxRevision(t, db); n != 9 {
		t.Fatalf("catalog_revision %d after restore, want 9", n)
	}
}

// A conversation deleted before uni-switch forgot catalog rows is taken out
// of the app's list when the Sessions page is next read, and Restore
// still brings it back. The app's non-prod build's codex-dev.db counts.
func TestTidyCodexCatalogForgetsEarlierDeletes(t *testing.T) {
	_, codex := manageSetup(t)
	cxMakeState(t, codex)
	tr, err := Delete("codex", mgCX) // no catalog yet: as an older uni-switch did
	if err != nil {
		t.Fatal(err)
	}
	db := cxMakeCatalog(t, codex, "codex-dev.db")
	tidyOnce = sync.Once{}
	t.Cleanup(func() { tidyOnce = sync.Once{} })
	TidyCodexCatalog()
	if _, _, ok := cxRow(t, db, "local", mgCX); ok {
		t.Fatal("the earlier delete is still in the Codex app's list")
	}
	if _, _, ok := cxRow(t, db, "local", cxOther); !ok {
		t.Fatal("a conversation whose rollout is there was taken out")
	}
	if _, err := Restore(tr.Key); err != nil {
		t.Fatal(err)
	}
	if _, _, ok := cxRow(t, db, "local", mgCX); !ok {
		t.Fatal("Restore didn't put the row the tidy took back")
	}
}

// A rollout that can't be looked at counts as there, and one the state
// database names outside the Codex folder is never judged.
func TestTidyCodexCatalogKeepsWhatItCantJudge(t *testing.T) {
	_, codex := manageSetup(t)
	cxMakeState(t, codex)
	db := cxMakeCatalog(t, codex, "codex.db")
	st := cxOpenDB(t, filepath.Join(codex, "state_5.sqlite"))
	if _, err := st.Exec(`UPDATE threads SET rollout_path = ? WHERE id = ?`, filepath.Join(t.TempDir(), "rollout-"+cxOther+".jsonl"), cxOther); err != nil {
		t.Fatal(err)
	}
	if err := tidyCodexCatalog([]string{codex}); err != nil {
		t.Fatal(err)
	}
	if _, _, ok := cxRow(t, db, "local", cxOther); !ok {
		t.Fatal("a row whose rollout is outside the Codex folder was taken out")
	}
	if _, _, ok := cxRow(t, db, "local", mgCX); !ok {
		t.Fatal("a row whose rollout is there was taken out")
	}
	if n := cxRevision(t, db); n != 7 {
		t.Fatalf("catalog_revision %d, want 7", n)
	}
}

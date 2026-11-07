package state

import (
	"context"
	"path/filepath"
	"runtime"
	"testing"
)

func TestWindowsCanonicalDatabasePath(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows filename syntax")
	}
	path := `\\?\` + filepath.Join(t.TempDir(), "uni-switch.db")
	if _, e := Execute(context.Background(), "state.initialize", Input{Path: path, Query: "CREATE TABLE sample(id INTEGER)"}); e != nil {
		t.Fatal(e)
	}
	if _, e := Execute(context.Background(), "state.execute", Input{Path: path, Query: "INSERT INTO sample VALUES(?)", Params: []any{int64(7)}}); e != nil {
		t.Fatal(e)
	}
	r, e := Execute(context.Background(), "state.query", Input{Path: path, Query: "SELECT id FROM sample"})
	if e != nil || r.Rows[0][0] != int64(7) {
		t.Fatal(r, e)
	}
}

func TestProviderSnapshotAndJournalTransaction(t *testing.T) {
	path := filepath.Join(t.TempDir(), "uni-switch.db")
	ctx := context.Background()
	if _, e := Execute(ctx, "state.initialize", Input{Path: path, Query: "CREATE TABLE providers(id TEXT PRIMARY KEY,data TEXT);CREATE TABLE applied(id TEXT PRIMARY KEY,summary TEXT);"}); e != nil {
		t.Fatal(e)
	}
	batch := []Statement{{"INSERT INTO providers VALUES(?1,?2)", []any{"p", "draft"}}, {"INSERT INTO applied VALUES(?1,?2)", []any{"p", "snapshot"}}}
	if _, e := Execute(ctx, "state.execute", Input{Path: path, Batch: batch}); e != nil {
		t.Fatal(e)
	}
	if _, e := Execute(ctx, "state.execute", Input{Path: path, Query: "UPDATE providers SET data='edited' WHERE id='p'"}); e != nil {
		t.Fatal(e)
	}
	result, e := Execute(ctx, "state.query", Input{Path: path, Query: "SELECT summary FROM applied WHERE id='p'"})
	if e != nil || result.Rows[0][0] != "snapshot" {
		t.Fatal(result, e)
	}
}
func TestFailedStateBatchDoesNotPartiallyCommit(t *testing.T) {
	path := filepath.Join(t.TempDir(), "uni-switch.db")
	ctx := context.Background()
	_, _ = Execute(ctx, "state.initialize", Input{Path: path, Query: "CREATE TABLE providers(id TEXT PRIMARY KEY,data TEXT);INSERT INTO providers VALUES('p','original');"})
	_, e := Execute(ctx, "state.execute", Input{Path: path, Batch: []Statement{{"UPDATE providers SET data='changed' WHERE id='p'", nil}, {"INSERT INTO nonexistent VALUES('bad')", nil}}})
	if e == nil {
		t.Fatal("failed batch succeeded")
	}
	r, e := Execute(ctx, "state.query", Input{Path: path, Query: "SELECT data FROM providers WHERE id='p'"})
	if e != nil || r.Rows[0][0] != "original" {
		t.Fatal(r, e)
	}
}

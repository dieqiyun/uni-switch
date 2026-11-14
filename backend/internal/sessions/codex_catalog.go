package sessions

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// The Codex app (ChatGPT.app's Codex, 26.x) lists conversations from a
// catalog of its own, not from the rollouts: <CODEX_HOME>/sqlite/codex.db
// (codex-dev.db for a build whose BUILD_FLAVOR isn't "prod"), table
// local_thread_catalog, a row per host and thread. It fills the catalog
// from its app-server's thread/list, but once its first full sweep is done
// (local_thread_catalog_sync_state.last_full_reconciled_at) it only takes
// in threads updated since its watermark. A row leaves the catalog only on
// the app-server's thread/deleted or thread/archived notification
// (removeThreadFromCatalog), which Codex's own delete sends. A rollout
// uni-switch moved to its trash was no longer listed by the app-server
// (thread/list skips a thread whose rollout is gone), but its catalog row
// stayed, so the app went on listing it (zhuddan on Discord, Codex
// 26.1023).
//
// forgetCodexCatalog does what the app does on thread/deleted: the local
// host's row of a thread whose rollout is gone is deleted and
// catalog_revision raised, in one write transaction that waits for the
// app's own. Only threads Codex's state database names with a rollout
// under that Codex folder's sessions or archived_sessions, and neither it
// nor its packed twin there, are forgotten; a rollout that can't be
// looked at counts as there. The rows of a thread in uni-switch's trash are
// kept in its note, and Restore puts them back, since the app wouldn't
// take in a thread that old again.

// codexCatalogNames are the Codex app's database names, by build flavor.
var codexCatalogNames = []string{"codex.db", "codex-dev.db"}

// codexCatalogRow is a catalog row taken out, to be put back on Restore.
type codexCatalogRow struct {
	DB   string     `json:"db"`
	Cols []string   `json:"cols"`
	Vals []sqlValue `json:"vals"`
}

// sqlValue is a column's value with its SQLite type: none set is NULL.
type sqlValue struct {
	I *int64   `json:"i,omitempty"`
	R *float64 `json:"r,omitempty"`
	S *string  `json:"s,omitempty"`
	B []byte   `json:"b,omitempty"`
}

func toSQLValue(v any) sqlValue {
	switch x := v.(type) {
	case int64:
		return sqlValue{I: &x}
	case float64:
		return sqlValue{R: &x}
	case string:
		return sqlValue{S: &x}
	case []byte:
		return sqlValue{B: append([]byte{}, x...)}
	case bool:
		n := int64(0)
		if x {
			n = 1
		}
		return sqlValue{I: &n}
	case time.Time:
		s := x.Format(time.RFC3339Nano)
		return sqlValue{S: &s}
	}
	return sqlValue{}
}

func (v sqlValue) value() any {
	switch {
	case v.I != nil:
		return *v.I
	case v.R != nil:
		return *v.R
	case v.S != nil:
		return *v.S
	case v.B != nil:
		return v.B
	}
	return nil
}

// codexCatalogDBs are the Codex app's catalog databases of a Codex folder.
func codexCatalogDBs(home string) []string {
	var out []string
	for _, n := range codexCatalogNames {
		p := filepath.Join(home, "sqlite", n)
		if fi, err := os.Stat(p); err == nil && fi.Mode().IsRegular() {
			out = append(out, p)
		}
	}
	return out
}

// sqliteURL is a database's file: URL with query q.
func sqliteURL(path, q string) string {
	u := url.URL{Scheme: "file", Path: filepath.ToSlash(path), RawQuery: q}
	if len(u.Path) >= 2 && u.Path[1] == ':' {
		u.Path = "/" + u.Path
	}
	return u.String()
}

// codexGoneThreads are the threads a Codex folder's state databases name
// whose rollout is gone from its sessions or archived_sessions.
func codexGoneThreads(home string) map[string]bool {
	gone := map[string]bool{}
	// Codex may write the path through the folder's real name (/private/var
	// for /var on macOS), so both spellings count
	var roots []string
	for _, h := range []string{filepath.Clean(home), sameDirKey(home)} {
		roots = append(roots, filepath.Join(h, "sessions"), filepath.Join(h, "archived_sessions"))
	}
	for _, p := range codexStateDBs(home) {
		db, err := sql.Open("sqlite", sqliteURL(p, "mode=ro&_pragma=busy_timeout(2000)"))
		if err != nil {
			continue
		}
		rs, err := db.Query(`SELECT id, rollout_path FROM threads`)
		if err != nil {
			db.Close()
			continue
		}
		for rs.Next() {
			var id, path string
			if rs.Scan(&id, &path) != nil || !safeID.MatchString(id) || !filepath.IsAbs(path) {
				continue
			}
			path = filepath.Clean(path)
			if !inside(path, roots) || !strings.Contains(filepath.Base(path), id) {
				continue
			}
			if missing(path) && missing(rolloutTwin(path)) {
				gone[id] = true
			}
		}
		rs.Close()
		db.Close()
	}
	return gone
}

// missing says whether p is known not to be there: an error other than
// "not there" is not knowing.
func missing(p string) bool {
	_, err := os.Lstat(p)
	return errors.Is(err, fs.ErrNotExist)
}

// forgetCodexCatalog takes the local host's catalog rows of the Codex
// folder's threads whose rollout is gone out of the Codex app's catalog,
// and says the rows it took out, by thread.
func forgetCodexCatalog(home string) (map[string][]codexCatalogRow, error) {
	dbs := codexCatalogDBs(home)
	if len(dbs) == 0 {
		return nil, nil
	}
	gone := codexGoneThreads(home)
	if len(gone) == 0 {
		return nil, nil
	}
	out := map[string][]codexCatalogRow{}
	var first error
	for _, p := range dbs {
		if err := forgetIn(p, gone, out); err != nil && first == nil {
			first = err
		}
	}
	return out, first
}

func forgetIn(path string, gone map[string]bool, out map[string][]codexCatalogRow) error {
	db, err := sql.Open("sqlite", sqliteURL(path, "_pragma=busy_timeout(5000)"))
	if err != nil {
		return err
	}
	defer db.Close()
	ctx := context.Background()
	conn, err := db.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	var n int
	// another version of the app: nothing to do
	if err := conn.QueryRowContext(ctx, `SELECT count(*) FROM sqlite_master WHERE type = 'table' AND name IN ('local_thread_catalog', 'local_thread_catalog_hosts')`).Scan(&n); err != nil || n != 2 {
		return nil
	}
	ids, _ := json.Marshal(keys(gone))
	// the write lock first, so the rows read are the rows deleted
	if _, err := conn.ExecContext(ctx, `BEGIN IMMEDIATE`); err != nil {
		return err
	}
	done := false
	defer func() {
		if !done {
			conn.ExecContext(ctx, `ROLLBACK`)
		}
	}()
	rs, err := conn.QueryContext(ctx, `SELECT * FROM local_thread_catalog
		WHERE thread_id IN (SELECT value FROM json_each(?))
		  AND host_id IN (SELECT host_id FROM local_thread_catalog_hosts WHERE host_kind = 'local')`, string(ids))
	if err != nil {
		return err
	}
	cols, _ := rs.Columns()
	type taken struct {
		host, thread string
		row          codexCatalogRow
	}
	var rows []taken
	for rs.Next() {
		vals := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rs.Scan(ptrs...); err != nil {
			rs.Close()
			return err
		}
		t := taken{row: codexCatalogRow{DB: path, Cols: cols}}
		for i, c := range cols {
			v := toSQLValue(vals[i])
			t.row.Vals = append(t.row.Vals, v)
			if v.S != nil && c == "host_id" {
				t.host = *v.S
			}
			if v.S != nil && c == "thread_id" {
				t.thread = *v.S
			}
		}
		rows = append(rows, t)
	}
	rs.Close()
	if err := rs.Err(); err != nil {
		return err
	}
	if len(rows) == 0 {
		return nil
	}
	for _, t := range rows {
		if _, err := conn.ExecContext(ctx, `DELETE FROM local_thread_catalog WHERE host_id = ? AND thread_id = ?`, t.host, t.thread); err != nil {
			return err
		}
	}
	bumpCatalogRevision(ctx, conn)
	if _, err := conn.ExecContext(ctx, `COMMIT`); err != nil {
		return err
	}
	done = true
	for _, t := range rows {
		out[t.thread] = append(out[t.thread], t.row)
	}
	return nil
}

// bumpCatalogRevision raises the catalog's revision, as the app does on
// each change it makes, where the database has one.
func bumpCatalogRevision(ctx context.Context, conn *sql.Conn) {
	conn.ExecContext(ctx, `UPDATE local_thread_catalog_metadata SET catalog_revision = catalog_revision + 1 WHERE id = 1`)
}

func keys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// restoreCodexCatalog puts a restored thread's catalog rows back, with the
// columns the table has now; a row the app has again is left as it is.
func restoreCodexCatalog(rows []codexCatalogRow) error {
	by := map[string][]codexCatalogRow{}
	for _, r := range rows {
		if len(r.Cols) == 0 || len(r.Cols) != len(r.Vals) || !filepath.IsAbs(r.DB) || !contains(codexCatalogNames, filepath.Base(r.DB)) {
			continue
		}
		by[r.DB] = append(by[r.DB], r)
	}
	var first error
	for p, rs := range by {
		if err := restoreIn(p, rs); err != nil && first == nil {
			first = err
		}
	}
	return first
}

func restoreIn(path string, rows []codexCatalogRow) error {
	if fi, err := os.Stat(path); err != nil || !fi.Mode().IsRegular() {
		return nil
	}
	db, err := sql.Open("sqlite", sqliteURL(path, "_pragma=busy_timeout(5000)"))
	if err != nil {
		return err
	}
	defer db.Close()
	ctx := context.Background()
	conn, err := db.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	have := map[string]bool{}
	rs, err := conn.QueryContext(ctx, `SELECT name FROM pragma_table_info('local_thread_catalog')`)
	if err != nil {
		return err
	}
	for rs.Next() {
		var n string
		if rs.Scan(&n) == nil {
			have[n] = true
		}
	}
	rs.Close()
	if !have["host_id"] || !have["thread_id"] {
		return nil
	}
	if _, err := conn.ExecContext(ctx, `BEGIN IMMEDIATE`); err != nil {
		return err
	}
	added := int64(0)
	for _, r := range rows {
		var cols, marks []string
		var args []any
		for i, c := range r.Cols {
			if have[c] {
				cols = append(cols, `"`+strings.ReplaceAll(c, `"`, `""`)+`"`)
				marks = append(marks, "?")
				args = append(args, r.Vals[i].value())
			}
		}
		res, err := conn.ExecContext(ctx, `INSERT OR IGNORE INTO local_thread_catalog (`+strings.Join(cols, ", ")+`) VALUES (`+strings.Join(marks, ", ")+`)`, args...)
		if err != nil {
			conn.ExecContext(ctx, `ROLLBACK`)
			return err
		}
		n, _ := res.RowsAffected()
		added += n
	}
	if added > 0 {
		bumpCatalogRevision(ctx, conn)
	}
	_, err = conn.ExecContext(ctx, `COMMIT`)
	return err
}

// tidyCodexCatalog forgets, in each Codex folder, the catalog rows of the
// threads whose rollout is gone, and keeps the rows of those in uni-switch's
// trash in their notes. The caller holds mu.
func tidyCodexCatalog(homes []string) error {
	var first error
	took := map[string][]codexCatalogRow{}
	seen := map[string]bool{}
	for _, h := range homes {
		if h == "" || seen[sameDirKey(h)] {
			continue
		}
		seen[sameDirKey(h)] = true
		rows, err := forgetCodexCatalog(h)
		if err != nil && first == nil {
			first = err
		}
		for id, rs := range rows {
			took[id] = append(took[id], rs...)
		}
	}
	if len(took) == 0 {
		return first
	}
	notes, _ := filepath.Glob(filepath.Join(TrashDir(), "codex", "*", manifest))
	for _, p := range notes {
		var t Trashed
		b, err := os.ReadFile(p)
		if err != nil || json.Unmarshal(b, &t) != nil || len(took[t.ID]) == 0 {
			continue
		}
		t.CodexCatalog = append(t.CodexCatalog, took[t.ID]...)
		if err := writeManifest(filepath.Dir(p), t); err != nil && first == nil {
			first = err
		}
	}
	return first
}

func sameDirKey(p string) string {
	if r, err := filepath.EvalSymlinks(p); err == nil {
		p = r
	}
	return filepath.Clean(p)
}

var tidyOnce sync.Once

// TidyCodexCatalog takes the Codex app's catalog rows of the threads whose
// rollout is gone out of it, once a run of uni-switch: those uni-switch deleted
// before it did so itself.
func TidyCodexCatalog() {
	if !Reads("codex") {
		return
	}
	tidyOnce.Do(func() {
		mu.Lock()
		defer mu.Unlock()
		tidyCodexCatalog(CodexDirs())
	})
}

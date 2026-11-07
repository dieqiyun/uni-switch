// Package state owns persistence of provider records, applied snapshots and
// operation journals. The desktop retains the existing on-disk SQLite schema
// during migration: a different storage format is not a missing feature.
package state

import (
	"context"
	"database/sql"
	"encoding/json"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/edit"
	_ "modernc.org/sqlite"
)

type Statement struct {
	SQL    string `json:"sql"`
	Params []any  `json:"params"`
}
type Input struct {
	Path   string      `json:"path"`
	Query  string      `json:"query"`
	Params []any       `json:"params"`
	Batch  []Statement `json:"batch"`
}
type Result struct {
	Rows    [][]any `json:"rows"`
	Changed int64   `json:"changed"`
}

func failure() error {
	return &edit.Error{Code: "database_error", Message: "本地状态存储操作失败，请检查数据目录和磁盘空间"}
}
func args(values []any) []any {
	out := make([]any, len(values))
	for i, v := range values {
		if n, ok := v.(json.Number); ok {
			if x, e := n.Int64(); e == nil {
				out[i] = x
			} else {
				x, _ := n.Float64()
				out[i] = x
			}
		} else {
			out[i] = v
		}
	}
	return out
}

// Open is shared by the Go configuration coordinator and read-only gateway
// snapshot lookup. Both use the same durable database as existing installations.
func Open(path string) (*sql.DB, error) {
	if runtime.GOOS == "windows" {
		if strings.HasPrefix(path, `\\?\UNC\`) {
			path = `\\` + strings.TrimPrefix(path, `\\?\UNC\`)
		} else {
			path = strings.TrimPrefix(path, `\\?\`)
		}
	}
	if !filepath.IsAbs(path) || filepath.Base(path) != "uni-switch.db" {
		return nil, failure()
	}
	db, e := sql.Open("sqlite", path)
	if e != nil {
		return nil, failure()
	}
	db.SetMaxOpenConns(1)
	if _, e = db.Exec("PRAGMA busy_timeout=5000; PRAGMA synchronous=FULL;"); e != nil {
		db.Close()
		return nil, failure()
	}
	return db, nil
}
func Execute(parent context.Context, operation string, input Input) (Result, error) {
	result := Result{Rows: [][]any{}}
	// Windows canonicalize supplies verbatim paths. SQLite's filename parser
	// treats the '?' as URI syntax; give it the equivalent ordinary filename.
	if runtime.GOOS == "windows" {
		if strings.HasPrefix(input.Path, `\\?\UNC\`) {
			input.Path = `\\` + strings.TrimPrefix(input.Path, `\\?\UNC\`)
		} else {
			input.Path = strings.TrimPrefix(input.Path, `\\?\`)
		}
	}
	if !filepath.IsAbs(input.Path) || filepath.Base(input.Path) != "uni-switch.db" {
		return result, failure()
	}
	ctx, cancel := context.WithTimeout(parent, 15*time.Second)
	defer cancel()
	db, e := sql.Open("sqlite", input.Path)
	if e != nil {
		return result, failure()
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	if _, e = db.ExecContext(ctx, "PRAGMA busy_timeout=5000; PRAGMA synchronous=FULL;"); e != nil {
		return result, failure()
	}
	switch operation {
	case "state.query":
		// The request is built by the native Store, never exposed as a UI SQL API.
		q := strings.TrimSpace(strings.ToUpper(input.Query))
		if !strings.HasPrefix(q, "SELECT ") && !strings.HasPrefix(q, "PRAGMA TABLE_INFO(") {
			return result, failure()
		}
		rows, e := db.QueryContext(ctx, input.Query, args(input.Params)...)
		if e != nil {
			return result, failure()
		}
		defer rows.Close()
		columns, e := rows.Columns()
		if e != nil {
			return result, failure()
		}
		total := 0
		for rows.Next() {
			values := make([]any, len(columns))
			dest := make([]any, len(columns))
			for i := range dest {
				dest[i] = &values[i]
			}
			if rows.Scan(dest...) != nil {
				return result, failure()
			}
			for i, v := range values {
				if b, ok := v.([]byte); ok {
					values[i] = string(b)
				}
			}
			b, _ := json.Marshal(values)
			total += len(b)
			if total > 4<<20 {
				return result, failure()
			}
			result.Rows = append(result.Rows, values)
		}
		if rows.Err() != nil {
			return result, failure()
		}
		return result, nil
	case "state.execute":
		commands := input.Batch
		if commands == nil {
			commands = []Statement{{SQL: input.Query, Params: input.Params}}
		}
		tx, e := db.BeginTx(ctx, nil)
		if e != nil {
			return result, failure()
		}
		defer tx.Rollback()
		for _, s := range commands {
			r, e := tx.ExecContext(ctx, s.SQL, args(s.Params)...)
			if e != nil {
				return result, failure()
			}
			n, _ := r.RowsAffected()
			result.Changed += n
		}
		if tx.Commit() != nil {
			return result, failure()
		}
		return result, nil
	case "state.initialize":
		// PRAGMA journal_mode cannot be changed inside a transaction.
		if _, e = db.ExecContext(ctx, input.Query); e != nil {
			return result, failure()
		}
		return result, nil
	}
	return result, failure()
}

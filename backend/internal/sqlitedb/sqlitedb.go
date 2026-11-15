// Package sqlitedb is the one way uni-switch reaches SQLite: a package that
// opens a database imports it, blank, in place of modernc.org/sqlite.
//
//	import _ "github.com/dieqiyun/uni-switch/backend/internal/sqlitedb"
//
// modernc's SQLite runs on modernc's libc, which keeps its own copy of the
// environment, taken from os.Environ at the first getenv. On macOS,
// Windows and the BSDs that libc's getenv ends the whole process when it
// meets an entry with no "=" in it: it prints "libc.go:…:getenv:
// TODOTODO …" and calls os.Exit(1), where every real libc skips the entry.
// SQLite's first open reads SQLITE_TMPDIR, so a uni-switch started with such
// an entry quit at its first session scan, about two seconds after launch,
// with no crash report (#2327: a DLP tool's injected dylib left a fragment
// of its own path in the environment of apps Alfred started). Before
// anything can open a database, this package hands libc a copy with those
// entries left out.
package sqlitedb

import (
	"os"
	"strings"

	"modernc.org/libc"
	_ "modernc.org/sqlite"
)

// init runs before any of uni-switch's own code, and before any goroutine of
// it, so libc's copy is taken once, here: modernc's SetEnviron isn't safe
// to race with a first getenv.
func init() {
	libc.SetEnviron(nil, wellFormed(os.Environ()))
}

// wellFormed is env without the entries that aren't NAME=value. An entry
// whose name is empty after a leading "=" (Windows' "=C:=C:\…") has one,
// and getenv reads it without harm, so it stays.
func wellFormed(env []string) []string {
	out := make([]string, 0, len(env))
	for _, kv := range env {
		if strings.Contains(kv, "=") {
			out = append(out, kv)
		}
	}
	return out
}

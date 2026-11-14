package agent

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// #1665: Drift asks where Codex's requests went on every poll, and each ask
// opened logs_*.sqlite again (~2,500 opens in 25s of a start on Windows,
// each one work a filter driver multiplies). The log is read again only
// once it changed; asked again meanwhile, the answer is the one read.
func TestCodexReachedReadsLogOnlyWhenItChanged(t *testing.T) {
	home, _ := codexHome(t, "", "")
	dir := filepath.Join(home, ".codex")
	os.MkdirAll(dir, 0o700)
	db, err := sql.Open("sqlite", filepath.Join(dir, "logs_2.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`PRAGMA journal_mode=WAL`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE logs (id INTEGER PRIMARY KEY AUTOINCREMENT, ts INTEGER NOT NULL, ts_nanos INTEGER NOT NULL,
		level TEXT NOT NULL, target TEXT NOT NULL, feedback_log_body TEXT)`); err != nil {
		t.Fatal(err)
	}
	at := time.Now().Add(-time.Minute).Unix()
	log := func(url string) {
		at++
		if _, err := db.Exec(`INSERT INTO logs (ts, ts_nanos, level, target, feedback_log_body) VALUES (?, 0, 'INFO', 'codex_api::endpoint::responses_websocket', ?)`,
			at, "responses_websocket.connect{}: connecting to websocket: "+url); err != nil {
			t.Fatal(err)
		}
	}
	log("ws://127.0.0.1:3425/backend-api/codex/responses")
	since := time.Now().Add(-time.Hour)

	before := reachedOpens.Load()
	for range 50 {
		got, to, _ := codexReached(dir, since)
		if got.Unix() != at || to != "ws://127.0.0.1:3425/backend-api/codex/responses" {
			t.Fatalf("read %v %q, want %v and uni-switch's address", got, to, at)
		}
	}
	if n := reachedOpens.Load() - before; n != 1 {
		t.Fatalf("50 asks read the log %d times, want 1", n)
	}

	// a new request logged is read on the next ask
	log("wss://chatgpt.com/backend-api/codex/responses")
	if got, to, _ := codexReached(dir, since); got.Unix() != at || to != "wss://chatgpt.com/backend-api/codex/responses" {
		t.Fatalf("after a new request: %v %q", got, to)
	}
	if n := reachedOpens.Load() - before; n != 2 {
		t.Fatalf("the log read %d times, want 2 once it changed", n)
	}

	// asked from a later time, the answer is read for that time
	if got, _, _ := codexReached(dir, time.Now().Add(time.Minute)); !got.IsZero() {
		t.Fatalf("since after every request: %v", got)
	}
}

// forgetReached drops what codexReached kept of Codex's log, for a test of
// what Drift says about each request it logs, asked right after it. Two
// inserts inside one tick of the file system's clock can leave the log's
// size and time as they were (a rollback journal writes the same page
// again; a -wal that wrapped, as Codex's own has, writes over its old
// frames), and the kept read then stands for up to reachedFor, as it does
// in the app. When the log is read again is
// TestCodexReachedReadsLogOnlyWhenItChanged's to test.
func forgetReached() {
	reachedMu.Lock()
	clear(reachedMemo)
	reachedMu.Unlock()
}

package library

import (
	"path/filepath"
	"testing"
	"time"
)

// Two changes in one millisecond each keep their own copy at the name
// they wrote, not the other's (TestRemoveFoundEntryEveryFormat on a fast
// Linux machine, #1743).
func TestBackupsInOneMillisecond(t *testing.T) {
	sandbox(t)
	at := time.Date(2026, 10, 13, 12, 0, 0, 0, time.Local)
	was := backupNow
	backupNow = func() time.Time { return at }
	t.Cleanup(func() { backupNow = was })
	var runs []*backups
	for _, body := range []string{"first", "second", "third"} {
		p := filepath.Join(t.TempDir(), "config.json")
		write(t, p, body)
		b := newBackups()
		if err := b.keep("x", p); err != nil {
			t.Fatal(err)
		}
		runs = append(runs, b)
		if got := read(t, filepath.Join(b.dir, "x", "config.json")); got != body {
			t.Fatalf("the %s change's backup holds %q", body, got)
		}
	}
	if runs[0].dir == runs[1].dir || runs[1].dir == runs[2].dir || runs[0].dir == runs[2].dir {
		t.Fatalf("changes share a folder: %s %s %s", runs[0].dir, runs[1].dir, runs[2].dir)
	}
}

package agent

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// A sync that changes nothing writes nothing: with every agent connected,
// a second SyncCatalog (and what a running gateway keeps up) leaves each
// file's mtime as it was. A rewrite of the same bytes wakes every watcher
// of the file and swaps it under an agent reading it (#3870); before, 15
// files here were written again on each sync.
func TestSecondSyncWritesNothing(t *testing.T) {
	home, _ := codexHome(t, "", "")
	managed := claudeManaged
	claudeManaged = func() string { return filepath.Join(home, "managed-settings.json") }
	t.Cleanup(func() { claudeManaged = managed })
	for _, a := range All() {
		if a.Launch != nil || a.Native != nil || a.WSL != "" || len(a.Fields) == 0 {
			continue
		}
		if _, err := a.ConnectHow(); err != nil {
			continue
		}
		if err := a.Connect(); err != nil {
			t.Logf("%s: %v", a.ID, err)
		}
	}
	SyncCatalog()
	dshWiredOnce()
	old := time.Now().Add(-time.Hour).Truncate(time.Second)
	files := 0
	filepath.WalkDir(home, func(p string, e os.DirEntry, err error) error {
		if err == nil && !e.IsDir() {
			os.Chtimes(p, old, old)
			files++
		}
		return nil
	})
	if files < 20 {
		t.Fatalf("only %d files under home: agents weren't connected", files)
	}
	SyncCatalog()
	dshWiredOnce()
	filepath.WalkDir(home, func(p string, e os.DirEntry, err error) error {
		if err != nil || e.IsDir() {
			return nil
		}
		rel, _ := filepath.Rel(home, p)
		if st, err := os.Stat(p); err == nil && !st.ModTime().Equal(old) {
			t.Errorf("second sync wrote %s", rel)
		}
		return nil
	})
}

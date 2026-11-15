package sessions

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func ageTree(root string) {
	old := time.Now().Add(-24 * time.Hour)
	filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err == nil {
			os.Chtimes(p, old, old)
		}
		return nil
	})
}

// zachzhao1984 on Discord: some agents' sessions can't be deleted on the
// Sessions page. Droid's are files of its own (a transcript and its
// .settings.json, a subagent's beside them; the session index Droid keeps
// is a cache it rebuilds from them), so one goes to the trash whole: its
// transcript, its settings, its subagent's two, and nothing of another
// session's. Restore puts them back.
func TestDroidDeleteRestore(t *testing.T) {
	manageSetup(t)
	home := filepath.Join(t.TempDir(), "droid-home")
	t.Setenv("FACTORY_HOME_OVERRIDE", home)
	putDroid(t, home)
	ageTree(home)
	Reset()

	if !Deletable("droid") {
		t.Fatal("droid not deletable")
	}
	m, ok := findManaged(ListAgent("droid"), droidMain)
	if !ok || !m.Deletable || m.Files != 2 {
		t.Fatalf("listed as %+v", m)
	}
	dir := filepath.Join(home, ".factory", "sessions", "-work-droid")
	mine := []string{
		filepath.Join(dir, droidMain+".jsonl"), filepath.Join(dir, droidMain+".settings.json"),
		filepath.Join(dir, droidSub+".jsonl"), filepath.Join(dir, droidSub+".settings.json"),
	}
	others := []string{
		filepath.Join(dir, droidFork+".jsonl"), filepath.Join(dir, droidFork+".settings.json"),
		filepath.Join(home, ".factory", "sessions", droidOld+".jsonl"), filepath.Join(home, ".factory", "settings.json"),
	}

	// one being written to is left alone
	now := time.Now()
	os.Chtimes(mine[3], now, now)
	if _, err := Delete("droid", droidMain); !errors.Is(err, ErrActive) {
		t.Fatalf("a session just written: %v", err)
	}
	ageTree(home)

	tr, err := Delete("droid", droidMain)
	if err != nil {
		t.Fatal(err)
	}
	if len(tr.Items) != 4 || tr.Cwd != "/work/droid" || tr.Title != "Login bug" {
		t.Fatalf("trashed %+v, want its transcript and settings and its subagent's", tr)
	}
	for _, p := range mine {
		if _, err := os.Lstat(p); err == nil {
			t.Fatalf("%s is still there", p)
		}
	}
	for _, p := range others {
		if _, err := os.Stat(p); err != nil {
			t.Fatalf("another's %s: %v", p, err)
		}
	}
	if _, ok := findManaged(ListAgent("droid"), droidMain); ok {
		t.Fatal("still listed after the delete")
	}

	if _, err := Restore(tr.Key); err != nil {
		t.Fatal(err)
	}
	for _, p := range mine {
		if _, err := os.Stat(p); err != nil {
			t.Fatalf("%s isn't back: %v", p, err)
		}
	}
	Reset()
	if m, ok := findManaged(ListAgent("droid"), droidMain); !ok || m.Title != "Login bug" || m.Files != 2 {
		t.Fatalf("the restored session: %+v", m)
	}
}

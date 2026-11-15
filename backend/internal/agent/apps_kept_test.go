package agent

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

// The Agents page asks every desktop app whether it is installed on each
// poll. An app folder of many entries is listed again only when it
// changed, and an Info.plist is read again only when it changed.
func TestAppFoldersReadAgainOnlyWhenChanged(t *testing.T) {
	apps := t.TempDir()
	wasFolders, wasSpot := appFolders, spotlightApps
	appFolders = func() []string { return []string{apps} }
	spotlightApps = func(string) []string { return nil }
	t.Cleanup(func() { appFolders, spotlightApps = wasFolders, wasSpot })

	plist := func(id string) []byte {
		return []byte("<plist><dict><key>CFBundleIdentifier</key><string>" + id +
			"</string><key>CFBundleShortVersionString</key><string>1.2.3</string></dict></plist>")
	}
	write := func(bundle, id string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Join(bundle, "Contents"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(bundle, "Contents", "Info.plist"), plist(id), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for i := range 300 {
		if err := os.Mkdir(filepath.Join(apps, "Other "+strconv.Itoa(i)+".app"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	write(filepath.Join(apps, "Kept.app"), "test.kept")
	old := time.Now().Add(-time.Hour)
	age := func(paths ...string) {
		t.Helper()
		for _, p := range paths {
			if err := os.Chtimes(p, old, old); err != nil {
				t.Fatal(err)
			}
		}
	}
	keptPlist := filepath.Join(apps, "Kept.app", "Contents", "Info.plist")
	age(apps, keptPlist)

	app := desktopApp{id: "test.kept", names: []string{"Kept.app"}}
	if !macBundle(app) || app.version() != "1.2.3" {
		t.Fatalf("Kept.app not found: bundle %v version %q", macBundle(app), app.version())
	}
	if n := testing.AllocsPerRun(20, func() { macBundle(app); app.version() }); n > 40 {
		t.Errorf("an unchanged app folder and Info.plist cost %v allocations a look; they are listed and read again", n)
	}

	// A folder added one level down is looked in at once (#2325).
	moved := desktopApp{id: "test.moved", names: []string{"Moved.app"}}
	if macBundle(moved) {
		t.Fatal("Moved.app found before it was installed")
	}
	write(filepath.Join(apps, "AI", "Moved.app"), "test.moved")
	if !macBundle(moved) {
		t.Error("Moved.app in a folder added since isn't found: the folder's old list was kept")
	}
	// So is an Info.plist written since, of the same size and an old time
	// the folder list was kept at.
	age(apps, filepath.Join(apps, "AI"))
	macBundle(moved)
	if err := os.WriteFile(keptPlist, plist("test.kxpt"), 0o644); err != nil {
		t.Fatal(err)
	}
	if macBundle(app) {
		t.Error("Kept.app's Info.plist says another id now, and the old one was kept")
	}
}

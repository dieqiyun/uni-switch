package gui

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"testing"
)

// A right-click on KDE opens the tray's menu, and Wails calls the click
// handler from the menu's "opened" event; that call must not open the
// panel, while the icon's own click (Activate) still does (#1430).
func TestTrayMenuOpeningIsNoClick(t *testing.T) {
	const app = "github.com/wailsapp/wails/v3/pkg/application."
	opened := []string{
		"uni-switch/internal/gui.Run.func12",
		app + "(*linuxSystemTray).Event",
		"reflect.Value.call",
		"github.com/godbus/dbus/v5.(*Conn).handleCall",
	}
	if !openedBy(opened) {
		t.Fatal("a click from the menu's opened event was counted a click")
	}
	activate := []string{
		"uni-switch/internal/gui.Run.func12",
		app + "(*linuxSystemTray).Activate",
		"reflect.Value.call",
	}
	if openedBy(activate) {
		t.Fatal("the icon's click (Activate) was taken for the menu opening")
	}
	if openedBy([]string{app + "(*linuxSystemTray).EventGroup"}) {
		t.Fatal("EventGroup is not Event")
	}
	if menuOpening() {
		t.Fatal("a plain call was taken for the menu opening")
	}
}

// The frame menuOpening looks for is the Wails function that calls the
// click handler on the menu's "opened" event; an upgrade that renames it
// or stops calling the handler there fails here.
func TestTrayMenuOpenFrameIsWails(t *testing.T) {
	// go list -m names no folder for a module not yet unpacked, as on CI;
	// go mod download unpacks it and names its folder
	out, err := exec.Command("go", "mod", "download", "-json", "github.com/wailsapp/wails/v3").Output()
	if err != nil {
		t.Skip("go mod download:", err)
	}
	var mod struct{ Dir string }
	if err := json.Unmarshal(out, &mod); err != nil || mod.Dir == "" {
		t.Fatalf("go mod download named no folder: %s", out)
	}
	src, err := os.ReadFile(filepath.Join(mod.Dir, "pkg", "application", "systemtray_linux.go"))
	if err != nil {
		t.Fatal(err)
	}
	fn := regexp.MustCompile(`(?s)func \(s \*linuxSystemTray\) Event\(.*?\n}\n`).Find(src)
	if fn == nil {
		t.Fatalf("Wails has no (*linuxSystemTray).Event; menuOpenFrame %q is stale", menuOpenFrame)
	}
	if !regexp.MustCompile(`(?s)case "opened":\s*if s\.parent\.clickHandler != nil \{\s*s\.parent\.clickHandler\(\)`).Match(fn) {
		t.Fatalf("Wails' Event no longer calls the click handler on \"opened\"; see menuOpening:\n%s", fn)
	}
}

// fakeTray is the shape clickIn reads in Wails' SystemTray: impl, an
// interface holding a pointer to the platform's tray.
type fakeTray struct{ impl any }

type fakeLinuxTray struct {
	label                  string
	lastClickX, lastClickY int
}

// niri places the panel by where the tray host said the icon was clicked,
// which Wails keeps from Activate(x, y) and gives the click handler no way
// to ask for (#1670).
func TestTrayClickIn(t *testing.T) {
	for _, c := range []struct {
		name string
		tray fakeTray
		x    int
		ok   bool
	}{
		{"clicked", fakeTray{&fakeLinuxTray{lastClickX: 1490, lastClickY: 14}}, 1490, true},
		{"clicked at the bar's left end", fakeTray{&fakeLinuxTray{lastClickX: 0, lastClickY: 14}}, 0, true},
		{"no coordinates from the host", fakeTray{&fakeLinuxTray{}}, 0, false},
		{"no tray yet", fakeTray{}, 0, false},
		{"fields gone in a later Wails", fakeTray{&struct{ lastClickX string }{"x"}}, 0, false},
		{"not a pointer", fakeTray{fakeLinuxTray{lastClickX: 5}}, 0, false},
	} {
		x, ok := clickIn(reflect.ValueOf(&c.tray).Elem())
		if x != c.x || ok != c.ok {
			t.Errorf("%s: %d %v, want %d %v", c.name, x, ok, c.x, c.ok)
		}
	}
}

// clickIn reads the fields Wails' Activate fills before it calls the click
// handler; an upgrade that renames them or stops keeping the click fails
// here.
func TestTrayClickIsWails(t *testing.T) {
	out, err := exec.Command("go", "mod", "download", "-json", "github.com/wailsapp/wails/v3").Output()
	if err != nil {
		t.Skip("go mod download:", err)
	}
	var mod struct{ Dir string }
	if err := json.Unmarshal(out, &mod); err != nil || mod.Dir == "" {
		t.Fatalf("go mod download named no folder: %s", out)
	}
	app := filepath.Join(mod.Dir, "pkg", "application")
	src, err := os.ReadFile(filepath.Join(app, "systemtray_linux.go"))
	if err != nil {
		t.Fatal(err)
	}
	if !regexp.MustCompile(`(?s)type linuxSystemTray struct \{.*?\n\s*lastClickX int\n\s*lastClickY int\n`).Match(src) {
		t.Fatal("Wails' linuxSystemTray has no lastClickX, lastClickY int; see clickIn")
	}
	if !regexp.MustCompile(`(?s)func \(s \*linuxSystemTray\) Activate\(x int32, y int32\) \(err \*dbus\.Error\) \{\s*s\.lastClickX = int\(x\)\s*s\.lastClickY = int\(y\).*?s\.parent\.clickHandler\(\)`).Match(src) {
		t.Fatal("Wails' Activate no longer keeps the click before calling the click handler; see clickIn")
	}
	src, err = os.ReadFile(filepath.Join(app, "systemtray.go"))
	if err != nil {
		t.Fatal(err)
	}
	if !regexp.MustCompile(`(?s)type SystemTray struct \{.*?\n\s*impl\s+systemTrayImpl\n`).Match(src) {
		t.Fatal("Wails' SystemTray keeps its platform tray in no impl; see trayImpl")
	}
}

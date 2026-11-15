package omarchy

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWidgetFiles(t *testing.T) {
	exe := `/opt/my "apps"/uni-switch`
	files, err := widget(exe)
	if err != nil {
		t.Fatal(err)
	}
	var m struct {
		ID          string
		Kinds       []string
		EntryPoints struct{ BarWidget string }
	}
	if err := json.Unmarshal(files["manifest.json"], &m); err != nil {
		t.Fatal(err)
	}
	if m.ID != WidgetID || len(m.Kinds) != 1 || m.Kinds[0] != "bar-widget" || m.EntryPoints.BarWidget != "Widget.qml" {
		t.Errorf("manifest: %+v", m)
	}
	qml := string(files["Widget.qml"])
	if strings.Contains(qml, "__UNI_SWITCH__") || !strings.Contains(qml, `uniSwitch: "/opt/my \"apps\"/uni-switch"`) {
		t.Errorf("the executable isn't written in, quoted:\n%s", qml)
	}
	for _, s := range []string{`[root.uniSwitch, "panel"]`, `[root.uniSwitch, "app"]`, "Qt.RightButton"} {
		if !strings.Contains(qml, s) {
			t.Errorf("Widget.qml lacks %s", s)
		}
	}
}

// KeepWidget does nothing when the widget isn't in, and nothing (running no
// omarchy command, which isn't here) when it is and is up to date.
func TestKeepWidget(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("PATH", t.TempDir())
	if WidgetOn() {
		t.Fatal("on in an empty config")
	}
	if err := KeepWidget("/usr/bin/uni-switch"); err != nil {
		t.Fatal(err)
	}
	if WidgetOn() {
		t.Fatal("KeepWidget put it in")
	}
	files, _ := widget("/usr/bin/uni-switch")
	os.MkdirAll(widgetDir(), 0o755)
	for name, b := range files {
		os.WriteFile(filepath.Join(widgetDir(), name), b, 0o644)
	}
	if err := KeepWidget("/usr/bin/uni-switch"); err != nil {
		t.Fatalf("up to date: %v", err)
	}
	// moved: it is written again, then the shell is asked to load it
	if err := KeepWidget("/opt/uni-switch"); err == nil || !strings.Contains(err.Error(), "omarchy") {
		t.Fatalf("moved: %v", err)
	}
	if b, _ := os.ReadFile(filepath.Join(widgetDir(), "Widget.qml")); !strings.Contains(string(b), `"/opt/uni-switch"`) {
		t.Error("not rewritten")
	}
}

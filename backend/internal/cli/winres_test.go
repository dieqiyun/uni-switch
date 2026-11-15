package cli

import (
	"encoding/json"
	"os"
	"testing"
)

// Wails gives a Windows window the icon group whose numeric resource ID is
// 3 (NewIconFromResource(GetModuleHandle(""), 3) in its
// webview_window_windows.go), and go-winres makes a numeric ID only of a
// "#n" key. Named "APP", the group had no ID 3, so the taskbar and Alt-Tab
// showed Windows' blank page instead of the bird (#1777).
func TestWindowsIconIsResourceThree(t *testing.T) {
	b, err := os.ReadFile("../../build/windows/winres.json")
	if err != nil {
		t.Fatal(err)
	}
	var res map[string]map[string]json.RawMessage
	if err := json.Unmarshal(b, &res); err != nil {
		t.Fatal(err)
	}
	groups := res["RT_GROUP_ICON"]
	if _, ok := groups["#3"]; !ok {
		names := []string{}
		for k := range groups {
			names = append(names, k)
		}
		t.Fatalf("RT_GROUP_ICON has %q, want the window's icon under \"#3\"", names)
	}
}

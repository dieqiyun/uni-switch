package shortcut

import "testing"

func TestTarget(t *testing.T) {
	for in, want := range map[string]string{
		`C:\Tools\uni-switch-windows-amd64.exe`:          `C:\Tools\uni-switch-windows-amd64.exe`,
		`C:\Tools\uni-switch.exe.old`:                    `C:\Tools\uni-switch.exe`,
		`C:\Tools\uni-switch.exe.old-3`:                  `C:\Tools\uni-switch.exe`,
		`C:\Tools\uni-switch.exe.new`:                    `C:\Tools\uni-switch.exe`,
		`C:\Tools\UNI_SWITCH.EXE.OLD`:                    `C:\Tools\UNI_SWITCH.EXE`,
		`C:\old\uni-switch.exe`:                          `C:\old\uni-switch.exe`,
		`C:\Tools\uni-switch.exe.older`:                  `C:\Tools\uni-switch.exe.older`,
		`/usr/local/bin/uni-switch`:                      `/usr/local/bin/uni-switch`,
		`C:\Users\me\Downloads\uni-switch (1).exe.old-2`: `C:\Users\me\Downloads\uni-switch (1).exe`,
	} {
		if got := Target(in); got != want {
			t.Errorf("Target(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestStale(t *testing.T) {
	exe := "/tools/uni-switch.exe"
	for _, c := range []struct {
		target, dir string
		stale       bool
	}{
		{"", "", true}, // no shortcut yet
		{"/tools/uni-switch.exe", "/tools", false},    // already this one
		{"/TOOLS/uni-switch.EXE", "/Tools/", false},   // the same, in another case
		{"/downloads/uni-switch.exe", "/tools", true}, // the exe was moved
		{"/tools/uni-switch.exe", "/downloads", true}, // starts in another folder
		{"/tools/uni-switch.exe", "", true},           // in no folder
	} {
		if got := Stale(c.target, c.dir, exe); got != c.stale {
			t.Errorf("Stale(%q, %q) = %v, want %v", c.target, c.dir, got, c.stale)
		}
	}
}

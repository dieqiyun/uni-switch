package agent

import (
	"os"
	"strings"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/edit"
)

// claudeStatusAt makes the status line written run exe.
func claudeStatusAt(t *testing.T, exe string) {
	t.Helper()
	old := claudeStatusExe
	claudeStatusExe = func() string { return exe }
	t.Cleanup(func() { claudeStatusExe = old })
}

// steven_4422 on Discord: the 5h and 7d use in Claude Code. The Agents
// page's switch writes settings.json's statusLine to run this uni-switch's
// statusline by its full path, beside what else is there, and takes it
// out again; nothing else in the file moves.
func TestClaudeStatusLineSwitch(t *testing.T) {
	const before = `{
  "model": "opus",
  "env": {"FOO": "1"}
}
`
	home, path := claudeSettings(t, before)
	claudeStatusAt(t, "/Applications/uni-switch.app/Contents/MacOS/uni-switch")
	a := claude(home)
	f := a.Field("statusline")
	if f == nil {
		t.Fatal("Claude Code has no statusline field")
	}
	if f.Get() != "" {
		t.Fatalf("off before: %q", f.Get())
	}
	if opts := f.Options(nil); len(opts) != 1 || opts[0].Value != "on" {
		t.Fatalf("options with no status line: %+v", opts)
	}
	if err := f.Set("on"); err != nil {
		t.Fatal(err)
	}
	get := func(k string) string { v, _ := edit.GetJSON(path, k); return v }
	if get("statusLine.type") != "command" || get("statusLine.command") != "/Applications/uni-switch.app/Contents/MacOS/uni-switch statusline" ||
		get("model") != "opus" || get("env.FOO") != "1" {
		t.Fatalf("on:\n%s", readFile(path))
	}
	if f.Get() != "on" {
		t.Fatalf("Get after on: %q", f.Get())
	}
	// on again writes nothing new
	if err := f.Set("on"); err != nil {
		t.Fatal(err)
	}
	if err := f.Set(""); err != nil {
		t.Fatal(err)
	}
	if _, ok := edit.GetJSONRaw(path, "statusLine"); ok || get("model") != "opus" || get("env.FOO") != "1" {
		t.Fatalf("off:\n%s", readFile(path))
	}
	if err := f.Set("maybe"); err == nil {
		t.Fatal("a value neither on nor off was taken")
	}
}

// A status line of the user's own (claude-hud, a script) is never
// written over or taken out: the switch isn't offered, on is refused, off
// and Disconnect leave it byte for byte.
func TestClaudeStatusLineLeavesTheUsers(t *testing.T) {
	const theirs = `{
  "statusLine": {"type": "command", "command": "bash -c 'node ~/.claude/plugins/claude-hud/dist/index.js'", "padding": 0}
}
`
	home, path := claudeSettings(t, theirs)
	claudeStatusAt(t, "/usr/local/bin/uni-switch")
	a := claude(home)
	f := a.Field("statusline")
	if f.Get() != "" {
		t.Fatalf("the user's read as uni-switch's: %q", f.Get())
	}
	if opts := f.Options(nil); opts != nil {
		t.Fatalf("offered over the user's own: %+v", opts)
	}
	if err := f.Set("on"); err == nil || !strings.Contains(err.Error(), "of its own") {
		t.Fatalf("on over the user's own: %v", err)
	}
	if err := f.Set(""); err != nil {
		t.Fatal(err)
	}
	if err := a.Unwire(); err != nil {
		t.Fatal(err)
	}
	if got := readFile(path); got != theirs {
		t.Fatalf("the user's status line changed:\n%s", got)
	}
}

// Disconnect takes uni-switch's status line out with the rest of uni-switch.
func TestClaudeStatusLineGoesOnDisconnect(t *testing.T) {
	home, path := claudeSettings(t, `{"statusLine": {"type": "command", "command": "uni-switch statusline"}, "model": "opus"}`)
	a := claude(home)
	if a.Field("statusline").Get() != "on" {
		t.Fatal("the reference's hand-written `uni-switch statusline` isn't read as uni-switch's")
	}
	if err := a.Unwire(); err != nil {
		t.Fatal(err)
	}
	if _, ok := edit.GetJSONRaw(path, "statusLine"); ok {
		t.Fatalf("uni-switch's status line left after Disconnect:\n%s", readFile(path))
	}
}

// settings.json that can't be read is unknown, not "no status line": on
// writes nothing over it.
func TestClaudeStatusLineUnreadable(t *testing.T) {
	home, path := claudeSettings(t, `{"model": "opus",,`)
	a := claude(home)
	if err := a.Field("statusline").Set("on"); err == nil {
		t.Fatal("on over a settings.json that can't be read")
	}
	if b, _ := os.ReadFile(path); string(b) != `{"model": "opus",,` {
		t.Fatalf("written over: %s", b)
	}
}

func TestClaudeStatusCommand(t *testing.T) {
	for _, c := range []struct{ exe, goos, want string }{
		{"/Applications/uni-switch.app/Contents/MacOS/uni-switch", "darwin", "/Applications/uni-switch.app/Contents/MacOS/uni-switch statusline"},
		{"/home/me/Jo's Apps/uni-switch-x86_64.AppImage", "linux", `'/home/me/Jo'\''s Apps/uni-switch-x86_64.AppImage' statusline`},
		{`C:\Users\Me Too\AppData\Local\Programs\uni-switch\uni-switch.exe`, "windows", `'C:/Users/Me Too/AppData/Local/Programs/uni-switch/uni-switch.exe' statusline`},
		{`C:\Users\me\uni-switch.exe`, "windows", `C:/Users/me/uni-switch.exe statusline`},
	} {
		got := claudeStatusCommand(c.exe, c.goos)
		if got != c.want {
			t.Errorf("claudeStatusCommand(%q, %s) = %q, want %q", c.exe, c.goos, got, c.want)
		}
		if !claudeStatusOurs(got) {
			t.Errorf("%q, written by uni-switch, isn't read as uni-switch's", got)
		}
	}
	for cmd, want := range map[string]bool{
		"uni-switch statusline":              true,
		"uni-switch statusline claude":       true,
		`"C:/x/uni-switch.exe" statusline`:   true,
		"uni-switch-darwin-arm64 statusline": true,
		"uni-switch":                         false,
		"uni-switch status":                  false,
		"uni-switch statuslinex":             false,
		"notuni-switch statusline":           false,
		"~/.claude/statusline.sh":            false,
		"bash -c 'uni-switch statusline'":    false,
		`'/x/uni-switch`:                     false,
		"":                                   false,
		`/x/my\ apps/uni-switch statusline`:  true,
		"uni-switch statusline; rm -rf ~":    false,
		"uni-switch;statusline":              false,
		"uni-switch statusline && echo hi":   false,
		"uni-switch statusline | tr a b":     false,
		"$(which uni-switch) statusline":     false,
		"'/x/uni-switch' 'statusline'":       true,
	} {
		if got := claudeStatusOurs(cmd); got != want {
			t.Errorf("claudeStatusOurs(%q) = %v, want %v", cmd, got, want)
		}
	}
}

// A WSL distro's Claude Code isn't offered uni-switch's status line, running
// or stopped (a stopped one's fields fall back to uni-switch's models), and on
// is refused there.
func TestWSLClaudeNoStatusLine(t *testing.T) {
	root, _ := claudeDistroHome(t, `{}`)
	saveDeepseek(t)
	d := distro{Name: "Ubuntu", Root: root, Home: "/home/me", Has: map[string]bool{"dir:.claude": true}, Gateway: "172.20.0.1"}
	k := wslKindOf("claude")
	cur := map[string]string{"model": "deepseek/pro"}
	for name, a := range map[string]*Agent{"running": wslAgent(k, d), "stopped": asleep(wslAgent(k, d), k, d)} {
		f := a.Field("statusline")
		if f == nil {
			t.Fatalf("%s distro: no statusline field", name)
		}
		if o := f.Options(cur); len(o) != 0 {
			t.Errorf("%s distro offers %v", name, o)
		}
	}
	if err := wslAgent(k, d).Field("statusline").Set("on"); err == nil {
		t.Error("on taken in a distro")
	}
}

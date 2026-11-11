package cli

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/testenv"
)

// uni-switch daemon: status before an install says how to install; start
// says to install first; a word it doesn't take prints its usage; and a
// `go test` or `go run` build, which Go deletes, is never installed as the
// service. None of them runs the system's service tool.
func TestDaemonCmd(t *testing.T) {
	if runtime.GOOS != "linux" && runtime.GOOS != "darwin" {
		t.Skip("systemd and launchd only")
	}
	helpHome(t)
	dir := t.TempDir()
	calls := filepath.Join(dir, "calls")
	for _, name := range []string{"systemctl", "launchctl", "loginctl"} {
		testenv.Program(t, filepath.Join(dir, name), "#!/bin/sh\necho \""+name+" $*\" >> '"+calls+"'\n")
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	prev := daemonWait
	daemonWait = 0
	t.Cleanup(func() { daemonWait = prev })
	out, err := runOut(t, "daemon")
	if err != nil || !strings.Contains(out, "isn't installed") || !strings.Contains(out, "uni-switch daemon install") {
		t.Errorf("status: %q, %v", out, err)
	}
	if _, err := runOut(t, "daemon", "start"); err == nil || !strings.Contains(err.Error(), "isn't installed") {
		t.Errorf("start: %v", err)
	}
	if _, err := runOut(t, "daemon", "bogus"); err == nil || !strings.Contains(err.Error(), "usage: uni-switch daemon") {
		t.Errorf("bogus: %v", err)
	}
	if _, err := runOut(t, "daemon", "install"); err == nil || !strings.Contains(err.Error(), "go run") {
		t.Errorf("install from a test build: %v", err)
	}
	if b, _ := os.ReadFile(calls); len(b) > 0 {
		t.Errorf("ran %s", b)
	}
}

// uni-switch daemon's words for the page the service serves beside the
// gateway (HuanCheng on Discord): --web on loopback by default, --lan on
// every interface, --addr where told; a command that says nothing of it
// leaves it as it is, and one that contradicts itself is refused.
func TestParseDaemon(t *testing.T) {
	for _, c := range []struct {
		args    []string
		cmd     string
		w       daemonWeb
		wantErr string
	}{
		{nil, "status", daemonWeb{}, ""},
		{[]string{"install"}, "install", daemonWeb{}, ""},
		{[]string{"install", "--web"}, "install", daemonWeb{true, true, "127.0.0.1:3430"}, ""},
		{[]string{"install", "--web", "--lan"}, "", daemonWeb{}, "unknown"},
		{[]string{"install", "--lan"}, "", daemonWeb{}, "unknown"},
		{[]string{"install", "--addr", "10.0.0.5:8080"}, "", daemonWeb{}, "loopback"},
		{[]string{"install", "--web", "--addr=[::1]:3431", "--lan"}, "", daemonWeb{}, "unknown"},
		{[]string{"install", "--no-web"}, "install", daemonWeb{true, false, ""}, ""},
		{[]string{"install", "--no-web", "--web"}, "", daemonWeb{}, "both"},
		{[]string{"install", "--no-web", "--lan"}, "", daemonWeb{}, "unknown"},
		{[]string{"install", "--addr", "3430"}, "", daemonWeb{}, "host:port"},
		{[]string{"install", "--addr"}, "", daemonWeb{}, "unknown"},
		{[]string{"install", "--bogus"}, "", daemonWeb{}, "unknown"},
		{[]string{"web"}, "web", daemonWeb{}, ""},
		{[]string{"web", "status"}, "web", daemonWeb{}, ""},
		{[]string{"web", "on"}, "web", daemonWeb{true, true, "127.0.0.1:3430"}, ""},
		{[]string{"web", "on", "--lan"}, "", daemonWeb{}, "unknown"},
		{[]string{"web", "--lan"}, "", daemonWeb{}, "unknown"},
		{[]string{"web", "off"}, "web", daemonWeb{true, false, ""}, ""},
		{[]string{"web", "off", "--lan"}, "", daemonWeb{}, "unknown"},
		{[]string{"web", "maybe"}, "", daemonWeb{}, "usage"},
		{[]string{"web", "on", "--web"}, "", daemonWeb{}, "unknown"},
		{[]string{"start", "--web"}, "", daemonWeb{}, "usage"},
		{[]string{"status", "x"}, "", daemonWeb{}, "usage"},
	} {
		cmd, w, err := parseDaemon(c.args)
		if c.wantErr != "" {
			if err == nil || !strings.Contains(err.Error(), c.wantErr) {
				t.Errorf("%q: %v, want an error with %q", c.args, err, c.wantErr)
			}
			continue
		}
		if err != nil || cmd != c.cmd || w != c.w {
			t.Errorf("%q: %q %+v %v, want %q %+v", c.args, cmd, w, err, c.cmd, c.w)
		}
	}
}

// The page's key stays what it was across installs, so a browser stays
// signed in: UNI_SWITCH_WEB_KEY when the shell has one, else the service's
// own, else a new one; installed again with nothing said, the service
// keeps its page and key; turned off, neither is kept.
func TestWebOptions(t *testing.T) {
	const prev = "0123456789abcdef0123456789abcdef"
	on := daemonWeb{true, true, "127.0.0.1:3430"}
	if o, err := webOptions(daemonWeb{}, "127.0.0.1:3430", prev, ""); err != nil || o.Web != "127.0.0.1:3430" || o.Key != prev {
		t.Errorf("kept: %+v %v", o, err)
	}
	if o, err := webOptions(daemonWeb{}, "", "", ""); err != nil || o.Web != "" {
		t.Errorf("none kept: %+v %v", o, err)
	}
	if o, err := webOptions(on, "0.0.0.0:3430", prev, ""); err != nil || o.Web != "127.0.0.1:3430" || o.Key != prev {
		t.Errorf("on again: %+v %v", o, err)
	}
	if o, err := webOptions(on, "", "", "my-own-key-0123456789"); err != nil || o.Key != "my-own-key-0123456789" {
		t.Errorf("the shell's key: %+v %v", o, err)
	}
	o, err := webOptions(on, "", "", "")
	if err != nil || len(o.Key) != 32 || o.Web != "127.0.0.1:3430" {
		t.Errorf("new: %+v %v", o, err)
	}
	if o, err := webOptions(daemonWeb{set: true}, "127.0.0.1:3430", prev, ""); err != nil || o.Web != "" || o.Key != "" {
		t.Errorf("off: %+v %v", o, err)
	}
	if _, err := webOptions(on, "", "", "short"); err == nil {
		t.Error("a UNI_SWITCH_WEB_KEY too short to stand between the network and the keys was taken")
	}
}

// uni-switch daemon status and uni-switch daemon web print the page's link, with
// the key, read from the service's file, and say the page is open to
// anyone with the link when it is served on the network.
func TestDaemonStatusPrintsPage(t *testing.T) {
	if runtime.GOOS != "linux" && runtime.GOOS != "darwin" {
		t.Skip("systemd and launchd only")
	}
	home := helpHome(t)
	dir := t.TempDir()
	for _, name := range []string{"systemctl", "launchctl", "loginctl"} {
		testenv.Program(t, filepath.Join(dir, name), "#!/bin/sh\nexit 0\n")
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("UNI_SWITCH_ADDR", "127.0.0.1:1") // no gateway answers
	const key = "0123456789abcdef0123456789abcdef"
	file := filepath.Join(home, ".config", "systemd", "user", "uni-switch.service")
	text := "ExecStart=\"/usr/bin/uni-switch\" serve \"--web=127.0.0.1:3430\"\nEnvironment=\"UNI_SWITCH_WEB_KEY=" + key + "\"\n"
	if runtime.GOOS == "darwin" {
		file = filepath.Join(home, "Library", "LaunchAgents", "io.uni-switch.desktop.serve.plist")
		text = "<array><string>/usr/bin/uni-switch</string><string>serve</string><string>--web=127.0.0.1:3430</string></array>\n<key>UNI_SWITCH_WEB_KEY</key><string>" + key + "</string>\n"
	}
	os.MkdirAll(filepath.Dir(file), 0o755)
	if err := os.WriteFile(file, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"daemon", "status"}, {"daemon", "web"}} {
		out, err := runOut(t, args...)
		if err != nil || !strings.Contains(out, "http://127.0.0.1:3430/?k="+key) {
			t.Errorf("%q: %q, %v", args, out, err)
		}
	}
}

// uni-switch serve takes --web and --web=host:port alone; anything else is
// refused before a gateway is started.
func TestServeArgs(t *testing.T) {
	helpHome(t)
	for _, args := range [][]string{{"serve", "--lan"}, {"serve", "--web=3430"}, {"serve", "web"}} {
		if _, err := runOut(t, args...); err == nil || !strings.Contains(err.Error(), "uni-switch serve") {
			t.Errorf("%q: %v", args, err)
		}
	}
}

package agent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/gateway"
)

// fakeScience is claude-science's daemon as its CLI tells it: status,
// stop and serve --detached, each one noted.
type fakeScience struct {
	dir       string // its data folder, where its lock file is
	running   bool
	pid, port int
	env       []string // the environment the daemon running was started in
	calls     []string
}

func (f *fakeScience) run(env []string, _ string, args ...string) ([]byte, error) {
	f.calls = append(f.calls, strings.Join(args, " "))
	switch args[0] {
	case "status":
		if !f.running {
			return []byte(`{"running":false}`), nil
		}
		return json.Marshal(map[string]any{"running": true, "pid": f.pid, "port": f.port, "version": "0.1.62", "data_dir": "/x"})
	case "stop":
		f.running, f.env = false, nil
	case "serve":
		f.running, f.pid, f.env, f.port = true, f.pid+1, env, 8000
		if i := slices.Index(args, "--port"); i > 0 {
			f.port = 0
			json.Unmarshal([]byte(args[i+1]), &f.port)
		}
	}
	f.lock()
	return nil, nil
}

// lock writes its lock file as the daemon does, removed when it stops.
func (f *fakeScience) lock() {
	path := filepath.Join(f.dir, "operon.lock")
	if !f.running {
		os.Remove(path)
		return
	}
	b, _ := json.Marshal(map[string]any{"pid": f.pid, "version": "0.1.62", "port": f.port, "sandbox_port": f.port + 1})
	os.WriteFile(path, b, 0o600)
}

func (f *fakeScience) base() (string, bool) {
	for _, kv := range f.env {
		if v, ok := strings.CutPrefix(kv, scienceVar+"="); ok {
			return v, true
		}
	}
	return "", false
}

// Claude Science is connected by restarting its daemon with
// ANTHROPIC_BASE_URL set to the gateway, on the port it was on, and by
// nothing else; disconnected by restarting it without the variable, one
// set in uni-switch's own environment too. A daemon uni-switch didn't start is
// drift, never stopped behind the user's back; one not running is started
// while it is connected.
func TestClaudeScienceRestartsItsDaemon(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv(scienceVar, "https://elsewhere.example")
	f := &fakeScience{dir: t.TempDir(), running: true, pid: 40, port: 8123}
	f.lock()
	oldRun, oldBin, oldWait, oldDir := scienceRun, scienceBin, scienceWait, scienceDir
	t.Cleanup(func() { scienceRun, scienceBin, scienceWait, scienceDir = oldRun, oldBin, oldWait, oldDir })
	scienceRun, scienceBin, scienceWait = f.run, func() string { return "/bin/claude-science" }, time.Second
	scienceDir = func() string { return f.dir }
	a, err := Find(ScienceID)
	if err != nil {
		t.Fatal(err)
	}
	field := a.Fields[0]
	if field.Get() != "" || a.Check() != "" || a.Notice() != "" {
		t.Fatal("connected before Connect")
	}

	if err := field.Set(uniSwitchID); err != nil {
		t.Fatal(err)
	}
	if got, ok := f.base(); !ok || got != gateway.URL() {
		t.Errorf("its daemon was started on %q (set %v), want %s", got, ok, gateway.URL())
	}
	if want := []string{"status", "stop", "serve --detached --no-browser --port 8123", "status"}; !slices.Equal(f.calls, want) {
		t.Errorf("ran %q, want %q", f.calls, want)
	}
	if field.Get() != uniSwitchID || a.Check() != "" || !strings.Contains(a.Notice(), "Claude account") {
		t.Errorf("after Connect: %q, check %q, notice %q", field.Get(), a.Check(), a.Notice())
	}
	// set again, as Connect does after connecting (Pick): left running
	f.calls = nil
	if err := field.Set(uniSwitchID); err != nil || !slices.Equal(f.calls, []string{"status"}) {
		t.Errorf("set again: %v, ran %q", err, f.calls)
	}
	if !strings.Contains(a.Launch(), scienceVar+"="+gateway.URL()) && !strings.Contains(a.Launch(), `$env:`+scienceVar+`="`+gateway.URL()) {
		t.Errorf("launch %q", a.Launch())
	}

	// the window, opened while none ran, started its own: drift, and the
	// keeper leaves it running
	f.pid, f.env, f.calls = 77, nil, nil
	f.lock()
	if d := a.Check(); !strings.Contains(d, "without uni-switch's gateway") {
		t.Errorf("a daemon uni-switch didn't start: check %q", d)
	}
	f.calls = nil
	keepScienceOnce()
	if !slices.Equal(f.calls, []string{"status"}) || f.pid != 77 {
		t.Errorf("the keeper touched a daemon running: %q", f.calls)
	}
	// none running (the computer restarted): started on the gateway
	f.running, f.calls = false, nil
	f.lock()
	keepScienceOnce()
	if got, _ := f.base(); !f.running || got != gateway.URL() || a.Check() != "" {
		t.Errorf("not started again on the gateway: %q, %q", f.calls, got)
	}
	if f.port != 8123 {
		t.Errorf("started again on port %d, not the 8123 it was on", f.port)
	}

	// disconnected: running again, without the variable even where
	// uni-switch's own environment has it
	f.calls = nil
	if err := field.Set(""); err != nil {
		t.Fatal(err)
	}
	if got, ok := f.base(); !f.running || ok {
		t.Errorf("after Disconnect: running %v, %s=%q", f.running, scienceVar, got)
	}
	if field.Get() != "" || a.Check() != "" || a.Notice() != "" {
		t.Errorf("still connected after Disconnect")
	}
	if _, err := os.Stat(scienceMarkPath()); !os.IsNotExist(err) {
		t.Errorf("the note is left: %v", err)
	}
	// and the keeper starts nothing now
	f.running, f.calls = false, nil
	f.lock()
	keepScienceOnce()
	if f.running || len(f.calls) != 0 {
		t.Errorf("started while disconnected: %q", f.calls)
	}
	// a disconnect with none running starts none
	writeScienceMark(scienceMark{URL: gateway.URL(), PID: 1})
	if err := field.Set(""); err != nil || f.running {
		t.Errorf("a disconnect started one: %v %q", err, f.calls)
	}
	// and with none installed there is nothing to connect
	scienceBin = func() string { return "" }
	if err := field.Set(uniSwitchID); err == nil || field.Get() != "" || a.detect() {
		t.Errorf("connected with none installed: %v", err)
	}
}

// It is found where its installers put it: the Mac app's bundle, Windows'
// per-user Programs folder, ~/.local/bin.
func TestFindClaudeScience(t *testing.T) {
	home := t.TempDir()
	t.Setenv("PATH", "")
	t.Setenv("LOCALAPPDATA", filepath.Join(home, "AppData", "Local"))
	if got := findScience(home); got != "" {
		t.Fatalf("found %q in an empty home", got)
	}
	var want string
	switch runtime.GOOS {
	case "darwin":
		want = filepath.Join(home, "Applications", "Claude Science.app", "Contents", "Resources", "bin", "claude-science")
	case "windows":
		want = filepath.Join(home, "AppData", "Local", "Programs", "ClaudeScience", "claude-science.exe")
	default:
		want = filepath.Join(home, ".local", "bin", "claude-science")
	}
	os.MkdirAll(filepath.Dir(want), 0o755)
	if err := os.WriteFile(want, []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}
	if got := findScience(home); got != want {
		t.Errorf("found %q, want %q", got, want)
	}
}

package gui

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/proc"
)

var machO = []byte{0xcf, 0xfa, 0xed, 0xfe, 0, 0, 0, 0}

// The rex CLI counts only as a native program: Rex, the Perl deployment
// tool, puts a script named rex on PATH too (#4565). In the app it is
// found under Contents, the app's own Rex and a framework's passed over.
func TestFindRex(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Rex runs on macOS, Linux and FreeBSD")
	}
	dir := t.TempDir()
	write := func(p string, b []byte, mode os.FileMode) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, b, mode); err != nil {
			t.Fatal(err)
		}
	}
	script := filepath.Join(dir, "perl", "rex")
	write(script, []byte("#!/usr/bin/env perl\n"), 0o755)
	native := filepath.Join(dir, "native", "rex")
	write(native, machO, 0o755)
	plain := filepath.Join(dir, "plain", "rex")
	write(plain, machO, 0o644)
	for p, want := range map[string]bool{script: false, native: true, plain: false, filepath.Join(dir, "none"): false} {
		if got := nativeProgram(p); got != want {
			t.Errorf("nativeProgram(%s) = %v, want %v", p, got, want)
		}
	}

	app := filepath.Join(dir, "Rex Beta.app")
	write(filepath.Join(app, "Contents", "MacOS", "Rex"), machO, 0o755)
	write(filepath.Join(app, "Contents", "Frameworks", "X.framework", "rex"), machO, 0o755)
	if got := rexInApp(app); got != "" {
		t.Fatalf("rexInApp took %q: the app's own Rex or a framework's", got)
	}
	cli := filepath.Join(app, "Contents", "Resources", "bin", "rex")
	write(cli, machO, 0o755)
	if got := rexInApp(app); got != cli {
		t.Fatalf("rexInApp = %q, want %q", got, cli)
	}

	// on PATH: the script is passed over for the one in the app
	home := filepath.Join(dir, "home")
	write(filepath.Join(home, "Applications", "Rex Beta.app", "Contents", "Resources", "bin", "rex"), machO, 0o755)
	t.Setenv("HOME", home)
	t.Setenv("PATH", filepath.Dir(script))
	proc.Forget()
	t.Cleanup(proc.Forget)
	if got := lookRex(); !strings.HasPrefix(got, filepath.Join(home, "Applications")) && !strings.HasPrefix(got, "/Applications/") {
		t.Fatalf("lookRex = %q with only Perl's rex on PATH, want the app's", got)
	}
	t.Setenv("PATH", filepath.Dir(native))
	proc.Forget()
	if got := lookRex(); got != native {
		t.Fatalf("lookRex = %q, want the one on PATH %q", got, native)
	}
}

// fakeRex models the rex CLI as Rex's server answered it (snapshot
// 0.0.1793420730, run from outside Rex): a session target is a full ID or
// a label, any case; one matching nothing or more than one exits 3, and so
// does run with no -s unless there is exactly one session. `rex new` takes
// no --label (exit 2). down makes every call exit 4.
type fakeRex struct {
	t        *testing.T
	sessions []fakeRexSession
	ran      [][]string
	down     bool
	seq      int
	bin      string // the last rex run
}

type fakeRexSession struct {
	id, label string
	windows   []string // each tab's label
	ids       []string // each tab's window ID
}

func (f *fakeRex) fail(code int, msg string) ([]byte, error) {
	err := proc.Command("/bin/sh", "-c", fmt.Sprintf("exit %d", code)).Run()
	ee := err.(*exec.ExitError)
	ee.Stderr = []byte(msg)
	return nil, ee
}

func (f *fakeRex) target(s string) (int, string) {
	var hit []int
	for i, x := range f.sessions {
		if x.id == s {
			return i, ""
		}
	}
	for i, x := range f.sessions {
		if strings.EqualFold(x.label, s) {
			hit = append(hit, i)
		}
	}
	switch len(hit) {
	case 0:
		return -1, fmt.Sprintf("Error: session target %q not found", s)
	case 1:
		return hit[0], ""
	}
	return -1, fmt.Sprintf("Error: session target %q is ambiguous", s)
}

func (f *fakeRex) run(_ context.Context, bin string, args ...string) ([]byte, error) {
	f.ran, f.bin = append(f.ran, args), bin
	if f.down {
		return f.fail(4, "Error: server unreachable")
	}
	flags, rest := map[string]string{}, []string{}
	for i := 1; i < len(args); i++ {
		a := args[i]
		if a == "--" {
			rest = append(rest, args[i+1:]...)
			break
		}
		if !strings.HasPrefix(a, "-") {
			rest = append(rest, a)
			continue
		}
		k, v, eq := strings.Cut(strings.TrimLeft(a, "-"), "=")
		if !eq && k != "json" && i+1 < len(args) {
			i++
			v = args[i]
		}
		flags[k] = v
	}
	switch args[0] {
	case "ls":
		var b strings.Builder
		b.WriteString(`{"sessions":[`)
		for i, x := range f.sessions {
			if i > 0 {
				b.WriteString(",")
			}
			fmt.Fprintf(&b, `{"session_id":%q,"label":%q}`, x.id, x.label)
		}
		b.WriteString("]}")
		return []byte(b.String()), nil
	case "new":
		if _, ok := flags["label"]; ok {
			return f.fail(2, "Error: unknown flag: --label")
		}
		f.seq++
		x := fakeRexSession{id: fmt.Sprintf("session:%04d", f.seq), label: rest[0]}
		w := flags["window"]
		if w == "" {
			w = "Window 1"
		}
		x.windows, x.ids = []string{w}, []string{fmt.Sprintf("window:%04d", f.seq)}
		f.sessions = append(f.sessions, x)
		if _, ok := flags["json"]; ok {
			return []byte(fmt.Sprintf(`{"session_id":%q,"initial_windows":[{"window_id":%q,"block_ids":["block:%04d"],"revision":1}]}`, x.id, x.ids[0], f.seq)), nil
		}
		return []byte(x.id + "\n"), nil
	case "client":
		// no app connected: the CLI's own client only
		return []byte(`{"clients":[{"client_id":"client:cli","info":{"kind":"cli","name":"rex"},"principal":{"kind":"peer","transport":"unix","display":"uid 501"},"connected_at":"2026-10-31T23:24:11Z","connection_state":"connected"}]}`), nil
	case "run":
		i := -1
		if s, ok := flags["s"]; ok {
			var msg string
			if i, msg = f.target(s); i < 0 {
				return f.fail(3, msg)
			}
		} else if len(f.sessions) == 1 {
			i = 0
		} else if len(f.sessions) == 0 {
			return f.fail(3, "Error: session target default not found: no sessions")
		} else {
			return f.fail(3, "Error: session target default is ambiguous: multiple sessions are available")
		}
		f.seq++
		w := fmt.Sprintf("window:%04d", f.seq)
		f.sessions[i].windows = append(f.sessions[i].windows, "Window "+w[7:])
		f.sessions[i].ids = append(f.sessions[i].ids, w)
		return []byte(fmt.Sprintf(`{"window_id":%q,"block_ids":["block:%04d"],"revision":2}`, w, f.seq)), nil
	case "window":
		i, msg := f.target(flags["s"])
		if args[1] != "rename" || len(rest) != 3 || i < 0 {
			return f.fail(3, msg)
		}
		j := slices.Index(f.sessions[i].ids, rest[1])
		if j < 0 {
			return f.fail(3, "Error: window target not found")
		}
		f.sessions[i].windows[j] = rest[2]
		return nil, nil
	}
	f.t.Fatalf("fake rex: %q", args)
	return nil, nil
}

func (f *fakeRex) use(t *testing.T) {
	wasRun, wasShow := rexRun, rexShow
	t.Cleanup(func() { rexRun, rexShow = wasRun, wasShow })
	rexRun, rexShow = f.run, func() {}
}

// A session opens in a tab of a Rex session of uni-switch's own, labelled
// uni-switch, whether Rex has no session, one or several (StringKe, #4565: run
// with no -s exits 3 for none and for several alike, and `rex new` has no
// --label). The tab is named for the session; the label and the script
// stay arguments of their own, never parsed by a shell on the way. With
// Rex's server not running (exit 4) it says to open Rex.
func TestOpenInRex(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Rex runs on macOS, Linux and FreeBSD")
	}
	label := `it's "$(touch pwned)"`
	for _, tc := range []struct {
		name     string
		sessions []fakeRexSession
	}{
		{"no session", nil},
		{"one session", []fakeRexSession{{id: "session:a1", label: "work"}}},
		{"two sessions", []fakeRexSession{{id: "session:a1", label: "okcall"}, {id: "session:a2", label: "cswap"}}},
		{"uni-switch's and two more", []fakeRexSession{{id: "session:a1", label: "okcall"}, {id: "session:a2", label: "uni-switch"}, {id: "session:a3", label: "cswap"}}},
		{"two named uni-switch", []fakeRexSession{{id: "session:a1", label: "UNI_SWITCH"}, {id: "session:a2", label: "uni-switch"}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := &fakeRex{t: t, sessions: slices.Clone(tc.sessions)}
			f.use(t)
			for range 2 {
				if err := openInRex("/x/rex", "cd '/tmp' && claude", label); err != nil {
					t.Fatalf("%v; ran %q", err, f.ran)
				}
			}
			var mine []fakeRexSession
			for _, s := range f.sessions {
				if strings.EqualFold(s.label, "uni-switch") {
					mine = append(mine, s)
				}
			}
			own := 0
			for _, s := range tc.sessions {
				if strings.EqualFold(s.label, "uni-switch") {
					own++
				}
			}
			if own == 0 && (len(mine) != 1 || mine[0].label != "uni-switch") {
				t.Fatalf("sessions %+v, want one session labelled uni-switch made", f.sessions)
			}
			if want := len(tc.sessions) + max(0, 1-own); len(f.sessions) != want {
				t.Fatalf("sessions %+v, want %d: uni-switch's made once, then used", f.sessions, want)
			}
			tabs := 0
			for _, s := range f.sessions {
				for _, w := range s.windows {
					if w == label {
						tabs++
					}
				}
			}
			if tabs != 2 {
				t.Fatalf("sessions %+v, want two tabs named %q", f.sessions, label)
			}
			for _, a := range f.ran {
				if a[0] == "run" && !slices.Contains(a, "--label="+label) || a[0] == "new" && !slices.Contains(a, "--window="+label) {
					t.Fatalf("rex %q: the label not an argument of its own", a)
				}
				if i := slices.Index(a, "--"); (a[0] == "run" || a[0] == "new") && (i < 0 || !slices.Equal(a[i:i+3], []string{"--", "/bin/sh", "-c"})) {
					t.Fatalf("rex %q: not the script in sh", a)
				}
			}
		})
	}

	// the session ended between the list and the run: a new one
	f := &fakeRex{t: t, sessions: []fakeRexSession{{id: "session:a1", label: "uni-switch"}}}
	f.use(t)
	wasRun := rexRun
	rexRun = func(ctx context.Context, bin string, args ...string) ([]byte, error) {
		if args[0] == "run" {
			f.sessions = nil
		}
		return wasRun(ctx, bin, args...)
	}
	if err := openInRex("/x/rex", "claude", "Claude Code"); err != nil {
		t.Fatal(err)
	}
	if len(f.sessions) != 1 || f.sessions[0].windows[0] != "Claude Code" {
		t.Fatalf("sessions %+v, want a new uni-switch session", f.sessions)
	}

	f = &fakeRex{t: t, down: true}
	f.use(t)
	if err := openInRex("/x/rex", "claude", ""); err == nil || !strings.Contains(err.Error(), "open Rex") || len(f.ran) != 1 {
		t.Fatalf("server not running: %v, ran %q", err, f.ran)
	}
	f = &fakeRex{t: t}
	f.use(t)
	rexRun = func(context.Context, string, ...string) ([]byte, error) { return f.fail(1, "Error: boom") }
	if err := openInRex("/x/rex", "claude", ""); err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("rex failing: %v, want its output", err)
	}

	// the script, run as Rex runs it
	dir := t.TempDir()
	args := rexScript("cd " + shellQuote(dir) + " && echo ran > out")
	t.Setenv("SHELL", "/usr/bin/true")
	if out, err := proc.Command(args[1], args[2:]...).CombinedOutput(); err != nil {
		t.Fatalf("script: %v %s", err, out)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "out")); string(b) != "ran\n" {
		t.Fatalf("the command didn't run in its folder: %q", b)
	}
	if got := rexLabel("  a\nlong  " + strings.Repeat("x", 80)); !strings.HasPrefix(got, "a long x") || len([]rune(got)) != 60 || rexLabel("") != "uni-switch" {
		t.Fatalf("rexLabel: %q", got)
	}
}

// The same against the real rex, when UNI_SWITCH_TEST_REX names one: a server
// of the test's own (rex starts one for the socket in XDG_RUNTIME_DIR),
// with no session, one and two of others, then uni-switch's among them.
func TestOpenInRexReal(t *testing.T) {
	bin := os.Getenv("UNI_SWITCH_TEST_REX")
	if bin == "" {
		t.Skip("UNI_SWITCH_TEST_REX names no rex program")
	}
	run, err := os.MkdirTemp("/tmp", "rx") // a socket path's room is short
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(run) })
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_RUNTIME_DIR", run)
	t.Setenv("SHELL", "/bin/sh")
	for _, k := range []string{"REX_SESSION", "REX_BLOCK", "REX_SERVER", "REX_CLIENT"} {
		t.Setenv(k, "")
		os.Unsetenv(k)
	}
	wasShow := rexShow
	t.Cleanup(func() { rexShow = wasShow })
	rexShow = func() {}
	rex := func(args ...string) []byte {
		t.Helper()
		out, err := rexRun(context.Background(), bin, args...)
		if err != nil {
			t.Fatalf("rex %q: %v", args, rexError(err))
		}
		return out
	}
	t.Cleanup(func() {
		ids, _ := rexRun(context.Background(), bin, "ls", "-q", "--short=false")
		if f := strings.Fields(string(ids)); len(f) > 0 {
			rexRun(context.Background(), bin, append([]string{"kill"}, f...)...)
		}
		rexRun(context.Background(), bin, "server", "stop")
	})
	type session struct {
		ID    string `json:"session_id"`
		Label string `json:"label"`
	}
	list := func() []session {
		var l struct{ Sessions []session }
		if err := json.Unmarshal(rex("ls", "--json"), &l); err != nil {
			t.Fatal(err)
		}
		return l.Sessions
	}
	tabs := func(id string) []string {
		var w struct {
			Windows []struct{ Label string }
		}
		if err := json.Unmarshal(rex("window", "ls", "-s", id, "--json"), &w); err != nil {
			t.Fatal(err)
		}
		var l []string
		for _, x := range w.Windows {
			l = append(l, x.Label)
		}
		return l
	}
	mine := func() string {
		t.Helper()
		id := ""
		for _, s := range list() {
			if s.Label == "uni-switch" {
				if id != "" {
					t.Fatalf("two uni-switch sessions: %+v", list())
				}
				id = s.ID
			}
		}
		if id == "" {
			t.Fatalf("no uni-switch session: %+v", list())
		}
		return id
	}
	open := func(label string) {
		t.Helper()
		if err := openInRex(bin, "sleep 120", label); err != nil {
			t.Fatalf("with sessions %+v: %v", list(), err)
		}
	}

	open(`it's "$(touch pwned)"`) // no session
	if got := tabs(mine()); !slices.Equal(got, []string{`it's "$(touch pwned)"`}) {
		t.Fatalf("no session: tabs %q", got)
	}
	rex("kill", mine())
	rex("new", "work", "--", "sleep", "120")
	open("one") // one of another's
	if got := tabs(mine()); !slices.Equal(got, []string{"one"}) {
		t.Fatalf("one session: tabs %q", got)
	}
	rex("kill", mine())
	rex("new", "notes", "--", "sleep", "120")
	open("two") // two of others'
	if got := tabs(mine()); !slices.Equal(got, []string{"two"}) {
		t.Fatalf("two sessions: tabs %q", got)
	}
	open("-three") // uni-switch's among them: a tab added to it
	if got := tabs(mine()); !slices.Equal(got, []string{"two", "-three"}) || len(list()) != 3 {
		t.Fatalf("uni-switch's and two: tabs %q, sessions %+v", got, list())
	}
}

// fakeRexBin is a rex program for a test: a shell script that writes each
// call's arguments to dir/log and answers from the files in dir: ls.json
// for `rex ls`, clients.json for `rex client list` (clients.fail makes it
// exit 1), do.exit to refuse an app action with that status and do.hang
// to never answer one.
func fakeRexBin(t *testing.T, dir string) string {
	t.Helper()
	bin := filepath.Join(dir, "rex")
	script := `#!/bin/sh
d=` + shellQuote(dir) + `
{ for a in "$@"; do printf '%s\n' "$a"; done; echo ---; } >> "$d/log"
case "$1" in
ls) cat "$d/ls.json" ;;
run) echo '{"window_id":"window:run1","block_ids":["block:1"],"revision":2}' ;;
window) ;;
new) echo '{"session_id":"session:new1","initial_windows":[{"window_id":"window:new1","block_ids":["block:2"],"revision":1}]}' ;;
client)
	if [ -f "$d/clients.fail" ]; then echo "Error: boom" >&2; exit 1; fi
	cat "$d/clients.json" ;;
-C)
	[ "$3" = do ] || exit 2
	if [ -f "$d/do.hang" ]; then exec sleep 30; fi
	if [ -f "$d/do.exit" ]; then echo "Error: client refused the action: remote control is off" >&2; exit "$(cat "$d/do.exit")"; fi
	echo '{"performed_by":"'"$2"'"}' ;;
*) echo "Error: unknown command $1" >&2; exit 2 ;;
esac
`
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return bin
}

// After opening the tab, uni-switch asks the Rex app to go to it: a tab added
// from outside only shows in the app's sidebar, and `open -b` only brings
// the window forward (StringKe, #4565). The app client is the one of kind
// app attached to uni-switch's session, else the last app connected, never a
// lost one; it is asked with `rex -C <client> do session.select`, with the
// session and the new tab, for a new session and an added tab alike. Remote
// Control off (a refused do), no app, a failed client list or an app that
// never answers leave the tab open and the open a success. Runs a fake rex
// program, a script, so the arguments go through a real exec.
func TestOpenInRexSelectsTheTab(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Rex runs on macOS, Linux and FreeBSD")
	}
	wasShow, wasWait := rexShow, rexSelectWait
	t.Cleanup(func() { rexShow, rexSelectWait = wasShow, wasWait })
	rexShow = func() {}
	rexSelectWait = 300 * time.Millisecond

	const (
		none    = `{"sessions":[{"session_id":"session:w","label":"work"}]}`
		mine    = `{"sessions":[{"session_id":"session:w","label":"work"},{"session_id":"session:m","label":"uni-switch"}]}`
		cli     = `{"client_id":"client:cli","info":{"kind":"cli","name":"rex"},"principal":{"kind":"peer","transport":"unix","display":"uid 501"},"connected_at":"2026-10-31T23:24:11Z","connection_state":"connected"}`
		appW    = `{"client_id":"client:appW","info":{"kind":"app","name":"Rex.app","platform":"macos"},"principal":{"kind":"peer","transport":"unix","display":"uid 501"},"connected_at":"2026-10-31T20:00:00Z","connection_state":"connected","session_ids":["session:w"]}`
		appM    = `{"client_id":"client:appM","info":{"kind":"app","name":"Rex.app","platform":"ios"},"principal":{"kind":"tailscale","transport":"tailscale","display":"me"},"connected_at":"2026-10-31T19:00:00Z","connection_state":"connected","session_ids":["session:m","session:w"]}`
		appLost = `{"client_id":"client:lost","info":{"kind":"app","name":"Rex.app"},"principal":{"kind":"peer","transport":"unix","display":"uid 501"},"connected_at":"2026-10-31T21:00:00Z","connection_state":"lost","connection_lost_at":"2026-10-31T22:00:00Z"}`
	)
	clients := func(c ...string) string { return `{"clients":[` + strings.Join(c, ",") + `]}` }
	for _, tc := range []struct {
		name, ls, clients string
		files             map[string]string
		want              []string // the do call, nil for none
	}{
		{"new session, an app on another", none, clients(appM, cli, appW, appLost), nil,
			[]string{"-C", "client:appW", "do", "session.select", "session_id=session:new1", "window_id=window:new1"}},
		{"uni-switch's session, the app attached to it", mine, clients(appM, cli, appW, appLost), nil,
			[]string{"-C", "client:appM", "do", "session.select", "session_id=session:m", "window_id=window:run1"}},
		{"only a lost app", mine, clients(cli, appLost), nil, nil},
		{"no app", none, clients(cli), nil, nil},
		{"client list fails", mine, clients(appW), map[string]string{"clients.fail": ""}, nil},
		{"remote control off", mine, clients(appW), map[string]string{"do.exit": "1"},
			[]string{"-C", "client:appW", "do", "session.select", "session_id=session:m", "window_id=window:run1"}},
		{"the app never answers", none, clients(appW), map[string]string{"do.hang": ""},
			[]string{"-C", "client:appW", "do", "session.select", "session_id=session:new1", "window_id=window:new1"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			files := map[string]string{"ls.json": tc.ls, "clients.json": tc.clients}
			maps.Copy(files, tc.files)
			for k, v := range files {
				if err := os.WriteFile(filepath.Join(dir, k), []byte(v), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			bin := fakeRexBin(t, dir)
			start := time.Now()
			if err := openInRex(bin, "claude", "Claude Code"); err != nil {
				t.Fatalf("openInRex: %v, want the open to stand", err)
			}
			if d := time.Since(start); d > 10*time.Second {
				t.Fatalf("openInRex took %v", d)
			}
			b, _ := os.ReadFile(filepath.Join(dir, "log"))
			var calls [][]string
			for _, c := range strings.Split(strings.TrimSuffix(string(b), "---\n"), "---\n") {
				calls = append(calls, strings.Split(strings.TrimSuffix(c, "\n"), "\n"))
			}
			opened := false
			var dos [][]string
			for _, c := range calls {
				switch c[0] {
				case "run", "new":
					opened = true
				case "-C":
					dos = append(dos, c)
				}
			}
			if !opened {
				t.Fatalf("no tab opened: %q", calls)
			}
			if tc.want == nil {
				if len(dos) != 0 {
					t.Fatalf("asked %q, want no app asked", dos)
				}
				return
			}
			if len(dos) != 1 || !slices.Equal(dos[0], tc.want) {
				t.Fatalf("asked %q, want %q", dos, tc.want)
			}
		})
	}
}

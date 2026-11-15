package gui

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/dieqiyun/uni-switch/backend/internal/proc"
)

// Rex (superlogical.com/rex, a terminal multiplexer) claims no .command
// file, only public.item and rex://, so Launch Services never lists it as
// a terminal and `open -a` can't hand it a script (StringKe, #4565). Its
// own CLI opens a command in it, in a tab of a Rex session of uni-switch's own
// (openInRex). Rex is offered when its CLI is found, on PATH or in the app.
const rexBundleID = "com.superlogical.rex"

// rexFound is the rex CLI, or "" with none: a var so a test puts a fake.
var rexFound = findRex

var rexMemo struct {
	sync.Mutex
	at   time.Time
	path string
}

// findRex is the rex program: one on PATH, else one in a Rex app in
// /Applications or ~/Applications (the app puts its own on PATH only in
// the terminals it opens). Only a native program counts: Rex, the Perl
// deployment tool, installs a script named rex too. Kept a minute, as
// the settings ask on every state.
func findRex() string {
	rexMemo.Lock()
	defer rexMemo.Unlock()
	if !rexMemo.at.IsZero() && time.Since(rexMemo.at) < time.Minute {
		return rexMemo.path
	}
	rexMemo.at, rexMemo.path = time.Now(), lookRex()
	return rexMemo.path
}

func lookRex() string {
	if p, err := proc.Look("rex"); err == nil && nativeProgram(p) {
		return p
	}
	var apps []string
	for _, dir := range []string{"/Applications", filepath.Join(os.Getenv("HOME"), "Applications")} {
		m, _ := filepath.Glob(filepath.Join(dir, "Rex*.app"))
		apps = append(apps, m...)
	}
	for _, app := range apps {
		if p := rexInApp(app); p != "" {
			return p
		}
	}
	return ""
}

// rexInApp is an executable named rex under app's Contents, not in a
// framework, at most four folders down.
func rexInApp(app string) string {
	root := filepath.Join(app, "Contents")
	found := ""
	filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil || found != "" {
			return fs.SkipDir
		}
		if d.IsDir() {
			rel, _ := filepath.Rel(root, p)
			if strings.Count(rel, string(filepath.Separator)) >= 4 || strings.HasSuffix(d.Name(), ".framework") || d.Name() == "Frameworks" {
				return fs.SkipDir
			}
			return nil
		}
		if d.Name() == "rex" && nativeProgram(p) {
			found = p
			return fs.SkipAll
		}
		return nil
	})
	return found
}

// nativeProgram says p is an executable Mach-O or ELF file, not a script.
func nativeProgram(p string) bool {
	st, err := os.Stat(p)
	if err != nil || !st.Mode().IsRegular() || st.Mode().Perm()&0o111 == 0 {
		return false
	}
	f, err := os.Open(p)
	if err != nil {
		return false
	}
	defer f.Close()
	head := make([]byte, 4)
	if _, err := f.Read(head); err != nil {
		return false
	}
	for _, magic := range [][]byte{
		{0xcf, 0xfa, 0xed, 0xfe}, {0xce, 0xfa, 0xed, 0xfe}, // Mach-O 64, 32
		{0xca, 0xfe, 0xba, 0xbe}, // universal
		{0x7f, 'E', 'L', 'F'},
	} {
		if bytes.Equal(head, magic) {
			return true
		}
	}
	return false
}

// withRex adds Rex to the terminals found when its CLI is, as the program
// that opens it.
func withRex(found terminalDiscovery) terminalDiscovery {
	for _, app := range found.Apps {
		if app.ID == rexBundleID {
			return found
		}
	}
	if p := rexFound(); p != "" {
		found.Apps = append(found.Apps, terminalApp{ID: rexBundleID, Name: "Rex", Path: p})
	}
	return found
}

// rexScript is what a Rex tab runs: the command in sh, as the .command
// file runs it, then the login shell, so the tab stays when the agent
// quits. It and the label reach rex as arguments of their own, never
// parsed by a shell on the way.
func rexScript(command string) []string {
	return []string{"--", "/bin/sh", "-c", command + "\nexec \"${SHELL:-/bin/zsh}\" -l"}
}

// rexLabel is a tab's label: the session's or agent's name on one line,
// cut to 60 characters; "uni-switch" when it has none.
func rexLabel(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if s == "" {
		return "uni-switch"
	}
	if utf8.RuneCountInString(s) > 60 {
		s = string([]rune(s)[:59]) + "…"
	}
	return s
}

// rexRun runs the rex CLI and gives its standard output; a failure is an
// *exec.ExitError holding what it wrote to standard error. A var so a test
// runs none.
var rexRun = func(ctx context.Context, bin string, args ...string) ([]byte, error) {
	return proc.CommandContext(ctx, bin, args...).Output()
}

// rexShow brings Rex forward, a var so a test opens nothing.
var rexShow = func() { proc.Command("open", "-b", rexBundleID).Run() }

// rexSessionLabel labels the Rex session uni-switch opens its tabs in.
const rexSessionLabel = "uni-switch"

// openInRex opens command in a new tab of Rex, through its CLI at bin.
//
// Outside Rex the CLI has no session of its own, and `rex run` with no -s
// takes the only session there is: with none, or more than one (the usual
// case), it exits 3, which means a target not found or ambiguous (StringKe,
// #4565). Nor can the CLI tell which session the app shows. So uni-switch keeps
// a session of its own, labelled uni-switch: found by `rex ls`, given by its full
// ID (a label can be shared), a tab added with `rex run -s` and named for
// the session or agent with `rex window rename` (run's --label names the
// block, not the tab). With none, `rex new uni-switch --window <label>` makes it,
// its first tab so named; so does a 3 from run, the session having ended
// since the list.
//
// A tab added from outside shows only in the app's sidebar: the app keeps
// the tab it was on, and `open -b` only brings its window forward
// (StringKe, #4565). So the app client is then asked to go to the new tab
// (rexSelect).
func openInRex(bin, command, label string) error {
	label = rexLabel(label)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	id, err := rexOwnSession(ctx, bin)
	if err != nil {
		return rexError(err)
	}
	if id != "" {
		out, err := rexRun(ctx, bin, append([]string{"run", "-s", id, "--label=" + label, "--json"}, rexScript(command)...)...)
		if err == nil {
			var made struct {
				WindowID string `json:"window_id"`
			}
			if json.Unmarshal(out, &made) == nil && made.WindowID != "" {
				// the tab's name only: the tab is open either way
				rexRun(ctx, bin, "window", "rename", "-s", id, "--", made.WindowID, label)
			}
			rexShow()
			rexSelect(ctx, bin, id, made.WindowID)
			return nil
		}
		if rexExit(err) != 3 {
			return rexError(err)
		}
	}
	out, err := rexRun(ctx, bin, append([]string{"new", rexSessionLabel, "--window=" + label, "--json"}, rexScript(command)...)...)
	if err != nil {
		return rexError(err)
	}
	rexShow()
	var made struct {
		SessionID string `json:"session_id"`
		Windows   []struct {
			WindowID string `json:"window_id"`
		} `json:"initial_windows"`
	}
	if json.Unmarshal(out, &made) == nil && made.SessionID != "" {
		w := ""
		if len(made.Windows) > 0 {
			w = made.Windows[0].WindowID
		}
		rexSelect(ctx, bin, made.SessionID, w)
	}
	return nil
}

// rexSelectWait bounds rexSelect. A client that doesn't answer an action
// keeps `rex do` waiting 10s ("did not answer within 10s", tried on rex
// 0.0.1793420730 with a CLI client), and the GUI waits for the open; the
// window is already forward by then. A var so a test waits less.
var rexSelectWait = 3 * time.Second

// rexSelect asks the Rex app to show window (a tab) of session, with
// `rex -C <app client> do session.select session_id=… window_id=…`, the
// app's own "Go to Session" action. The app client is the one of kind app
// in `rex client list --json` attached to session, else the last app to
// connect; one whose connection is lost is passed over. An app action is
// refused unless Remote Control is on in Rex's settings, and there may be
// no app at all (the CLI starts a server of its own): the tab is open
// either way, so a missing client, a refusal or a slow answer is let go.
// It runs after rexShow, so the window comes forward without waiting.
func rexSelect(ctx context.Context, bin, session, window string) {
	ctx, cancel := context.WithTimeout(ctx, rexSelectWait)
	defer cancel()
	out, err := rexRun(ctx, bin, "client", "list", "--json")
	if err != nil {
		return
	}
	var list struct {
		Clients []struct {
			ID    string `json:"client_id"`
			State string `json:"connection_state"`
			Info  struct {
				Kind string `json:"kind"`
			} `json:"info"`
			Sessions []string `json:"session_ids"`
		} `json:"clients"`
	}
	if json.Unmarshal(out, &list) != nil {
		return
	}
	app := ""
	for _, c := range list.Clients {
		if c.ID == "" || c.Info.Kind != "app" || c.State == "lost" {
			continue
		}
		if slices.Contains(c.Sessions, session) {
			app = c.ID
			break
		}
		app = c.ID // in the order they connected: the last one so far
	}
	if app == "" {
		return
	}
	args := []string{"-C", app, "do", "session.select", "session_id=" + session}
	if window != "" {
		args = append(args, "window_id="+window)
	}
	rexRun(ctx, bin, args...)
}

// rexOwnSession is the full ID of uni-switch's own Rex session, "" with none:
// one labelled exactly uni-switch, else one so labelled in another case.
func rexOwnSession(ctx context.Context, bin string) (string, error) {
	out, err := rexRun(ctx, bin, "ls", "--json")
	if err != nil {
		return "", err
	}
	var list struct {
		Sessions []struct {
			ID    string `json:"session_id"`
			Label string `json:"label"`
		} `json:"sessions"`
	}
	if err := json.Unmarshal(out, &list); err != nil {
		return "", fmt.Errorf("rex ls --json: %w", err)
	}
	id := ""
	for _, s := range list.Sessions {
		if s.ID == "" {
			continue
		}
		if s.Label == rexSessionLabel {
			return s.ID, nil
		}
		if id == "" && strings.EqualFold(s.Label, rexSessionLabel) {
			id = s.ID
		}
	}
	return id, nil
}

// rexExit is the exit status of a rex that ran and failed, else -1.
func rexExit(err error) int {
	if ee := (*exec.ExitError)(nil); errors.As(err, &ee) {
		return ee.ExitCode()
	}
	return -1
}

// rexError says why rex failed: its own words, or that Rex's server
// couldn't be reached (exit 4).
func rexError(err error) error {
	if rexExit(err) == 4 {
		return errors.New("rex couldn't reach the Rex server: open Rex and try again")
	}
	if ee := (*exec.ExitError)(nil); errors.As(err, &ee) {
		if msg := strings.TrimSpace(string(ee.Stderr)); msg != "" {
			return fmt.Errorf("rex: %s", msg)
		}
	}
	return fmt.Errorf("rex: %w", err)
}

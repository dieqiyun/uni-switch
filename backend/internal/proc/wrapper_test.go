//go:build !windows

package proc

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/testenv"
)

// miseWrapperScript is the script deluxe (Discord, Omarchy Linux) had in
// ~/.local/bin/claude: its last line, as they gave it, runs claude
// through mise, and inside `mise x` that is the script again.
const miseWrapperScript = `#!/usr/bin/env bash
# claude, through mise
set -e
exec mise x "claude" -- "claude" "$@"
`

// wrapperFixture is a machine with the wrapper first on PATH, mise's real
// claude in its installs folder, and, when withMise, a fake mise beside
// which answers `mise which claude` with it and writes down each ask.
type wrapperFixture struct {
	dir, wrapper, real, asked string
}

func newWrapperFixture(t *testing.T, wrapper, mise string) wrapperFixture {
	t.Helper()
	resetUnwrapped(t)
	// a fake mise that answers is given a minute, not miseWait: a shell
	// started on a loaded machine can take seconds to run its first line
	was := miseAsk
	miseAsk = func() (context.Context, context.CancelFunc) {
		return context.WithTimeout(context.Background(), time.Minute)
	}
	t.Cleanup(func() { miseAsk = was })
	d := t.TempDir()
	f := wrapperFixture{
		dir:     d,
		wrapper: filepath.Join(d, "local-bin", "claude"),
		real:    filepath.Join(d, "mise", "installs", "claude", "2.1.0", "bin", "claude"),
		asked:   filepath.Join(d, "asked"),
	}
	for _, x := range []string{filepath.Dir(f.wrapper), filepath.Dir(f.real), filepath.Join(d, "mise-bin")} {
		if err := os.MkdirAll(x, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	testenv.Program(t, f.wrapper, wrapper)
	testenv.Program(t, f.real, "#!/bin/sh\necho 2.1.0 '(Claude Code)'\n")
	path := filepath.Dir(f.wrapper)
	if mise != "" {
		mise = strings.NewReplacer("{asked}", f.asked, "{real}", f.real, "{wrapper}", f.wrapper).Replace(mise)
		testenv.Program(t, filepath.Join(d, "mise-bin", "mise"), mise)
		path += string(os.PathListSeparator) + filepath.Join(d, "mise-bin")
	}
	// /bin for the scripts' own sh, nothing of the host's mise
	t.Setenv("PATH", path+string(os.PathListSeparator)+"/bin")
	return f
}

// fakeMise answers `mise which claude` with the real program; `mise x`
// does what mise does, running the command found on PATH, which is the
// wrapper again.
const fakeMise = `#!/bin/sh
echo "$*" >> '{asked}'
if [ "$1 $2" = "which claude" ]; then echo '{real}'; exit 0; fi
exit 1
`

func resetUnwrapped(t *testing.T) {
	t.Helper()
	clear := func() {
		unwrapped.Lock()
		unwrapped.m = map[unwrapKey]unwrapEntry{}
		unwrapped.Unlock()
	}
	clear()
	t.Cleanup(clear)
}

func (f wrapperFixture) asks(t *testing.T) string {
	t.Helper()
	b, _ := os.ReadFile(f.asked)
	return string(b)
}

// The wrapper is run as the program mise says it stands for, which runs
// Claude Code, rather than as itself, which runs itself for ever.
func TestFindToolUnwrapsMiseWrapper(t *testing.T) {
	f := newWrapperFixture(t, miseWrapperScript, fakeMise)
	if got := FindTool("claude"); got != f.real {
		t.Fatalf("FindTool(claude) = %q, want mise's %q", got, f.real)
	}
	if got, err := LookPath("claude"); err != nil || got != f.real {
		t.Fatalf("LookPath(claude) = %q, %v, want %q", got, err, f.real)
	}
	// asked once; the answer is kept while the wrapper is unchanged
	if a := f.asks(t); a != "which claude\n" {
		t.Fatalf("mise was asked %q", a)
	}
}

// Without mise to ask, the wrapper is what it was before.
func TestFindToolKeepsWrapperWithoutMise(t *testing.T) {
	f := newWrapperFixture(t, miseWrapperScript, "")
	if got := FindTool("claude"); got != f.wrapper {
		t.Fatalf("FindTool(claude) = %q, want the wrapper %q", got, f.wrapper)
	}
}

// A mise that fails, names the wrapper itself, or doesn't answer leaves
// the wrapper as it was; one that doesn't answer is killed.
func TestFindToolKeepsWrapperWhenMiseDoesntSay(t *testing.T) {
	for name, mise := range map[string]string{
		"fails":       "#!/bin/sh\necho \"$*\" >> '{asked}'\necho 'mise ERROR claude not installed' >&2\nexit 1\n",
		"names it":    "#!/bin/sh\necho \"$*\" >> '{asked}'\necho '{wrapper}'\n",
		"names a dir": "#!/bin/sh\necho \"$*\" >> '{asked}'\necho /bin\n",
	} {
		t.Run(name, func(t *testing.T) {
			f := newWrapperFixture(t, miseWrapperScript, mise)
			if got := FindTool("claude"); got != f.wrapper {
				t.Fatalf("FindTool(claude) = %q, want the wrapper %q", got, f.wrapper)
			}
			if f.asks(t) == "" {
				t.Fatal("mise wasn't asked")
			}
		})
	}
	t.Run("hangs", func(t *testing.T) {
		f := newWrapperFixture(t, miseWrapperScript, "#!/bin/sh\necho $$ > '{asked}'\nexec sleep 60\n")
		// mise is given up on once the fake has written its pid, as
		// miseWait's end does to a mise that doesn't answer; not at a
		// time a loaded machine passes before the fake's first line runs
		// (#1838: 300ms killed it unasked)
		var wg sync.WaitGroup
		t.Cleanup(wg.Wait)
		gaveUp := make(chan time.Time, 1)
		miseAsk = func() (context.Context, context.CancelFunc) {
			ctx, cancel := context.WithCancel(context.Background())
			wg.Add(1)
			go func() {
				defer wg.Done()
				deadline := time.Now().Add(time.Minute)
				for strings.TrimSpace(f.asks(t)) == "" && ctx.Err() == nil && time.Now().Before(deadline) {
					time.Sleep(10 * time.Millisecond)
				}
				select {
				case gaveUp <- time.Now():
				default:
				}
				cancel()
			}()
			return ctx, cancel
		}
		if got := FindTool("claude"); got != f.wrapper {
			t.Fatalf("FindTool(claude) = %q, want the wrapper %q", got, f.wrapper)
		}
		// given up on, FindTool doesn't wait out the fake's 60s sleep
		if d := time.Since(<-gaveUp); d > 30*time.Second {
			t.Fatalf("FindTool returned %s after mise was given up on", d)
		}
		pid := strings.TrimSpace(f.asks(t))
		if pid == "" {
			t.Fatal("mise wasn't asked")
		}
		// the ask was killed with its group: nothing of it outlives the test
		deadline := time.Now().Add(time.Minute)
		for alive(pid) {
			if time.Now().After(deadline) {
				t.Fatalf("mise %s still running", pid)
			}
			time.Sleep(20 * time.Millisecond)
		}
	})
}

func alive(pid string) bool {
	_, err := os.Stat("/proc/" + pid)
	if err == nil {
		return true
	}
	if !os.IsNotExist(err) || runtimeHasProcFS() {
		return false
	}
	// no /proc (macOS): ps says
	out, _ := Command("ps", "-o", "stat=", "-p", pid).Output()
	s := strings.TrimSpace(string(out))
	return s != "" && !strings.HasPrefix(s, "Z")
}

func runtimeHasProcFS() bool {
	_, err := os.Stat("/proc/self")
	return err == nil
}

// A script that isn't such a wrapper is run as it is, and mise isn't
// asked: the user's own fix (the program `mise which` gave, in "$bin"),
// a launcher of node, a program.
func TestFindToolLeavesOtherScripts(t *testing.T) {
	for name, script := range map[string]string{
		"deluxe's fix": "#!/usr/bin/env bash\nbin=$(mise which claude) || exit 1\nexec mise x \"claude\" -- \"$bin\" \"$@\"\n",
		"node":         "#!/bin/sh\nexec node /opt/claude/cli.js \"$@\"\n",
		"another tool": "#!/bin/sh\nexec mise x node -- codex \"$@\"\n",
		"commented":    "#!/bin/sh\n# exec mise x claude -- claude \"$@\"\nexec /opt/claude/claude \"$@\"\n",
	} {
		t.Run(name, func(t *testing.T) {
			f := newWrapperFixture(t, script, fakeMise)
			if got := FindTool("claude"); got != f.wrapper {
				t.Fatalf("FindTool(claude) = %q, want it as it is, %q", got, f.wrapper)
			}
			if a := f.asks(t); a != "" {
				t.Fatalf("mise was asked %q", a)
			}
		})
	}
}

// The ways a wrapper names mise and its command.
func TestParseMiseWrapper(t *testing.T) {
	for _, c := range []struct {
		script     string
		ok         bool
		mise, tool string
	}{
		{miseWrapperScript, true, "mise", "claude"},
		{"#!/bin/sh\nexec mise exec claude@latest -- claude \"$@\"\n", true, "mise", "claude@latest"},
		{"#!/bin/sh\nexec /usr/bin/mise x -q 'npm:@anthropic-ai/claude-code' -- 'claude' \"$@\"\n", true, "/usr/bin/mise", "npm:@anthropic-ai/claude-code"},
		{"#!/bin/zsh\n  exec rtx x claude -- claude \"$@\" # old name\n", true, "rtx", "claude"},
		{"#!/bin/sh\nmise x -- claude \"$@\"\n", true, "mise", ""},
		{"#!/bin/sh\nexec mise x claude -- /opt/claude \"$@\"\n", false, "", ""},
		{"#!/bin/sh\nexec mise run claude -- claude\n", false, "", ""},
		{"#!/bin/sh\nexec asdf exec \"claude\" \"$@\"\n", false, "", ""},
	} {
		w, ok := parseMiseWrapper([]byte(c.script), "claude")
		if ok != c.ok || w.mise != c.mise || w.tool != c.tool {
			t.Errorf("%q: %+v %v, want %q %q %v", c.script, w, ok, c.mise, c.tool, c.ok)
		}
	}
}

// When plain `mise which` doesn't know the command (the tool isn't in the
// home's config), the wrapper's own tool is asked for.
func TestFindToolAsksWithWrappersTool(t *testing.T) {
	f := newWrapperFixture(t, "#!/bin/sh\nexec mise x claude@2.1.0 -- claude \"$@\"\n",
		"#!/bin/sh\necho \"$*\" >> '{asked}'\n[ \"$*\" = \"which --tool claude@2.1.0 claude\" ] || exit 1\necho '{real}'\n")
	if got := FindTool("claude"); got != f.real {
		t.Fatalf("FindTool(claude) = %q, want %q", got, f.real)
	}
	if a := f.asks(t); a != "which claude\nwhich --tool claude@2.1.0 claude\n" {
		t.Fatalf("mise was asked %q", a)
	}
}

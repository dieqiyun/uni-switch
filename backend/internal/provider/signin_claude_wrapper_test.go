package provider

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/claudecode"
	"github.com/dieqiyun/uni-switch/backend/internal/testenv"
)

// miseWrapped puts deluxe's (Discord, Omarchy Linux) ~/.local/bin/claude
// first on PATH: a script ending `exec mise x "claude" -- "claude" "$@"`,
// beside a fake mise which, as mise does, runs the command it finds on
// PATH, so the script runs itself until killed. answers says whether the
// fake answers `mise which claude` with exe, Claude Code itself. The
// Claude found is the one uni-switch looks up, not a stand-in.
func miseWrapped(t *testing.T, exe string, answers bool) string {
	t.Helper()
	d := t.TempDir()
	bin, miseBin := filepath.Join(d, "local-bin"), filepath.Join(d, "mise-bin")
	os.MkdirAll(bin, 0o755)
	os.MkdirAll(miseBin, 0o755)
	wrapper := filepath.Join(bin, "claude")
	testenv.Program(t, wrapper, "#!/bin/sh\nexec mise x \"claude\" -- \"claude\" \"$@\"\n")
	which := "exit 1"
	if answers {
		which = fmt.Sprintf("echo '%s'; exit 0", exe)
	}
	testenv.Program(t, filepath.Join(miseBin, "mise"), fmt.Sprintf(`#!/bin/sh
if [ "$1 $2" = "which claude" ]; then %s; fi
[ "$1" = x ] || exit 1
while [ $# -gt 0 ] && [ "$1" != "--" ]; do shift; done
shift
# the test is over: end rather than run on
[ -d '%s' ] || exit 3
exec "$@"
`, which, d))
	t.Setenv("PATH", bin+string(os.PathListSeparator)+miseBin+string(os.PathListSeparator)+os.Getenv("PATH"))
	old := claudeExecutable
	claudeExecutable = claudecode.Find
	t.Cleanup(func() { claudeExecutable = old })
	return wrapper
}

// The Claude sign-in runs the Claude Code mise names, and its link shows,
// rather than the wrapper, which never starts it.
func TestClaudeSignInThroughMiseWrapper(t *testing.T) {
	claudeHome(t)
	noAnthropic(t)
	exe := fakeClaudeLogin(t, fakeClaudeAccount{email: "mise@example.com", plan: "max", refresh: "sk-ant-ort01-mise"}, "", false)
	// linkWait stays the sign-in's own 30s: the link comes as soon as
	// Claude Code prints it, so the wait only costs time on a busy machine,
	// where sh, the fake mise and sh again can take seconds to start (#1802)
	miseWrapped(t, exe, true)

	st, err := StartSignIn("claude")
	if err != nil {
		t.Fatal(err)
	}
	if !st.PasteCode || !strings.Contains(st.URL, "platform.claude.com") {
		t.Fatalf("state %+v", st)
	}
	if err := SubmitSignInCallback(st.ID, "the-code#st"); err != nil {
		t.Fatal(err)
	}
	if st = waitDone(t, st.ID); st.State != "done" || st.User != "mise@example.com" {
		t.Fatalf("state %+v", st)
	}
}

// When mise can't say, the wrapper is run as before; it prints nothing,
// and the sign-in ends after linkWait saying which program it ran and how
// to see what it does, not waiting for ever.
func TestClaudeSignInWrapperThatNeverStarts(t *testing.T) {
	claudeHome(t)
	noAnthropic(t)
	exe := fakeClaudeLogin(t, fakeClaudeAccount{email: "x@example.com", plan: "pro", refresh: "sk-ant-ort01-x"}, "", false)
	wrapper := miseWrapped(t, exe, false)
	was := linkWait
	linkWait = 2 * time.Second
	t.Cleanup(func() { linkWait = was })

	start := time.Now()
	_, err := StartSignIn("claude")
	if err == nil {
		t.Fatal("signed in through a wrapper that never starts Claude Code")
	}
	// it gives up when linkWait is over, not before, and not long after:
	// the upper bound is for a busy machine, the point is it isn't for ever
	if d := time.Since(start); d < linkWait || d > linkWait+time.Minute {
		t.Fatalf("gave up after %s, linkWait %s", d, linkWait)
	}
	if !strings.Contains(err.Error(), wrapper) || !strings.Contains(err.Error(), "printed nothing") || !strings.Contains(err.Error(), "in a terminal") {
		t.Fatalf("error %q doesn't name %s and what to do", err, wrapper)
	}
}

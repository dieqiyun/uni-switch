package gateway

import (
	"context"

	"os"
	"path/filepath"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/agentenv"
	"github.com/dieqiyun/uni-switch/backend/internal/budget"
	"github.com/dieqiyun/uni-switch/backend/internal/gwbudget"
	"github.com/dieqiyun/uni-switch/backend/internal/gwtool"
	"github.com/dieqiyun/uni-switch/backend/internal/provider"
	"github.com/dieqiyun/uni-switch/backend/internal/routes"
	"github.com/dieqiyun/uni-switch/backend/internal/testenv"
)

// standInsEnv names the folder of a test binary's stand-in CLIs, for a
// test binary it starts again.
const standInsEnv = "UNI_SWITCH_GATEWAY_TEST_STAND_INS"

// Isolate every process, including child test binaries. HOME
// alone does not isolate the macOS Keychain, and account caches eventually
// expire: discovery must keep finding only the test's sign-ins and CLIs.
func isolatedTests(m *testing.M) (int, error) {
	home, err := os.MkdirTemp("", "uni-switch-gateway-test-")
	if err != nil {
		return 1, err
	}
	defer os.RemoveAll(home)
	for name, value := range map[string]string{
		"HOME": home, "USERPROFILE": home,
		"XDG_CONFIG_HOME": filepath.Join(home, ".config"),
		"XDG_CACHE_HOME":  filepath.Join(home, ".cache"),
		"XDG_DATA_HOME":   filepath.Join(home, ".local", "share"),
		"APPDATA":         filepath.Join(home, "AppData", "Roaming"),
		"LOCALAPPDATA":    filepath.Join(home, "AppData", "Local"),
	} {
		if err := os.Setenv(name, value); err != nil {
			return 1, err
		}
	}
	// The package isolates itself rather than through testenv.Run, so it
	// says so as testenv does: what reads the marker (a CLI identity kept
	// on disk, provider.keepsIdentities; the machine's own folders,
	// proc.SystemDirs) then acts as in any isolated test binary. Without it
	// an ask still going when its test ended wrote cli-identity.json into
	// the next test's folder as that was removed (#1524, again after
	// 9c377231 dropped this package's own switch).
	if err := os.Setenv(testenv.Marker, home); err != nil {
		return 1, err
	}
	for _, name := range agentenv.Vars {
		if err := os.Unsetenv(name); err != nil {
			return 1, err
		}
	}
	// The machine's proxy variables too: net/http freezes them into its
	// first routed request, so a test that sets its own afterwards (search's
	// Auto, by ALL_PROXY alone) reads the machine's instead, and its
	// searcher goes out through a proxy that cannot reach the test's
	// servers.
	testenv.ClearProxy()
	// The fakes of ChatGPT's backend read turns plain; codex_zstd_test's
	// send them zstd, as the real one is sent.
	provider.CodexZstdForTests(false)
	// and speak HTTP: codex_socket_test's fakes speak the WebSocket
	codexSocketOff.Store(true)
	// Finding an inert CLI also prevents discovery from falling back to
	// one installed at an absolute system path. Tests can prepend their
	// own fakes, as fakeClaude does, and still use ordinary shell tools.
	// A test binary this one starts again (the cold-start redaction tests)
	// runs the stand-ins its parent made: on macOS a newly written program
	// waits for the system's first-run check, and under a full suite's load
	// a child's account discovery spent its whole 20s on new stand-ins.
	bin := os.Getenv(standInsEnv)
	if bin == "" {
		bin = filepath.Join(home, "bin")
		if err := os.Mkdir(bin, 0o755); err != nil {
			return 1, err
		}
		if err := testenv.StandIns(bin, []string{"security", "secret-tool", "claude", "codex", "cursor-agent", "devin", "grok", "kiro-cli"}); err != nil {
			return 1, err
		}
		defer testenv.RemovePrograms()
		if err := os.Setenv(standInsEnv, bin); err != nil {
			return 1, err
		}
	}
	if err := os.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH")); err != nil {
		return 1, err
	}
	// no route is written to disk behind a test's back; the history's own
	// tests call saveRoute themselves
	keepRoutes = false
	// no test asks a vendor: a provider with a real base URL and a made-up
	// key would, in the background (a plan key's windows, #1016), and a
	// plugin host's first start fetched models.dev
	testenv.Offline()
	// and no rest waits on a reading in the background: it would ask a
	// test's fake upstream after the test ended (#2036, CodeAlex52)
	awaitAllowance = func(context.Context, string, string) bool { return false }
	// no route writes a request body in place while it is held
	guardHeldBodies()
	return heldWrittenAfter(m.Run()), nil
}

// The package's tests run as an isolated test binary: nothing the marker
// turns off (a CLI identity kept on disk, #1524) runs behind a test's back.
func TestRunsAsAnIsolatedTestBinary(t *testing.T) {
	if os.Getenv(testenv.Marker) == "" {
		t.Fatalf("TestMain left %s unset", testenv.Marker)
	}
}

// rotation is what serveRotation registered: the routings plugins/rotation
// serves, which the gateway's tests route by as a user with it on does.
var rotation []func()

var chained func()

func serveMiddleware() { chained = func() {} }

// withoutMiddleware takes the JS middleware chain away for t, as
// plugins/middleware off does.
func withoutMiddleware(t *testing.T) {
	chained()
	t.Cleanup(func() { chained = func() {} })
}

func serveRotation() {
	for name, s := range routes.Builtin {
		rotation = append(rotation, routes.Register(name, s))
	}
}

// withoutRotation takes them away for t, as plugins/rotation off does.
func withoutRotation(t *testing.T) {
	for _, remove := range rotation {
		remove()
	}
	rotation = nil
	t.Cleanup(serveRotation)
}

// groupsServed is plugins/groups on, as for a user with a group of their
// own: the gateway's tests serve their groups.
var groupsServed func()

// searchOn is plugins/search on: the gateway's tests offer and answer
// uni-switch's web search, as a user with it on has.
var searchOn func()

// limitsOn is plugins/budget on: the gateway's tests hold gateway keys to
// their limits, as a user with it on has.
var limitsOn func()

// withoutLimits is plugins/budget off for t, with no refresh to turn it
// on: a key held to a limit is refused with nothing sent.
func withoutLimits(t *testing.T) {
	limitsOn()
	t.Cleanup(func() { limitsOn = gwbudget.Limit(budget.Limiter{}) })
}

// withoutSearch is plugins/search off for t, in a process serving the
// gateway (gwtool.Gate): uni-switch's web search is neither offered nor
// answered.
func withoutSearch(t *testing.T) {
	searchOn()
	stop := gwtool.Gate()
	t.Cleanup(func() {
		stop()
		searchOn = gwtool.Serve(gwtool.WebSearch)
	})
}

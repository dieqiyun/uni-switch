package usage

import (
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/testenv"
)

// TestMain gives the package a home of its own (testenv): a test that
// doesn't set one reads and writes there, never in the runner's agents or,
// on Windows, its APPDATA and LOCALAPPDATA. The package's ledger reads what
// sessions finds, which asks each agent's own variable before its folder in
// the home: one left in the shell would point a test at the runner's real
// agent (#522), so testenv clears those too. A test's answers are built
// again as soon as the history changes, unless it holds them (effortFresh).
func TestMain(m *testing.M) {
	effortFresh, effortBehind = 0, false
	testenv.Main(m)
}

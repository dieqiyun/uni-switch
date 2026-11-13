package sessions

import (
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/testenv"
)

// The tests run in a home of their own, never the user's real files, and
// a scan's buffer given back is filled with garbage, so a reader that kept
// a line past its scan fails here rather than for a user.
func TestMain(m *testing.M) {
	poisonReleased = true
	// a test's saves are written as they come; TestSaveWaitsOutTheGap
	// sets the gap
	saveGap = 0
	testenv.Main(m)
}

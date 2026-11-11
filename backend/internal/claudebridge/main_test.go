package claudebridge

import (
	"os"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/testenv"
)

// helperEnv makes the test binary the helper itself, as an agent starts
// it: a process of its own, reading the environment it was handed.
const helperEnv = "CLAUDEBRIDGE_TEST_HELPER"

// The tests run in a home of their own, never the user's real files.
func TestMain(m *testing.M) {
	if os.Getenv(helperEnv) != "" {
		if err := RunMCP(nil); err != nil {
			os.Stderr.WriteString(err.Error() + "\n")
			os.Exit(1)
		}
		os.Exit(0)
	}
	testenv.Main(m)
}

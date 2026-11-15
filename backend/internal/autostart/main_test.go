package autostart

import (
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/testenv"
)

// The tests run in a home of their own, never the user's real files, and
// never ask the system about the user's own login item.
func TestMain(m *testing.M) {
	keepToTheSandbox()
	testenv.Main(m)
}

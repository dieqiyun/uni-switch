package gui

import (
	"net"
	"os"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/fx"
	"github.com/dieqiyun/uni-switch/backend/internal/testenv"
)

// TestMain keeps the rate off the network: a state read with no rate cached
// asks for one behind it (#541), which would otherwise reach the real
// endpoint and land in whatever test runs next. And the tests run in a
// home of their own, never the user's real files.
func TestMain(m *testing.M) {
	// started by TestGatewayFixQuitsOnlyAuni-switch as an older uni-switch
	if addr := os.Getenv("UNI_SWITCH_TEST_FAKE_GATEWAY"); addr != "" {
		fakeOlderGateway(addr)
		return
	}
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err == nil {
		addr := l.Addr().String()
		l.Close() // refused at once from here on
		fx.SetRateURL("http://" + addr + "/")
	}
	os.Exit(testenv.Run(m))
}

package testenv

import (
	"os"
	"testing"
)

// This package's tests run without the machine's proxy variables too:
// Offline's guard sits below the proxy hop, so one standing on loopback
// carries a refused host for real, and TestOffline then answers
// models.dev through it.
func TestMain(m *testing.M) {
	ClearProxy()
	os.Exit(m.Run())
}

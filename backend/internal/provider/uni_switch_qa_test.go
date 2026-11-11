//go:build uniswitchqa

package provider

import (
	"context"
	"strings"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/proc"
)

func TestUniSwitchQAProcessIsolation(t *testing.T) {
	if !proc.Isolated() {
		t.Fatal("test must run in an isolated HOME")
	}
	ps, err := listProcesses(context.Background())
	if err != nil || len(ps) != 0 {
		t.Fatalf("host process enumeration was not blocked: %d, %v", len(ps), err)
	}
	if err := endCodexAppServer(0); err == nil || !strings.Contains(err.Error(), "QA process isolation") {
		t.Fatalf("host process restart was not blocked: %v", err)
	}
}

//go:build uniswitchqa

package provider

import (
	"context"
	"errors"

	"github.com/dieqiyun/uni-switch/backend/internal/proc"
)

// Only QA builds include this boundary. A temporary HOME alone does not
// isolate the Windows process table: an app-server has no readable HOME.
// Never enumerate or restart the host's Codex from an isolated UI run.
func init() {
	list, end := listProcesses, endCodexAppServer
	listProcesses = func(ctx context.Context) ([]proc.Process, error) {
		if proc.Isolated() {
			return nil, nil
		}
		return list(ctx)
	}
	endCodexAppServer = func(pid int) error {
		if proc.Isolated() {
			return errors.New("QA process isolation blocks app-server restart")
		}
		return end(pid)
	}
}

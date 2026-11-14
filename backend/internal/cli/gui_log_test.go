//go:build !nogui

package cli

import (
	"os"
	"strings"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/applog"
)

// The desktop app, its window and its panel each write their log to
// app.log before the app starts: opened from the Finder, the Dock or the
// Start menu it has no terminal, and a user asked for its log had none to
// send (rayem1w on X, 0.1.2330).
func TestTheAppWritesItsLog(t *testing.T) {
	was, wasPanel := openApp, openPanel
	t.Cleanup(func() { openApp, openPanel = was, wasPanel; applog.Stop() })
	started := 0
	openApp = func(bool, string) error { started++; return nil }
	openPanel = func() error { started++; return nil }

	for name, open := range map[string]func() error{
		"app":    func() error { return runGUI(false, "") },
		"window": func() error { return runWindow("usage") },
		"panel":  runPanel,
	} {
		applog.Stop()
		os.Remove(applog.Path())
		if err := open(); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		b, err := os.ReadFile(applog.Path())
		if err != nil {
			t.Fatalf("%s wrote no log: %v", name, err)
		}
		if !strings.Contains(string(b), "started (") {
			t.Errorf("%s's log says nothing of its start:\n%s", name, b)
		}
		if applog.Current() != applog.Path() {
			t.Errorf("%s: the log it writes is %q, want %q", name, applog.Current(), applog.Path())
		}
	}
	if started != 3 {
		t.Errorf("the app was started %d times, want 3", started)
	}
}

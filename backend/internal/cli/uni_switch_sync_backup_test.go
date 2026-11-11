package cli

import (
	"strings"
	"testing"
)

func TestUniSwitchSyncBackupCommandsRemoved(t *testing.T) {
	helpHome(t)
	for _, name := range []string{"backup", "restore", "webdav", "dav", "s3", "github"} {
		if text := helpFor([]string{name, "--help"}); strings.Contains(text, "uni-switch "+name+" ") {
			t.Errorf("help still advertises removed command %q", name)
		}
	}
	for _, line := range strings.Split(usage, "\n") {
		for _, name := range []string{"backup", "restore", "webdav", "s3", "github"} {
			if strings.HasPrefix(strings.TrimSpace(line), "uni-switch "+name+" ") {
				t.Errorf("usage still offers %s", name)
			}
		}
	}
}

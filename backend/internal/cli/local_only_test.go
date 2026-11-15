package cli

import "testing"

func TestLocalOnlyCommandsRejectSharing(t *testing.T) {
	for _, args := range [][]string{{"install", "--lan"}, {"web", "on", "--lan"}, {"install", "--addr", "0.0.0.0:3430"}, {"web", "on", "--addr", "192.168.1.2:3430"}} {
		if _, _, err := parseDaemon(args); err == nil {
			t.Errorf("daemon accepted network sharing: %v", args)
		}
	}
	for _, args := range [][]string{{"--lan"}, {"--addr", "0.0.0.0:3430"}, {"--addr", "192.168.1.2:3430"}} {
		if err := webCmd(args); err == nil {
			t.Errorf("web accepted network sharing: %v", args)
		}
	}
	if err := serveCmd([]string{"--web=0.0.0.0:3430"}); err == nil {
		t.Fatal("serve accepted a network page")
	}
	if _, err := webOptions(daemonWeb{}, "0.0.0.0:3430", "fixture-key", ""); err == nil {
		t.Fatal("service reused an old network address")
	}
}

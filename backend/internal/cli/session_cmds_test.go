package cli

import (
	"os"
	"testing"
)

// What an agent starts for each session or message (the image MCP, Claude
// Code's MCP helper, the status line) never runs the start-up moves, which
// list every provider and account and so start the plugin host and ask
// claude, cursor-agent and devin who is signed in (#3318).
func TestSessionCommandsSkipStartUp(t *testing.T) {
	ran := 0
	was := startUp
	startUp = func() { ran++ }
	t.Cleanup(func() { startUp = was })
	t.Setenv("UNI_SWITCH_ADDR", "127.0.0.1:1")
	t.Setenv("UNI_SWITCH_MCP_CALLBACK", "")
	null, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatal(err)
	}
	defer null.Close()
	stdin := os.Stdin
	os.Stdin = null
	t.Cleanup(func() { os.Stdin = stdin })
	for _, args := range [][]string{
		{"mcp", "image"},
		{"mcp", "nope"},
		{"claude-mcp-helper"},
		{"statusline"},
	} {
		_ = run(args)
		if ran != 0 {
			t.Fatalf("%v ran the start-up moves", args)
		}
	}
	// and a command that reads the files still has them brought up first
	_ = run([]string{"version"})
	if ran != 1 {
		t.Fatalf("version: start-up ran %d times, want 1", ran)
	}
}

package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDisconnectPreviewKeepsDryRunForFileAgents(t *testing.T) {
	home, _ := codexHome(t, "", "model = \"gpt-5.4\"\n")
	a := codex(home)
	if err := a.Apply("model", "fake/m1"); err != nil {
		t.Fatal(err)
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	tmp, err := filepath.EvalSymlinks(os.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("TMPDIR", tmp)
	changes, err := DisconnectPreview(a, exe)
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, change := range changes {
		for _, line := range change.Lines {
			if strings.Contains(line.Text, "gpt-5.4") || strings.Contains(line.Was, "gpt-5.4") {
				found = true
			}
		}
	}
	if !found {
		t.Fatalf("Codex preview lost the restored model: %+v", changes)
	}
}

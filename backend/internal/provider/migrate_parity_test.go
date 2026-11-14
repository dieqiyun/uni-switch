package provider

import (
	"os"
	"path/filepath"

	"testing"
	"time"
)

// Devin's own account moves with the plan the Devin CLI says, as the
// built-in showed it, though its saved row has none.
func TestDevinOwnMovesWithPlan(t *testing.T) {
	home := claudeHome(t)
	data := filepath.Join(home, "data")
	t.Setenv("XDG_DATA_HOME", data)
	t.Setenv("APPDATA", data) // where Windows' Devin CLI keeps it
	os.MkdirAll(filepath.Join(data, "devin"), 0o700)
	if err := os.WriteFile(filepath.Join(data, "devin", "credentials.toml"), devinCredentials("cog_own_key_1234", "", "", ""), 0o600); err != nil {
		t.Fatal(err)
	}
	devinStatus.Lock()
	devinStatus.user, devinStatus.plan, devinStatus.ok, devinStatus.at = "dev@example.com", "Devin Pro", true, time.Now()
	devinStatus.Unlock()
	t.Cleanup(devinStatus.forget)
	own := movingOf(t, "devin")["dev@example.com"]
	md, _ := own.Auth["metadata"].(map[string]any)
	if !own.Own || own.Plan != "Devin Pro" || md["plan"] != "Devin Pro" {
		t.Fatalf("Devin's own: %+v", own)
	}
}

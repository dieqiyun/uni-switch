package settings

import (
	"bytes"
	"os"
	"testing"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/filememo"
)

// #1665: a catalog sync holds what was read for its round. A change to the
// settings made while it holds them is made from settings.json as it is on
// the disk: what the user or another uni-switch saved inside the held second
// stays.
func TestHeldSettingsAreNotWhatAWriteIsMadeFrom(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	if err := Save(Settings{Theme: "light"}); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-time.Hour)
	if err := os.Chtimes(Path(), old, old); err != nil {
		t.Fatal(err)
	}
	release := filememo.Hold()
	defer release()
	if Load().Theme != "light" {
		t.Fatal("theme not saved")
	}
	// another uni-switch's save, past the time a stamp can't tell apart
	b, err := os.ReadFile(Path())
	if err != nil || !bytes.Contains(b, []byte(`"light"`)) {
		t.Fatalf("%v: %s", err, b)
	}
	if err := os.WriteFile(Path(), bytes.Replace(b, []byte(`"light"`), []byte(`"dark"`), 1), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(Path(), old.Add(time.Minute), old.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if Load().Theme != "light" {
		t.Fatal("not held: the test no longer holds what it is about")
	}
	if err := SetGatewayConversations(true); err != nil {
		t.Fatal(err)
	}
	if got := Load(); got.Theme != "dark" || !got.GatewayConversations {
		t.Fatalf("theme %q, recording %v: the other save was written over", got.Theme, got.GatewayConversations)
	}
}

package settings

import (
	"testing"
)

// AccountDiscoveryOff parses settings.json once for each version of the
// file: it is asked for each agent and each plugin every time the
// providers are built, and parsed the whole file each time (#2502). A
// change saved is seen on the next ask.
func TestAccountDiscoveryOffParsedOnce(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("UNI_SWITCH_NO_ACCOUNTS", "")
	s := Load()
	s.NoAccountDiscovery = true
	if err := Save(s); err != nil {
		t.Fatal(err)
	}
	if !AccountDiscoveryOff() {
		t.Fatal("saved off, read on")
	}
	n := Parses()
	for range 100 {
		AccountDiscoveryOff()
	}
	if got := Parses() - n; got != 0 {
		t.Fatalf("an unchanged settings.json was parsed %d more times", got)
	}
	s = Load()
	s.NoAccountDiscovery = false
	if err := Save(s); err != nil {
		t.Fatal(err)
	}
	if AccountDiscoveryOff() {
		t.Fatal("saved on, still read off")
	}
}

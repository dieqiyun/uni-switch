package provider

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/settings"
)

// The editor's looks at what the user set on a provider's models (names,
// efforts, compact and reply limits) and the gateway's saved address read
// the settings every caller shares, not a copy of their own: with a few
// thousand models named, a copy each was most of what they allocated. What
// they return is the caller's to change, and doesn't reach the settings.
func TestModelPrefsReadTheSharedSettings(t *testing.T) {
	isolate(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
	s := settings.Load()
	s.ModelNames, s.ModelEfforts = map[string]string{}, map[string][]string{}
	s.ModelCompacts, s.ModelOutputs = map[string]int{}, map[string]int{}
	for i := range 3000 {
		k := fmt.Sprintf("other/model-%d", i)
		s.ModelNames[k], s.ModelEfforts[k] = fmt.Sprintf("Model %d", i), []string{"low", "high"}
		s.ModelCompacts[k], s.ModelOutputs[k] = 1000+i, 2000+i
	}
	s.ModelNames["p/m"], s.ModelEfforts["p/m"] = "Mine", []string{"low", "high"}
	s.ModelCompacts["p/m"], s.ModelOutputs["p/m"] = 50000, 8000
	if err := settings.Save(s); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-time.Hour)
	if err := os.Chtimes(settings.Path(), old, old); err != nil {
		t.Fatal(err)
	}
	p := Provider{ID: "p"}
	if got := p.ModelNames()["m"]; got != "Mine" {
		t.Fatalf("ModelNames: %q", got)
	}
	if got := CompactsOf("p")["m"]; got != 50000 {
		t.Fatalf("CompactsOf: %d", got)
	}
	if got := OutputsOf("p")["m"]; got != 8000 {
		t.Fatalf("OutputsOf: %d", got)
	}
	es := p.ModelEfforts()["m"]
	es[0] = "changed"
	if again := p.ModelEfforts()["m"]; len(again) != 2 || again[0] != "low" {
		t.Fatalf("a caller's change reached the settings: %v", again)
	}

	copied := testing.AllocsPerRun(5, func() { settings.Load() })
	for name, look := range map[string]func(){
		"ModelNames":   func() { p.ModelNames() },
		"ModelEfforts": func() { p.ModelEfforts() },
		"CompactsOf":   func() { CompactsOf("p") },
		"OutputsOf":    func() { OutputsOf("p") },
		"SavedAddr":    func() { settings.SavedAddr() },
	} {
		if n := testing.AllocsPerRun(10, look); n > copied/10 {
			t.Errorf("%s: %.0f allocations, against %.0f for a copy of the settings", name, n, copied)
		}
	}
}

package agent

import (
	"bytes"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// #5222: DeepSeek Harness Desktop with four profiles (desktop, web,
// headless, open-design). A model picked in dsh is saved by dsh into the
// profile in use alone (ConfigEditor.edit), so the desktop profile started
// on hy4free while web, headless and open-design still started on
// deepseek-v4.1-flash. uni-switch read web's start alone: the Agents page showed
// a model the desktop app's sessions don't start on, and its log said
// "wrote dsh's route again" each time the round moved uni-switch's web search
// entry after dsh's save, which read as uni-switch writing over the pick.
//
// The desktop profile's bytes here are what dsh 0.2.1-rc.2's own save
// (dsh-agent-default-model saveSelection through the yaml 2.9.1 library)
// wrote over uni-switch's entry in a sandbox: the value unquoted, the effort
// added, uni-switch's comment kept.
func TestDshShowsTheStartOfTheProfileInUse(t *testing.T) {
	home, dir, web := dshRouteHome(t)
	dshKeyFixture(dir)
	apps := t.TempDir()
	was := appFolders
	appFolders = func() []string { return []string{apps} }
	t.Cleanup(func() { appFolders = was })
	files := map[string]string{"web": web}
	for _, p := range []string{"desktop", "headless", "open-design"} {
		f := filepath.Join(dir, "profiles", p, "cordis.patch.yml")
		if err := os.MkdirAll(filepath.Dir(f), 0o755); err != nil {
			t.Fatal(err)
		}
		files[p] = f
	}
	for _, f := range files {
		if err := os.WriteFile(f, []byte(dshProfileTemplate), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	a := dsh(home)
	if err := a.Field("model").Set("uni-switch/deepseek/pro"); err != nil {
		t.Fatal(err)
	}
	dshWiredOnce()

	read := func(p string) string { b, _ := os.ReadFile(files[p]); return string(b) }
	ours := "- id: agent-default-model # uni-switch\n  config:\n    provider: uni-switch\n    model: \"deepseek/pro\"\n"
	saved := "- id: agent-default-model # uni-switch\n  config:\n    provider: uni-switch\n    model: deepseek/flash\n    reasoningEffort: high\n"
	desk := read("desktop")
	if !strings.Contains(desk, ours) {
		t.Fatalf("uni-switch's start isn't as this test expects:\n%s", desk)
	}
	if err := os.WriteFile(files["desktop"], []byte(strings.Replace(desk, ours, saved, 1)), 0o600); err != nil {
		t.Fatal(err)
	}

	// no desktop app: the web profile is the one shown, as before
	if got := dsh(home).Field("model").Get(); got != "uni-switch/deepseek/pro" {
		t.Fatalf("without the desktop app the model shown = %q, want web's uni-switch/deepseek/pro", got)
	}

	installApp(t, apps, "DeepSeek Harness.app", dshAppPlist)
	a = dsh(home)
	if got := a.Field("model").Get(); got != "uni-switch/deepseek/flash" {
		t.Fatalf("model shown = %q, want the desktop profile's uni-switch/deepseek/flash, the one its sessions start on", got)
	}
	if got := a.Field("effort").Get(); got != "high" {
		t.Fatalf("effort shown = %q, want the desktop profile's high", got)
	}

	// the rounds leave dsh's pick where dsh saved it and copy it nowhere:
	// each profile's start is the user's own
	var buf bytes.Buffer
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })
	for range 3 {
		var r dshRounds
		if trouble := r.once(); trouble != "" {
			t.Fatalf("round: %s", trouble)
		}
	}
	log.SetOutput(os.Stderr)
	if desk := read("desktop"); !strings.Contains(desk, saved) {
		t.Fatalf("the rounds wrote over dsh's own pick:\n%s", desk)
	}
	for _, p := range []string{"web", "headless", "open-design"} {
		if !strings.Contains(read(p), ours) {
			t.Fatalf("the %s profile's start moved:\n%s", p, read(p))
		}
	}
	// the one write: uni-switch's web search entry follows the desktop's start
	if !strings.Contains(read("desktop"), "    model: \"deepseek/flash\"\n") {
		t.Fatalf("uni-switch's web search entry didn't follow the start:\n%s", read("desktop"))
	}
	said := buf.String()
	if strings.Count(said, "dsh's web search in "+files["desktop"]+" follows the model it starts on now") != 1 {
		t.Fatalf("the round's log doesn't say its one write was the search entry:\n%s", said)
	}
	if strings.Contains(said, "wrote dsh's route again") {
		t.Fatalf("the round says it wrote dsh's route again, with the route as it was:\n%s", said)
	}
}

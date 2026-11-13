package appdir

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// fakeRegistry stands in for Windows' registry: reg is what a process
// started now would get. followStart is taken again over it, as uni-switch's
// start would, and both are put back after the test.
func fakeRegistry(t *testing.T, reg map[string]string) {
	t.Helper()
	was, wasFor := regEnv, followFor
	regEnv = func(name string) (string, bool) { v, ok := reg[name]; return v, ok }
	followFor = 0
	t.Cleanup(func() { regEnv, followFor = was, wasFor; followStart() })
}

// unset clears name for the test, put back after it.
func unset(t *testing.T, name string) {
	t.Helper()
	t.Setenv(name, "")
	os.Unsetenv(name)
}

// bm_rabbit on Discord: Pi's AgentDir moved, PI_CODING_AGENT_DIR changed
// in the user's variables while uni-switch ran, and uni-switch wrote Pi's files
// into the old folder again. A variable that came from the registry reads
// as the registry has it now, and the programs uni-switch starts get it too.
func TestAgentVarFollowsTheRegistry(t *testing.T) {
	const v = "PI_CODING_AGENT_DIR"
	old, moved := filepath.Join(t.TempDir(), "old"), filepath.Join(t.TempDir(), "new")
	reg := map[string]string{v: old}
	fakeRegistry(t, reg)
	t.Setenv(v, old) // as Windows gave it to uni-switch at its start
	followStart()
	if got := Getenv(v); got != old {
		t.Fatalf("at start %s = %q, want %q", v, got, old)
	}
	reg[v] = moved // the user changes it in System Properties
	if got := Getenv(v); got != moved {
		t.Fatalf("after the user moved it, %s = %q, want %q (the registry's now, not uni-switch's start's)", v, got, moved)
	}
	if got := os.Getenv(v); got != moved {
		t.Errorf("uni-switch's environment, which the programs it starts get, has %s = %q, want %q", v, got, moved)
	}
	delete(reg, v) // and then deletes it
	if got, ok := LookupEnv(v); ok {
		t.Errorf("after the user deleted it, %s = %q, want it unset", v, got)
	}
	if _, ok := os.LookupEnv(v); ok {
		t.Errorf("uni-switch's environment still has %s after the user deleted it", v)
	}
}

// One set while none was in the registry at start is followed too: a
// CODEX_HOME the user adds while uni-switch runs.
func TestAgentVarAddedLaterFollows(t *testing.T) {
	const v = "CODEX_HOME"
	reg := map[string]string{}
	fakeRegistry(t, reg)
	unset(t, v)
	followStart()
	d := t.TempDir()
	reg[v] = d
	if got := Getenv(v); got != d {
		t.Errorf("%s added to the user's variables reads %q, want %q", v, got, d)
	}
}

// A value the launcher set, unlike the registry's, and one set inside
// uni-switch since it started, stand as they are.
func TestAgentVarSetByHandStands(t *testing.T) {
	const v, w = "CLAUDE_CONFIG_DIR", "CODEX_HOME"
	mine, reg1, reg2 := t.TempDir(), t.TempDir(), t.TempDir()
	reg := map[string]string{v: reg1, w: reg1}
	fakeRegistry(t, reg)
	t.Setenv(v, mine) // `$env:CLAUDE_CONFIG_DIR=…; uni-switch`
	t.Setenv(w, reg1)
	followStart()
	reg[v], reg[w] = reg2, reg2
	if got := Getenv(v); got != mine {
		t.Errorf("the launcher's %s reads %q, want %q", v, got, mine)
	}
	os.Setenv(w, mine) // inside uni-switch, a test's sandbox
	if got := Getenv(w); got != mine {
		t.Errorf("%s set inside uni-switch reads %q, want %q", w, got, mine)
	}
}

// What isn't an agent's variable is uni-switch's environment as it is.
func TestOtherVarsDontFollow(t *testing.T) {
	reg := map[string]string{"UNI_SWITCH_FOLLOW_TEST": "reg"}
	fakeRegistry(t, reg)
	unset(t, "UNI_SWITCH_FOLLOW_TEST")
	followStart()
	if _, ok := LookupEnv("UNI_SWITCH_FOLLOW_TEST"); ok {
		t.Error("a variable no agent reads followed the registry")
	}
}

// Read again after followFor, not on every call.
func TestRegistryReadAtMostEachFollowFor(t *testing.T) {
	const v = "PI_CODING_AGENT_DIR"
	reads := 0
	was, wasFor := regEnv, followFor
	regEnv = func(name string) (string, bool) {
		if name == v {
			reads++
		}
		return "", false
	}
	followFor = time.Hour
	t.Cleanup(func() { regEnv, followFor = was, wasFor; followStart() })
	unset(t, v)
	followStart()
	start := reads
	for range 100 {
		Getenv(v)
	}
	if reads != start {
		t.Errorf("100 reads within followFor read the registry %d times, want 0", reads-start)
	}
}

func TestGoneNamedFolderIsNotMade(t *testing.T) {
	root := t.TempDir()
	gone := filepath.Join(root, "piData", ".pi", "agent")
	t.Setenv("PI_CODING_AGENT_DIR", gone)
	var g *GoneError
	for _, d := range []string{gone, filepath.Join(gone, "sessions")} {
		if err := Gone(d); !errors.As(err, &g) || g.Var != "PI_CODING_AGENT_DIR" || g.Dir != gone {
			t.Errorf("Gone(%q) = %v, want a GoneError naming PI_CODING_AGENT_DIR and %s", d, err, gone)
		}
	}
	// the folder there: what is under it is made as before
	if err := os.MkdirAll(gone, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := Gone(filepath.Join(gone, "sessions")); err != nil {
		t.Errorf("under a folder that exists: %v", err)
	}
	// the named folder alone missing, its parent there: it is made
	t.Setenv("PI_CODING_AGENT_DIR", filepath.Join(root, "fresh"))
	if err := Gone(filepath.Join(root, "fresh", "sessions")); err != nil {
		t.Errorf("a named folder whose parent exists: %v", err)
	}
	// a folder no variable names, the default one, is made
	t.Setenv("PI_CODING_AGENT_DIR", "")
	if err := Gone(filepath.Join(root, "home", ".pi", "agent")); err != nil {
		t.Errorf("a default folder: %v", err)
	}
	// a name that only starts the same is another folder
	t.Setenv("CODEX_HOME", filepath.Join(root, "codex"))
	if err := Gone(filepath.Join(root, "codex-other")); err != nil {
		t.Errorf("a sibling whose name starts the same: %v", err)
	}
}

package profile

import (
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/agent"
	"github.com/dieqiyun/uni-switch/backend/internal/library"
	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// inUse is which of the saved profiles InUse names, read as the GUI's state
// reads them: every detected agent's values once, passed in.
func inUse(t *testing.T) []string {
	t.Helper()
	detected := agent.Detected()
	now := map[string]map[string]string{}
	for _, a := range detected {
		now[a.ID] = a.Values()
	}
	ps := must[map[string]Profile](t)(Load())
	var out []string
	for _, n := range Names(ps) {
		if InUse(ps[n], detected, now) {
			out = append(out, n)
		}
		// read here, the same answer (the TUI and the CLI pass none)
		if InUse(ps[n], detected, now) != InUse(ps[n], detected, nil) {
			t.Errorf("%s: in use with the values passed differs from read", n)
		}
	}
	return out
}

func setModel(t *testing.T, id, model string) {
	t.Helper()
	for _, a := range agent.Detected() {
		if a.ID == id {
			if err := a.Apply("model", model); err != nil {
				t.Fatal(err)
			}
			return
		}
	}
	t.Fatalf("no %s", id)
}

func same(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// Which profile is in use (superhtoooth on X: nothing in uni-switch said). A
// profile is in use while applying it would change nothing: the agents'
// fields, their model lists and the library's setup are as it has them.
// Saved, it is in use at once; another applied, that one is; a setting
// changed by hand since, neither is, until one is applied again.
func TestProfileInUse(t *testing.T) {
	modelsHome(t)
	if got := inUse(t); len(got) != 0 {
		t.Fatalf("no profiles yet, in use %v", got)
	}

	// 工作 saved under the name typed, Codex on m1: in use at once
	setModel(t, "codex", "relay/m1")
	if err := Save("工作", must[Profile](t)(Snapshot())); err != nil {
		t.Fatal(err)
	}
	if got := inUse(t); !same(got, []string{"工作"}) {
		t.Fatalf("saved 工作: in use %v", got)
	}

	// 游戏, Codex on m2: it is the one in use, 工作 no longer
	setModel(t, "codex", "relay/m2")
	if got := inUse(t); len(got) != 0 {
		t.Fatalf("model changed by hand: in use %v", got)
	}
	if err := Save("游戏", must[Profile](t)(Snapshot())); err != nil {
		t.Fatal(err)
	}
	if got := inUse(t); !same(got, []string{"游戏"}) {
		t.Fatalf("saved 游戏: in use %v", got)
	}

	// 工作 applied
	ps := must[map[string]Profile](t)(Load())
	must[Applied](t)(Apply(ps["工作"]))
	if got := inUse(t); !same(got, []string{"工作"}) {
		t.Fatalf("applied 工作: in use %v", got)
	}

	// a model taken out of Codex's list by hand: the list isn't 工作's
	if err := provider.SetHiddenModels("codex", []string{"relay/m3"}); err != nil {
		t.Fatal(err)
	}
	if got := inUse(t); len(got) != 0 {
		t.Fatalf("model list changed: in use %v", got)
	}
	must[Applied](t)(Apply(ps["工作"]))
	if got := inUse(t); !same(got, []string{"工作"}) {
		t.Fatalf("applied 工作 again: in use %v", got)
	}

	// the library's setup is part of it: instructions changed since, 带库
	// isn't in use; applied, it is
	shared := "Use tabs."
	must[*library.Result](t)(library.SaveInstructions(library.InstructionsChange{Shared: &shared, Agents: []string{"codex"}}))
	must[*library.Result](t)(library.SaveServer("", library.Server{Name: "fs", Transport: "stdio", Command: "npx", Agents: []string{"codex"}}))
	if err := Save("带库", must[Profile](t)(Snapshot())); err != nil {
		t.Fatal(err)
	}
	if got := inUse(t); !same(got, []string{"带库"}) {
		// 工作 has no library setup, so it is in use with any library
		if !same(got, []string{"工作", "带库"}) {
			t.Fatalf("saved 带库: in use %v", got)
		}
	}
	other := "Use spaces."
	must[*library.Result](t)(library.SaveInstructions(library.InstructionsChange{Shared: &other, Agents: []string{"codex"}}))
	for _, n := range inUse(t) {
		if n == "带库" {
			t.Fatal("instructions changed: 带库 still in use")
		}
	}
	must[*library.Result](t)(library.ServerAgents("fs", nil))
	must[Applied](t)(Apply(must[map[string]Profile](t)(Load())["带库"]))
	found := false
	for _, n := range inUse(t) {
		found = found || n == "带库"
	}
	if !found {
		t.Fatalf("applied 带库: in use %v", inUse(t))
	}

	// one synced from a computer whose agents aren't here does nothing
	// applied, so it isn't the setup in use
	if InUse(Profile{Fields: map[string]string{"opencode.model": "relay/m1"}}, agent.Detected(), nil) {
		t.Error("a profile of agents not here is in use")
	}
}

package gui

import (
	"testing"
)

// Each poll of the Agents and Providers pages marshaled the state it keeps
// only to find it the same as kept: 38 MB of what twenty polls allocated.
// The same state is now compared by its hash, and marshaled only when it
// changed, by this run or against the file the run before kept.
func TestKeptStateMarshaledOnlyWhenChanged(t *testing.T) {
	sandboxHome(t)
	forgetLastState()
	forgetLastProviders()
	t.Cleanup(forgetLastState)
	t.Cleanup(forgetLastProviders)
	s := stateJSON{Agents: []agentJSON{{ID: "codex", Name: "Codex"}}}
	ps := providersJSON{Providers: []providerJSON{{ID: "relay", Name: "Relay"}}}
	keepState(s)
	keepProviders(ps)
	before := keptMarshaled.Load()
	for range 20 {
		keepState(s)
		keepProviders(ps)
	}
	if n := keptMarshaled.Load() - before; n != 0 {
		t.Fatalf("20 polls of the same state and list marshaled them %d times, want 0", n)
	}
	// a new run reads the file the last one kept, and finds it the same
	forgetLastState()
	forgetLastProviders()
	keepState(s)
	keepProviders(ps)
	if n := keptMarshaled.Load() - before; n != 0 {
		t.Fatalf("the state as the last run kept it was marshaled %d times, want 0", n)
	}
	s.Agents = append(s.Agents, agentJSON{ID: "claude", Name: "Claude Code"})
	keepState(s)
	if n := keptMarshaled.Load() - before; n != 1 {
		t.Fatalf("a state changed was marshaled %d times, want 1", n)
	}
	got, ok := lastKnown()
	if !ok || len(got.Agents) != 2 || got.Agents[1].ID != "claude" {
		t.Fatalf("the changed state isn't the one kept: %+v", got.Agents)
	}
	forgetLastState()
	if got, ok := lastKnown(); !ok || len(got.Agents) != 2 {
		t.Fatalf("the changed state isn't in the file: %+v", got.Agents)
	}
}

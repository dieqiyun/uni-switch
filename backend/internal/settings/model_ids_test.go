package settings

import (
	"testing"
)

// Wires is ModelWires with each model the user lists by an id of theirs
// sent as the provider's own: a wire the user gave that id is theirs, a
// provider's "*" wire wraps the own id, and the maps are read again when
// they change in place.
func TestWiresTakeModelIDs(t *testing.T) {
	s := Settings{
		ModelWires: map[string]string{"r/x": "vendor-x", "p/*": "acct/*"},
		ModelIDs:   map[string]string{"r/flash": "flash-1017", "r/x": "x-own", "p/grok": "grok@500k", "r/blank": " "},
	}
	w := s.Wires()
	for k, want := range map[string]string{"r/flash": "flash-1017", "r/x": "vendor-x", "p/grok": "acct/grok@500k", "p/*": "acct/*"} {
		if w[k] != want {
			t.Errorf("Wires()[%s] = %q, want %q", k, w[k], want)
		}
	}
	if _, ok := w["r/blank"]; ok {
		t.Error("a blank own id is a wire")
	}
	s.ModelIDs["r/flash"] = "flash-1018"
	if got := s.Wires()["r/flash"]; got != "flash-1018" {
		t.Errorf("after a change in place: %q", got)
	}
	if got := (Settings{ModelWires: map[string]string{"a/b": "c"}}).Wires(); len(got) != 1 || got["a/b"] != "c" {
		t.Errorf("no ModelIDs: %v", got)
	}
}

// MoveModel moves every per-model setting but ModelIDs to the new key,
// and keeps one the user already has there.
func TestMoveModelKeepsTheUsersOwn(t *testing.T) {
	s := Settings{
		ModelNames:   map[string]string{"r/old": "Old", "r/new": "Mine"},
		ModelOutputs: map[string]int{"r/old": 8000},
		ModelIDs:     map[string]string{"r/old": "x"},
	}
	if !s.MoveModel("r/old", "r/new") {
		t.Fatal("nothing moved")
	}
	if s.ModelNames["r/new"] != "Mine" || s.ModelNames["r/old"] != "Old" {
		t.Errorf("names %v", s.ModelNames)
	}
	if s.ModelOutputs["r/new"] != 8000 || len(s.ModelOutputs) != 1 {
		t.Errorf("outputs %v", s.ModelOutputs)
	}
	if s.ModelIDs["r/old"] != "x" {
		t.Errorf("ids %v", s.ModelIDs)
	}
}

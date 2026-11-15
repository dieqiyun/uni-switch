package agent

import "testing"

// Drift asked a field's options up to three times, once a field to find
// the one on uni-switch and twice to compare it with uni-switch's record, and
// Claude Code's were made anew each time (35 MB of forty polls with real
// accounts). They are made once a look now.
func TestDriftMakesAFieldsOptionsOnce(t *testing.T) {
	v, made := "", 0
	a := &Agent{ID: "drift-options", Name: "X", Fields: []Field{{Key: "model", Label: "Model",
		Get: func() string { return v },
		Set: func(s string) error { v = s; return nil },
		Options: func(map[string]string) []Option {
			made++
			return []Option{
				{Value: "nowhere-relay/m1", Ref: "nowhere-relay/m1"},
				{Value: "nowhere-relay/m2", Ref: "nowhere-relay/m2"},
			}
		},
	}}}
	if err := a.Apply("model", "nowhere-relay/m1"); err != nil {
		t.Fatal(err)
	}
	// another of uni-switch's models, picked in the agent: not drift, and every
	// comparison read the options
	v = "nowhere-relay/m2"
	made = 0
	if d := a.Drift(); d != nil {
		t.Fatalf("one uni-switch model for another drifted: %+v", d)
	}
	if made != 1 {
		t.Fatalf("one Drift made the field's options %d times, want once", made)
	}
}

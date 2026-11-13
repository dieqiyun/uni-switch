package agent

import (
	"os"
	"reflect"
	"testing"
)

// A look at the stash's values reads and parses stash.json once, not once
// for each look, and still sees each change: uni-switch's own, and one another
// process wrote.
func TestStashLooksShareOneRead(t *testing.T) {
	stashHome(t)
	stash(map[string]string{"a.was": "1"})
	first := reflect.ValueOf(stashShared()).UnsafePointer()
	for range 20 {
		if reflect.ValueOf(stashShared()).UnsafePointer() != first {
			t.Fatal("a look at the stash read and parsed it again")
		}
	}
	own := stashLoad()
	own["a.was"] = "mine"
	if got := stashShared()["a.was"]; got != "1" {
		t.Fatalf("a change to a caller's own stash reached the shared one: %q", got)
	}
	stash(map[string]string{"b.was": "2"})
	if got := stashShared()["b.was"]; got != "2" {
		t.Fatalf("uni-switch's own change not seen: %q", got)
	}
	if err := os.WriteFile(stashPath(), []byte(`{"c.was": "theirs, longer"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if s := stashShared(); s["c.was"] != "theirs, longer" || s["a.was"] != "" {
		t.Fatalf("another process's write not seen: %v", s)
	}
}

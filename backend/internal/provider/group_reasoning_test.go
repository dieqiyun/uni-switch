package provider

import (
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/catalog"
)

// levels are the levels of the member that thinks in these tests.
var levels = []string{"low", "high", "max"}

// thinksMember is a member that thinks, with levels.
func thinksMember() Entry { return thinks("a", levels) }

// thinks is a model that does: its own levels as given. Same model id as
// quietMember's, so the two are members of one group.
func thinks(p string, efforts []string) Entry {
	return Entry{ID: p + "/m", Model: "m", Name: "m", Provider: Provider{ID: p}, Efforts: efforts, Reasoning: true}
}

// quietMember is a member that doesn't think, and has no levels of its own:
// the shape a plugin's model had before its plugin listed capabilities. Its
// provider's account gives a model list (so its levels are known to be none,
// not unknown: the gateway asks it the effort as it is, no more than high).
func quietMember() Entry {
	e := quiet("q")
	e.Provider.Account = &Account{Agent: "plugin", models: func() []catalog.Model { return nil }}
	return e
}

// quiet is a model that doesn't think: its provider gives no levels for it,
// and nothing says it reasons.
func quiet(p string) Entry {
	return Entry{ID: p + "/m", Model: "m", Name: "m", Provider: Provider{ID: p}}
}

// groupReasoningHome is a machine of its own with a provider whose model
// has levels the user set.
func groupReasoningHome(t *testing.T) {
	t.Helper()
	effortHome(t)
	if err := SetModelEfforts("a/m", levels); err != nil {
		t.Fatal(err)
	}
}

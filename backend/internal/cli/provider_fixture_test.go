package cli

import (
	"encoding/json"
	"os"

	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// groupsHome is a machine of its own with two providers: a serves m and
// only-a, b serves vendor/m (so uni-switch finds group/auto-m).
func groupsHome(t *testing.T) {
	t.Helper()
	t.Setenv("HOME", t.TempDir()) // no agent signed in
	t.Setenv("USERPROFILE", os.Getenv("HOME"))
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	t.Setenv("DSH_HOME", "") // a dsh profile is found through it, not through HOME
	for _, p := range []provider.Provider{
		{ID: "a", Name: "A", Key: "ka", Models: []string{"m", "only-a", "claude-opus-5-5"}, Chat: "http://127.0.0.1:1/v1"},
		{ID: "b", Name: "B", Key: "kb", Models: []string{"vendor/m", "gpt-5.5"}, Chat: "http://127.0.0.1:1/v1"},
	} {
		if err := provider.Save(p); err != nil {
			t.Fatal(err)
		}
	}
}

// sameGroup compares two values of a field, one of them read back from the
// agent's own settings where the [1m] mark rides on the group's id: the
// marked one is the group uni-switch set, not another group. (internal/agent's
// own, tested there.)
func storedGroups(t *testing.T) []map[string]any {
	t.Helper()
	b, err := os.ReadFile(provider.Path())
	if err != nil {
		t.Fatal(err)
	}
	var f struct {
		Groups []map[string]any `json:"groups"`
	}
	if err := json.Unmarshal(b, &f); err != nil {
		t.Fatal(err)
	}
	return f.Groups
}

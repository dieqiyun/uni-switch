package access

import (
	"encoding/json"
	"os"
	"slices"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/settings"
)

// The keys are kept in the order the user drags them into (alextjy on
// Discord): move-key puts one at a place, the store on disk keeps it, so a
// restart lists them the same way, and nothing else about any key changes,
// the sharing key's mirror in the settings included.
func TestMoveKeyKeepsOrder(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	var ids []string
	for _, name := range []string{"Laptop", "Phone", "Desk display", "CI"} {
		if _, err := Update("add-key", Change{Name: name, ReadOnly: name == "Desk display"}); err != nil {
			t.Fatal(err)
		}
	}
	keys, _ := List()
	for _, k := range keys {
		ids = append(ids, k.ID)
	}
	// the first is the sharing key, with a limit and models of its own
	s := settings.Load()
	s.LANKeyID = ids[0]
	if err := settings.Save(s); err != nil {
		t.Fatal(err)
	}
	if _, err := Update("models-key", Change{Key: ids[0], Models: []string{"openai/*"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := Update("off-key", Change{Key: ids[3]}); err != nil {
		t.Fatal(err)
	}
	before, _ := Export()
	if !before[0].LAN || before[0].Limit != nil {
		t.Fatal("the sharing key as set up:", before[0])
	}
	lan := settings.Load()

	move := func(id string, to int) {
		t.Helper()
		if _, err := Update("move-key", Change{Key: id, Index: &to}); err != nil {
			t.Fatal(err)
		}
	}
	order := func() []string {
		t.Helper()
		// read from the file itself, as a uni-switch started again would
		b, err := os.ReadFile(Path())
		if err != nil {
			t.Fatal(err)
		}
		var ks []Key
		if err := json.Unmarshal(b, &ks); err != nil {
			t.Fatal(err)
		}
		var out []string
		for _, k := range ks {
			out = append(out, k.Name)
		}
		return out
	}
	move(ids[2], 0) // Desk display to the top
	if got := order(); !slices.Equal(got, []string{"Desk display", "Laptop", "Phone", "CI"}) {
		t.Fatal("to the top:", got)
	}
	move(ids[0], 3) // the sharing key to the foot
	if got := order(); !slices.Equal(got, []string{"Desk display", "Phone", "CI", "Laptop"}) {
		t.Fatal("to the foot:", got)
	}
	move(ids[3], 2) // where it is: nothing moves
	if got := order(); !slices.Equal(got, []string{"Desk display", "Phone", "CI", "Laptop"}) {
		t.Fatal("in place:", got)
	}
	keys, _ = List()
	if got := []string{keys[0].Name, keys[1].Name, keys[2].Name, keys[3].Name}; !slices.Equal(got, order()) {
		t.Fatal("List:", got)
	}

	// each key is as it was, but for its place
	after, _ := Export()
	byID := func(ks []Key) map[string]string {
		m := map[string]string{}
		for _, k := range ks {
			b, _ := json.Marshal(k)
			m[k.ID] = string(b)
		}
		return m
	}
	a, b := byID(before), byID(after)
	for id := range a {
		if a[id] != b[id] {
			t.Fatalf("key %s changed:\n%s\n%s", id, a[id], b[id])
		}
	}
	if len(a) != len(b) {
		t.Fatal("keys:", len(a), len(b))
	}
	if now := settings.Load(); now.LANKeyID != lan.LANKeyID || now.LANKey != lan.LANKey {
		t.Fatalf("the sharing key's mirror moved: %q %q, was %q %q", now.LANKeyID, now.LANKey, lan.LANKeyID, lan.LANKey)
	}
	if who, ok := Authenticate(lanSecretOf(t, after, ids[0])); !ok || who.KeyID != ids[0] {
		t.Fatal("the sharing key after moving:", who, ok)
	}

	for _, bad := range []int{-1, 4} {
		if _, err := Update("move-key", Change{Key: ids[1], Index: &bad}); err == nil {
			t.Fatal("a place out of the list:", bad)
		}
	}
	if _, err := Update("move-key", Change{Key: ids[1]}); err == nil {
		t.Fatal("no place")
	}
	if got := order(); !slices.Equal(got, []string{"Desk display", "Phone", "CI", "Laptop"}) {
		t.Fatal("after refused moves:", got)
	}
}

func lanSecretOf(t *testing.T, ks []Key, id string) string {
	t.Helper()
	i := slices.IndexFunc(ks, func(k Key) bool { return k.ID == id })
	if i < 0 {
		t.Fatal("no key", id)
	}
	return ks[i].Secret
}

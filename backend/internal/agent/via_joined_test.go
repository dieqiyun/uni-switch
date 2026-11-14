package agent

import (
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"testing"
	"unsafe"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// A model field's list is its own models then the catalog's: the append
// copied the whole catalog for each field of each agent on every poll.
// withVia keeps the list joined while the own models read the same and the
// catalog's list is the one kept, and joins it again when either changed.
func TestWithViaKeepsTheJoinedList(t *testing.T) {
	syncHome(t)
	if err := provider.Save(provider.Provider{ID: "oa", Name: "OA", Key: "k", Chat: "http://127.0.0.1:1/v1", Models: []string{"gpt-5.5", "gpt-5.4"}}); err != nil {
		t.Fatal(err)
	}
	own := func(v string) []Option { return []Option{{Value: v, Note: "own"}} }
	want := func(o []Option) []Option {
		return append(slices.Clone(o), viaUniSwitchShared("pi", uniSwitchID+"/")...)
	}
	equal := func(a, b []Option) bool { return slices.EqualFunc(a, b, sameOption) }

	first := withVia(own("m1"), "pi", uniSwitchID+"/")
	if len(first) < 3 || !equal(first, want(own("m1"))) {
		t.Fatalf("the joined list is not own then the catalog: %+v", first)
	}
	again := withVia(own("m1"), "pi", uniSwitchID+"/")
	if &again[0] != &first[0] {
		t.Fatal("the same own models joined the catalog again")
	}
	if n := testing.AllocsPerRun(20, func() { withVia(own("m1"), "pi", uniSwitchID+"/") }); n > 60 {
		t.Fatalf("a list already joined took %.0f allocations", n)
	}
	if cap(again) != len(again) {
		t.Fatal("an append onto the kept list would write into it")
	}

	// own models that read otherwise, an icon among them, are joined anew
	changed := own("m1")
	changed[0].Icons = []string{"x"}
	if got := withVia(changed, "pi", uniSwitchID+"/"); &got[0] == &first[0] || !equal(got, want(changed)) {
		t.Fatalf("own models changed since are not what is listed: %+v", got[0])
	}
	if got := withVia(own("m2"), "pi", uniSwitchID+"/"); got[0].Value != "m2" || !equal(got, want(own("m2"))) {
		t.Fatalf("own models changed since are not what is listed: %+v", got[0])
	}

	// a provider saved since: the catalog's list is built again, and so
	// is the joined one
	if err := provider.Save(provider.Provider{ID: "nb", Name: "NB", Key: "k", Chat: "http://127.0.0.1:1/v1", Models: []string{"x"}}); err != nil {
		t.Fatal(err)
	}
	got := withVia(own("m2"), "pi", uniSwitchID+"/")
	if !slices.ContainsFunc(got, func(o Option) bool { return o.Value == uniSwitchID+"/nb/x" }) || !equal(got, want(own("m2"))) {
		t.Fatalf("a provider saved since is not listed: %d options", len(got))
	}

	if got := withVia(nil, "pi", uniSwitchID+"/"); !sameList(got, viaUniSwitchShared("pi", uniSwitchID+"/")) {
		t.Fatal("with no own models the catalog's list is not the one kept")
	}
}

// sameOption reads every field of an Option: one added and left out of it
// would keep a joined list whose own models changed only in that field.
func TestSameOptionReadsEveryField(t *testing.T) {
	typ := reflect.TypeFor[Option]()
	for i := range typ.NumField() {
		var a, b Option
		f := reflect.ValueOf(&b).Elem().Field(i)
		f = reflect.NewAt(f.Type(), unsafe.Pointer(f.UnsafeAddr())).Elem()
		switch f.Kind() {
		case reflect.String:
			f.SetString("x")
		case reflect.Bool:
			f.SetBool(true)
		case reflect.Int:
			f.SetInt(1)
		case reflect.Float64:
			f.SetFloat(1)
		case reflect.Slice:
			f.Set(reflect.ValueOf([]string{"x"}))
		default:
			t.Fatalf("Option.%s is a %s, which this test can't set", typ.Field(i).Name, f.Kind())
		}
		if sameOption(a, b) {
			t.Errorf("sameOption doesn't read Option.%s", typ.Field(i).Name)
		}
	}
}

// Claude Code's model field joins its own models with the list it keeps of
// its own (claudeViaShared), on this machine and in a distro asleep, and
// keeps that joined list too.
func TestClaudeModelFieldKeepsTheJoinedList(t *testing.T) {
	syncHome(t)
	if err := provider.Save(provider.Provider{ID: "oa", Name: "OA", Key: "k", Chat: "http://127.0.0.1:1/v1", Models: []string{"gpt-5.5", "gpt-5.4"}}); err != nil {
		t.Fatal(err)
	}
	home := os.Getenv("HOME")
	writeFile(t, filepath.Join(home, ".claude", "settings.json"), `{}`)
	field := claude(home).Field("model")
	first := field.Options(map[string]string{})
	if !slices.ContainsFunc(first, func(o Option) bool { return o.Value == "oa/gpt-5.4" }) {
		t.Fatal("the model field doesn't offer oa's models")
	}
	if again := field.Options(map[string]string{}); &again[0] != &first[0] {
		t.Fatal("Claude Code's own models joined its list from uni-switch again")
	}
	if err := provider.Save(provider.Provider{ID: "nb", Name: "NB", Key: "k", Chat: "http://127.0.0.1:1/v1", Models: []string{"y"}}); err != nil {
		t.Fatal(err)
	}
	if got := field.Options(map[string]string{}); !slices.ContainsFunc(got, func(o Option) bool { return o.Value == "nb/y" }) {
		t.Fatal("a provider saved since is not offered")
	}
}

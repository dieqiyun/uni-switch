package pluginrt

import (
	"reflect"
	"strings"
	"testing"
)

type testSettings struct {
	Page  int     `json:"page" title:"Rows at a time" description:"How many" enum:"10,20,50"`
	Ratio float64 `json:"ratio" title:"Ratio" min:"0" max:"1"`
	Shown bool    `json:"shown" title:"Shown"`
	Name  string  `json:"name" title:"Name"`
	skip  int
	Not   int `json:"-"`
}

// A settings struct is its schema, in its fields' order, and its values
// are the defaults.
func TestSchemaOf(t *testing.T) {
	s, defs := SchemaOf(testSettings{Page: 20, Ratio: 0.5, Name: "x"})
	if !reflect.DeepEqual(s.Order, []string{"page", "ratio", "shown", "name"}) {
		t.Fatalf("order: %v", s.Order)
	}
	if p := s.Properties["page"]; p.Type != "integer" || !reflect.DeepEqual(p.Enum, []any{int64(10), int64(20), int64(50)}) || p.Default != int64(20) || p.Title != "Rows at a time" {
		t.Fatalf("page: %+v", p)
	}
	if p := s.Properties["ratio"]; p.Type != "number" || *p.Minimum != 0 || *p.Maximum != 1 {
		t.Fatalf("ratio: %+v", p)
	}
	if !reflect.DeepEqual(defs, map[string]any{"page": int64(20), "ratio": 0.5, "shown": false, "name": "x"}) {
		t.Fatalf("defaults: %v", defs)
	}
	for _, bad := range []any{struct {
		A int `json:"a"`
	}{}, struct {
		A []int `json:"a" title:"A"`
	}{}, struct {
		A int `json:"a" title:"A" enum:"1,2"`
	}{A: 3}} {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("%T: no panic", bad)
				}
			}()
			SchemaOf(bad)
		}()
	}
}

// Only what the schema allows is taken, as JSON and YAML give it.
func TestValidate(t *testing.T) {
	s, _ := SchemaOf(testSettings{Page: 20})
	ok, err := s.Validate(map[string]any{"page": float64(50), "ratio": 1, "shown": true, "name": "y"})
	if err != nil || !reflect.DeepEqual(ok, map[string]any{"page": int64(50), "ratio": 1.0, "shown": true, "name": "y"}) {
		t.Fatalf("good: %v %v", ok, err)
	}
	for _, bad := range []map[string]any{
		{"page": 30.0}, {"page": 20.5}, {"page": "20"}, {"ratio": 2.0}, {"shown": "yes"}, {"name": 1}, {"other": 1},
	} {
		if _, err := s.Validate(bad); err == nil {
			t.Errorf("%v taken", bad)
		}
	}
}

// SetConfig keeps the user's value in plugins.yml, the rest of the file as
// it was; a value the schema refuses writes nothing; Compose leaves out a
// value edited in by hand that it doesn't allow.
func TestSetConfig(t *testing.T) {
	home(t)
	Register(Manifest{ID: "schema-test", Settings: testSettings{Page: 20}})
	m := Registered()[slicesIndex(Registered(), "schema-test")]
	if m.Defaults["page"] != int64(20) || m.Schema() == nil {
		t.Fatalf("registered: %+v", m)
	}
	if err := Set("other", false); err != nil {
		t.Fatal(err)
	}
	if err := SetConfig("schema-test", map[string]any{"page": 50.0}); err != nil {
		t.Fatal(err)
	}
	if err := SetConfig("schema-test", map[string]any{"page": 30.0}); err == nil || !strings.Contains(err.Error(), "not one of") {
		t.Fatalf("30 taken: %v", err)
	}
	if err := SetConfig("nope", map[string]any{"page": 50.0}); err == nil {
		t.Fatal("an unknown plugin taken")
	}
	f, _, err := Load()
	if err != nil || len(f.Plugins) != 2 || f.Plugins[0].ID != "other" || f.Plugins[1].Config["page"] != 50 {
		t.Fatalf("after: %+v %v", f, err)
	}
	c := byID(Compose([]Manifest{m}, f))["schema-test"]
	if c.Config["page"] != int64(50) || c.Sources["page"] != FromUser {
		t.Fatalf("composed: %+v", c)
	}
	f.Plugins[1].Config["page"] = 7
	c = byID(Compose([]Manifest{m}, f))["schema-test"]
	if c.Config["page"] != int64(20) || c.Sources["page"] != FromDefault {
		t.Fatalf("a value it doesn't allow: %+v", c)
	}
}

func slicesIndex(ms []Manifest, id string) int {
	for i, m := range ms {
		if m.ID == id {
			return i
		}
	}
	return -1
}

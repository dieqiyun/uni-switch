package provider

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func TestRetiredGroupsRemainInert(t *testing.T) {
	isolate(t)
	p := Provider{ID: "a", Name: "A", Key: "synthetic-a", Chat: "http://127.0.0.1:1/v1", Models: []string{"m"}}
	if err := Save(p); err != nil {
		t.Fatal(err)
	}
	q := p
	q.ID = "b"
	q.Name = "B"
	if err := Save(q); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(Path())
	if err != nil {
		t.Fatal(err)
	}
	var stored map[string]json.RawMessage
	if err = json.Unmarshal(data, &stored); err != nil {
		t.Fatal(err)
	}
	legacy := json.RawMessage(`[{"id":"legacy","members":["a/m","b/m"],"routing":"rotate","rules":[{"use":"b/m"}]}]`)
	stored["groups"] = legacy
	stored["noAutoGroups"] = json.RawMessage("false")
	data, _ = json.Marshal(stored)
	if err = os.WriteFile(Path(), data, 0600); err != nil {
		t.Fatal(err)
	}
	for _, e := range Served() {
		if IsGroupRef(e.ID) || strings.HasPrefix(e.ID, "auto/") {
			t.Fatalf("retired group served: %s", e.ID)
		}
	}
	if _, _, ok := Resolve("group/legacy"); ok {
		t.Fatal("legacy group resolved")
	}
	if _, _, ok := Resolve("a/m"); !ok {
		t.Fatal("direct model lost")
	}
	p.Fallback = []string{"group/legacy", "b/m"}
	if ms := FallbackModels(p); len(ms) != 1 || ms[0].Provider.ID != "b" {
		t.Fatalf("fallbacks: %+v", ms)
	}
	p.Name = "Edited"
	if err = Save(p); err != nil {
		t.Fatal(err)
	}
	data, _ = os.ReadFile(Path())
	if err = json.Unmarshal(data, &stored); err != nil {
		t.Fatal(err)
	}
	var want, got any
	json.Unmarshal(legacy, &want)
	json.Unmarshal(stored["groups"], &got)
	a, _ := json.Marshal(want)
	b, _ := json.Marshal(got)
	if string(a) != string(b) {
		t.Fatal("editing a provider changed inert legacy data")
	}
}

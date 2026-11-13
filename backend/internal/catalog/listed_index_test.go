package catalog

import (
	"fmt"
	"runtime"
	"strings"
	"testing"
)

// A model's levels are looked up through an index of each provider's
// keys, made once per catalog load: listedBy read every model of every
// provider asked through SameModel's regexp on each ask, and every look
// at the providers asks it for each model. A catalog loaded since is
// read at once.
func TestListedByReadsAnIndex(t *testing.T) {
	big := func(levels string) string {
		var b strings.Builder
		b.WriteString(`{"big": {"models": {`)
		for i := range 2000 {
			fmt.Fprintf(&b, `"vendor/Model_%d.5": {"id":"model-%d.5"},`, i, i)
		}
		b.WriteString(`"x": {"id":"x"}}},
		  "zai": {"models": {"glm-5.3-flash": {"id":"glm-5.3-flash","reasoning_options":[{"type":"effort","values":[` + levels + `]}]}}}}`)
		return b.String()
	}
	writeCatalog(t, big(`"low","high","max"`))
	ask := func() string {
		got, _ := ListedByBorrowed([]string{"big", "zai"}, "GLM 5-3 flash")
		return strings.Join(got, ",")
	}
	// under another spelling (SameModel), after every provider's keys as given
	if got := ask(); got != "low,high,max" {
		t.Fatalf("glm-5.3-flash under another spelling: %q", got)
	}
	if got, ok := ListedByBorrowed([]string{"big"}, "big/model_7.5"); !ok || len(got) != 0 {
		t.Fatalf("a key under its bare id: %v %v", got, ok)
	}
	var a, b runtime.MemStats
	runtime.ReadMemStats(&a)
	for range 20 {
		ask()
	}
	runtime.ReadMemStats(&b)
	// a SameModel of each of 2000 keys is ~200KB; a look in the index a
	// few strings
	if per := (b.TotalAlloc - a.TotalAlloc) / 20; per > 8<<10 {
		t.Fatalf("an ask took %d bytes with the catalog unchanged", per)
	}

	writeCatalog(t, big(`"low","high"`))
	if got := ask(); got != "low,high" {
		t.Fatalf("a catalog loaded since is not read: %q", got)
	}
}

// A model newer than the catalog takes its line's levels from the same
// index: KinEfforts split and joined every key of every maker on each ask.
func TestKinEffortsReadsAnIndex(t *testing.T) {
	big := func(levels string) string {
		var b strings.Builder
		b.WriteString(`{"big": {"models": {`)
		for i := range 2000 {
			fmt.Fprintf(&b, `"vendor/model-%d-v1.5": {"id":"model-%d-v1.5","reasoning":true},`, i, i)
		}
		b.WriteString(`"x": {"id":"x"}}},
		  "zai": {"models": {"glm-5.3-flash": {"id":"glm-5.3-flash","reasoning_options":[{"type":"effort","values":[` + levels + `]}]}}}}`)
		return b.String()
	}
	writeCatalog(t, big(`"low","high","max"`))
	ask := func() string {
		got, _ := KinEfforts([]string{"big", "zai"}, "glm-6.1-flash")
		return strings.Join(got, ",")
	}
	if got := ask(); got != "low,high,max" {
		t.Fatalf("glm-6.1-flash's line: %q", got)
	}
	var a, b runtime.MemStats
	runtime.ReadMemStats(&a)
	for range 20 {
		ask()
	}
	runtime.ReadMemStats(&b)
	// a versionless of each of 2000 keys is ~200KB; a look in the index a
	// few strings
	if per := (b.TotalAlloc - a.TotalAlloc) / 20; per > 8<<10 {
		t.Fatalf("an ask took %d bytes with the catalog unchanged", per)
	}

	writeCatalog(t, big(`"low","high"`))
	if got := ask(); got != "low,high" {
		t.Fatalf("a catalog loaded since is not read: %q", got)
	}
}

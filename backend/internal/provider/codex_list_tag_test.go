package provider

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/catalog"
	"github.com/dieqiyun/uni-switch/backend/internal/filememo"
	"github.com/dieqiyun/uni-switch/backend/internal/settings"
)

// listTagHome is a ChatGPT account signed in to Codex beside 8 providers
// of 40 models each, as a uni-switch in daily use has them, its files written
// long ago.
func listTagHome(t *testing.T) {
	t.Helper()
	signIn(t)
	for p := range 8 {
		id := fmt.Sprintf("relay%d", p)
		var ids []string
		var live []catalog.Model
		for m := range 40 {
			ids = append(ids, fmt.Sprintf("model-%d", m))
			live = append(live, catalog.Model{ID: fmt.Sprintf("model-%d", m), Context: 200000})
		}
		if err := Save(Provider{ID: id, Name: id, Chat: "https://" + id + ".test/v1", Key: "k", Models: ids}); err != nil {
			t.Fatal(err)
		}
		if err := catalog.SaveLive(id, "https://"+id+".test/v1", live); err != nil {
			t.Fatal(err)
		}
	}
	writeCodexConfig(t, "model_reasoning_effort = \"high\"\n")
	ageListFiles(t)
}

func writeCodexConfig(t *testing.T, body string) {
	t.Helper()
	path := filepath.Join(catalog.CodexHome(), "config.toml")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// ageListFiles has the files the tag is stamped by written long ago, as
// a uni-switch at rest has them, all at one time: a file changes its stamp
// by its size alone, as a write within one tick of a coarse clock does.
func ageListFiles(t *testing.T) {
	t.Helper()
	old := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	home := catalog.CodexHome()
	for _, p := range []string{Path(), settings.Path(), catalog.CachePath(),
		filepath.Join(home, "config.toml"), filepath.Join(home, "auth.json"), catalog.CodexModelsCache()} {
		if err := os.Chtimes(p, old, old); err != nil && !os.IsNotExist(err) {
			t.Fatal(err)
		}
	}
}

// measure is the allocations and bytes of one call of f, on average.
func measure(f func()) (allocs, bytes float64) {
	const runs = 50
	f()
	var a, z runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&a)
	for range runs {
		f()
	}
	runtime.ReadMemStats(&z)
	return float64(z.Mallocs-a.Mallocs) / runs, float64(z.TotalAlloc-a.TotalAlloc) / runs
}

// The tag of Codex's list is asked for on every GET of Codex's models and
// every backend reply, and built the whole catalog each time: 22 MB a call
// here, 2.1 GB in an hour of a uni-switch at rest. While nothing it is built
// from changed, it is kept.
func TestCodexListTagKeptWhileNothingChanged(t *testing.T) {
	listTagHome(t)
	want := buildCodexListTag("")
	wantOwn := buildCodexListTag("own")
	if CodexListTag() != want || CodexOwnListTag() != wantOwn {
		t.Fatal("the tag isn't the one built afresh")
	}
	n, by := measure(func() { CodexListTag(); CodexOwnListTag() })
	t.Logf("CodexListTag and CodexOwnListTag: %.0f allocs, %.0f bytes a call", n, by)
	if n > 200 {
		t.Errorf("with nothing changed, the tags took %.0f allocations (%.0f bytes) a call: the catalog built again", n, by)
	}
	release := Hold()
	n, by = measure(func() { CodexListTag(); CodexOwnListTag() })
	release()
	t.Logf("held: %.0f allocs, %.0f bytes a call", n, by)
	if n > 200 {
		t.Errorf("held, the tags took %.0f allocations (%.0f bytes) a call", n, by)
	}
	if CodexListTag() != want || CodexOwnListTag() != wantOwn {
		t.Fatal("the kept tag isn't the one built afresh")
	}
}

// A kept tag follows every change to what it is built from: uni-switch's own
// writes, and files another process writes behind its back.
func TestCodexListTagFollowsChanges(t *testing.T) {
	listTagHome(t)
	step := func(name string, change func(), unchanged ...bool) {
		t.Helper()
		before := CodexListTag()
		if before != CodexListTag() {
			t.Fatalf("%s: the tag moved with nothing changed", name)
		}
		change()
		ageListFiles(t)
		got := CodexListTag()
		stable := len(unchanged) > 0 && unchanged[0]
		if got == before && !stable {
			t.Errorf("%s: the tag stayed %s", name, got)
		}
		if got != before && stable {
			t.Errorf("%s: vendor metadata replaced the fixed context default", name)
		}
		if want := buildCodexListTag(""); got != want {
			t.Errorf("%s: the tag is %s, built afresh %s", name, got, want)
		}
	}
	step("a provider saved", func() {
		if err := Save(Provider{ID: "more", Name: "more", Chat: "https://more.test/v1", Key: "k", Models: []string{"m-1"}}); err != nil {
			t.Fatal(err)
		}
	})
	step("a setting saved", func() {
		s := settings.Load()
		s.CodexAutoReview = "relay0/model-1"
		if err := settings.Save(s); err != nil {
			t.Fatal(err)
		}
	})
	// a list's windows rewritten behind the stamp's files: only what
	// this process is told of says so
	relist := func(id string, n int) {
		t.Helper()
		b, err := os.ReadFile(catalog.LivePath(id))
		if err != nil {
			t.Fatal(err)
		}
		b = bytes.ReplaceAll(b, []byte(`"Context": 200000`), []byte(fmt.Sprintf(`"Context": %d`, n)))
		if err := os.WriteFile(catalog.LivePath(id), b, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	step("a list written, filememo told", func() {
		relist("relay1", 120000)
		filememo.Forget()
	}, true)
	step("a list written, what is held dropped (a plugin's change)", func() {
		relist("relay2", 110000)
		dropHeld()
	}, true)
	step("a vendor's list fetched", func() {
		if err := catalog.SaveLive("relay3", "https://relay3.test/v1", []catalog.Model{{ID: "model-1", Context: 100000}}); err != nil {
			t.Fatal(err)
		}
	}, true)
	step("a user changes a model's context", func() {
		p, err := Find("relay0")
		if err != nil {
			t.Fatal(err)
		}
		p.Contexts = map[string]int{"model-1": 128000}
		if err := Save(*p); err != nil {
			t.Fatal(err)
		}
	})
	step("Codex's effort set in Codex", func() {
		writeCodexConfig(t, "model_reasoning_effort = \"low\"\n")
	})

	// the providers' file as another uni-switch (the CLI) leaves it: nothing
	// in this process is told
	before, err := os.ReadFile(Path())
	if err != nil {
		t.Fatal(err)
	}
	if err := Save(Provider{ID: "elsewhere", Name: "elsewhere", Chat: "https://elsewhere.test/v1", Key: "k", Models: []string{"e-1"}}); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(Path())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(Path(), before, 0o600); err != nil {
		t.Fatal(err)
	}
	ageListFiles(t)
	step("the providers' file written by another uni-switch", func() {
		if err := os.WriteFile(Path(), after, 0o600); err != nil {
			t.Fatal(err)
		}
	})
}

// What the stamp can't see reaches the tag within codexListTagFor.
func TestCodexListTagBuiltAgainAfterItsTime(t *testing.T) {
	listTagHome(t)
	CodexListTag()
	old := codexListTagFor
	codexListTagFor = 0
	t.Cleanup(func() { codexListTagFor = old })
	if n, _ := measure(func() { CodexListTag() }); n < 1000 {
		t.Errorf("past its time, the tag took %.0f allocations a call: not built again", n)
	}
}

// A file written just now may be written again inside one tick of a
// coarse clock with the same size: a tag built then isn't kept.
func TestCodexListTagNotKeptOverAFreshFile(t *testing.T) {
	listTagHome(t)
	writeCodexConfig(t, "model_reasoning_effort = \"high\"\n")
	CodexListTag()
	if n, _ := measure(func() { CodexListTag() }); n < 1000 {
		t.Errorf("over a file written just now, the tag took %.0f allocations a call: kept", n)
	}
}

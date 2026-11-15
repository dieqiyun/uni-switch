package provider

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"testing"
	"time"
	"unsafe"

	"github.com/dieqiyun/uni-switch/backend/internal/catalog"
	"github.com/dieqiyun/uni-switch/backend/internal/filememo"
)

// A look at one model of the vendor's list (its APIs, price, tiers), asked
// for every model a page lists, reads the model as the list has it, not a
// copy: the copies were 32 MB of a live uni-switch's 2.6 GB in a minute and a
// half. A caller's append is its own, and a model the user gave its own id
// is still a copy with that id.
func TestLiveModelReadWithoutACopy(t *testing.T) {
	isolate(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
	var ms []catalog.Model
	for i := range 200 {
		ms = append(ms, catalog.Model{ID: fmt.Sprintf("m-%d", i), Name: fmt.Sprintf("M %d", i), APIs: []string{"openai-responses"}})
	}
	if err := catalog.SaveLive("r", "http://127.0.0.1:1/v1", ms); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-time.Hour)
	if err := os.Chtimes(catalog.LivePath("r"), old, old); err != nil {
		t.Fatal(err)
	}
	p := Provider{ID: "r", Name: "R", Key: "k", Chat: "http://127.0.0.1:1/v1", Models: []string{"m-7"}}
	defer Hold()()
	defer filememo.Hold()()
	if got := p.ListedAPIs("m-7"); !slices.Equal(got, []Protocol{"openai-responses"}) {
		t.Fatalf("m-7's APIs: %v", got)
	}
	bytesPer := func(f func()) uint64 {
		var before, after runtime.MemStats
		runtime.ReadMemStats(&before)
		for range 200 {
			f()
		}
		runtime.ReadMemStats(&after)
		return (after.TotalAlloc - before.TotalAlloc) / 200
	}
	// the steps before the list is read (whether it is the provider's, the
	// id it is listed by) and the read itself, the file's look, are the
	// same either way; what is over them is the copy. Each costs more on
	// Windows, whose stat allocates more.
	read := bytesPer(func() { _ = p.lists(); _ = p.OwnModelID("m-7"); catalog.LiveListed("r") })
	looked := bytesPer(func() { p.liveByID("m-7") })
	// the same steps leave nothing over; a copy, a model and more. Half a
	// model leaves room both ways.
	if size := uint64(unsafe.Sizeof(catalog.Model{})); looked >= read+size/2 {
		t.Fatalf("a look at one model took %d bytes, the list's read %d; a model is %d", looked, read, size)
	}

	got := p.liveByID("m-7")
	_ = append(got, catalog.Model{ID: "added"})
	if again := p.liveByID("m-7"); len(again) != 1 || again[0].ID != "m-7" {
		t.Fatalf("a caller's append reached the list: %+v", again)
	}
	if ms := catalog.LiveByID("r", "m-8"); len(ms) != 1 || ms[0].ID != "m-8" {
		t.Fatalf("LiveByID: %+v", ms)
	}
}

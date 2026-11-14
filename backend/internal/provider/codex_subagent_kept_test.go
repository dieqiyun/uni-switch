package provider

import (
	"path/filepath"
	"runtime"
	"strconv"
	"testing"
	"time"
)

// Codex's subagent models are kept while the catalog is held: its Agents
// row asked them each poll, a copy of Codex's whole catalog and a list
// made from it each time (17 MB of forty polls with real accounts).
func TestCodexSubagentModelsKept(t *testing.T) {
	isolate(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
	defer func(d, k time.Duration) { heldFor, keptFor = d, k }(heldFor, keptFor)
	heldFor, keptFor = time.Minute, time.Minute
	var models []string
	for i := range 200 {
		models = append(models, "m"+strconv.Itoa(i))
	}
	if err := Save(Provider{ID: "relay", Name: "Relay", Key: "k", Chat: "http://127.0.0.1:1/v1", Models: models}); err != nil {
		t.Fatal(err)
	}
	defer Hold()()
	if n := len(CodexSubagentModels()); n < 200 {
		t.Fatalf("Codex's subagents may go on %d models", n)
	}
	built := allocatedBy(func() { codexSubagentModels() })
	kept := allocatedBy(func() { CodexSubagentModels() })
	if kept*4 > built {
		t.Fatalf("asking Codex's subagent models again allocated %d bytes, building them %d", kept, built)
	}
}

// allocatedBy is how many bytes a call of f allocates, on average of ten.
func allocatedBy(f func()) uint64 {
	f()
	var a, b runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&a)
	for range 10 {
		f()
	}
	runtime.ReadMemStats(&b)
	return (b.TotalAlloc - a.TotalAlloc) / 10
}

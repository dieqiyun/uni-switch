package edit

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"
)

// GetYAML and its siblings are asked of an agent's YAML settings per
// field, every window poll (dsh's 80 KB settings.yaml); a look at a file
// that hasn't changed doesn't parse it again. It still sees a BOM in
// front, a nested map, a list and a block's text, and a write, its own or
// another process's.
func TestGetYAMLParsesTheFileOnce(t *testing.T) {
	var b strings.Builder
	b.WriteString("\xef\xbb\xbf# dsh settings\nagent:\n  model: fake/m1\n  levels:\n    low: a\n    high: b\n  tags: [x, y]\nhistory:\n")
	for i := range 3000 {
		fmt.Fprintf(&b, "  - {id: entry-%d, note: \"some words to make it longer\"}\n", i)
	}
	path := filepath.Join(t.TempDir(), "settings.yaml")
	if err := os.WriteFile(path, []byte(b.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-time.Hour)
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatal(err)
	}
	if v, ok := GetYAML(path, "agent.model"); !ok || v != "fake/m1" {
		t.Fatalf("model: %q %v", v, ok)
	}
	if m := GetYAMLMap(path, "agent.levels"); m["low"] != "a" || m["high"] != "b" || len(m) != 2 {
		t.Fatalf("levels: %v", m)
	}
	if l := GetYAMLList(path, "agent.tags"); strings.Join(l, ",") != "x,y" {
		t.Fatalf("tags: %v", l)
	}
	if v, ok := GetYAMLText(path, "agent.tags"); !ok || v != "[x, y]" {
		t.Fatalf("tags as text: %q %v", v, ok)
	}

	const looks = 20
	var m0, m1 runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&m0)
	for range looks {
		if v, _ := GetYAML(path, "agent.model"); v != "fake/m1" {
			t.Fatalf("model: %q", v)
		}
		GetYAMLMap(path, "agent.levels")
	}
	runtime.ReadMemStats(&m1)
	if per, limit := (m1.TotalAlloc-m0.TotalAlloc)/looks, uint64(b.Len()/10); per > limit {
		t.Errorf("a look at a %d KB file allocates %d KB, over %d KB", b.Len()>>10, per>>10, limit>>10)
	}

	if err := SetYAML(path, KV{Path: "agent.model", Value: "fake/m2"}); err != nil {
		t.Fatal(err)
	}
	if v, _ := GetYAML(path, "agent.model"); v != "fake/m2" {
		t.Errorf("after a write: %q, want fake/m2", v)
	}
	// another process's write, a moment later
	raw, _ := os.ReadFile(path)
	if err := os.WriteFile(path, []byte(strings.Replace(string(raw), "fake/m2", "fake/m3", 1)), 0o600); err != nil {
		t.Fatal(err)
	}
	later := time.Now().Add(time.Minute)
	os.Chtimes(path, later, later)
	if v, _ := GetYAML(path, "agent.model"); v != "fake/m3" {
		t.Errorf("after another's write: %q, want fake/m3", v)
	}
	// gone, then not YAML: nothing read, as before
	os.Remove(path)
	if v, ok := GetYAML(path, "agent.model"); ok || v != "" {
		t.Errorf("gone: %q %v", v, ok)
	}
	os.WriteFile(path, []byte("- a\n- b\n"), 0o600)
	if v, ok := GetYAML(path, "agent.model"); ok || v != "" {
		t.Errorf("a list: %q %v", v, ok)
	}
}

// blockSequence, BlockList's shortcut, says a block list for exactly the
// texts whose first document a decode finds a block sequence in.
func TestBlockSequenceAgreesWithADecode(t *testing.T) {
	for _, raw := range []string{
		"- a\n- b\n",
		"-\n",
		"-\ta\n",
		"# head\n\n- id: a\n  x: 1\n",
		"---\n- a\n",
		"--- - a\n",
		"--- # c\n- a\n",
		"%YAML 1.2\n---\n- a\n",
		"  - a\n  - b\n",
		"- a\r\n- b\r\n",
		"[]\n",
		"[a, b]\n",
		"# c\n[\n  {id: a},\n]\n",
		"--- [a]\n",
		"---\n[a]\n",
		"a: 1\n",
		"-1\n",
		"--x\n",
		"---\n",
		"",
		"# only a comment\n",
		"key: [- a]\n",
		"!!seq [a]\n",
		"&x [a]\n",
	} {
		var doc yaml.Node
		dec := yaml.NewDecoder(strings.NewReader(raw))
		ok := dec.Decode(&doc) == nil && len(doc.Content) == 1 && doc.Content[0].Kind == yaml.SequenceNode
		block, flow := ok && doc.Content[0].Style&yaml.FlowStyle == 0, ok && doc.Content[0].Style&yaml.FlowStyle != 0
		// a block list is told without a decode; a flow list never is
		// (text that isn't YAML may go either way: BlockList leaves it)
		if got := blockSequence(raw); block && !got || flow && got {
			t.Errorf("%q: blockSequence %v, a decode: block %v, flow %v", raw, got, block, flow)
		}
		if blockSequence(raw) {
			if _, ok := BlockList(raw); ok {
				t.Errorf("%q: a block list rewritten", raw)
			}
		}
	}
	if got, ok := BlockList("[{id: a}, {id: b}]\n"); !ok || !strings.Contains(got, "- id: a") {
		t.Errorf("a flow list: %q %v", got, ok)
	}
}

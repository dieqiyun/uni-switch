package pluginrt

import (
	"errors"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// A plugin is applied once what it requires is provided, whatever the
// order; one that fails or panics is FAILED, what it registered undone,
// and the rest run on; turned off, what it registered is undone newest
// first, with the plugins that require what it provided.
func TestReconcile(t *testing.T) {
	var log []string
	note := func(s string) func() { return func() { log = append(log, s) } }
	ms := []Manifest{
		{ID: "a", Requires: []string{"svc.b"}, Apply: func(c *Ctx) error {
			if c.Service("svc.b") != "b" {
				t.Errorf("a got %v", c.Service("svc.b"))
			}
			c.OnDispose(note("a.1"))
			c.OnDispose(note("a.2"))
			return nil
		}},
		{ID: "b", Provides: []string{"svc.b"}, Apply: func(c *Ctx) error {
			c.Provide("svc.b", "b")
			c.OnDispose(note("b"))
			return nil
		}},
		{ID: "bad", Apply: func(c *Ctx) error { c.OnDispose(note("bad")); return errors.New("no") }},
		{ID: "boom", Apply: func(c *Ctx) error { c.OnDispose(note("boom")); panic("boom") }},
		{ID: "old", API: "^2"},
		{ID: "wait", Requires: []string{"svc.none"}},
		{ID: "ui", Parts: []Part{{"nav.page", "ui"}}},
	}
	rt := New(ms)
	all := map[string]bool{"a": true, "b": true, "bad": true, "boom": true, "old": true, "wait": true, "ui": true}
	rt.Reconcile(all, nil)
	want := map[string]State{"a": Active, "b": Active, "bad": Failed, "boom": Failed, "old": Failed, "wait": Pending, "ui": Active}
	for _, s := range rt.Statuses() {
		if s.State != want[s.ID] {
			t.Errorf("%s is %s (%s), want %s", s.ID, s.State, s.Why, want[s.ID])
		}
		if s.ID == "ui" && !slices.Equal(s.Parts, []Part{{"nav.page", "ui"}}) {
			t.Errorf("ui's parts: %v", s.Parts)
		}
		if s.ID == "wait" && !strings.Contains(s.Why, "svc.none") {
			t.Errorf("wait says %q", s.Why)
		}
	}
	if !slices.Equal(log, []string{"bad", "boom"}) {
		t.Fatalf("the failed ones' parts undone: %v", log)
	}
	log = nil
	// b off: a, which requires it, goes first, its parts newest first
	off := map[string]bool{"a": true, "ui": true}
	rt.Reconcile(off, nil)
	if !slices.Equal(log, []string{"a.2", "a.1", "b"}) {
		t.Fatalf("disposed: %v", log)
	}
	if rt.Active("a") || rt.Active("b") || !rt.Active("ui") {
		t.Fatalf("after b off: %+v", rt.Statuses())
	}
	// and back on, a with it
	rt.Reconcile(all, nil)
	if !rt.Active("a") || !rt.Active("b") {
		t.Fatalf("after b on: %+v", rt.Statuses())
	}
	log = nil
	rt.Stop()
	if !slices.Equal(log, []string{"a.2", "a.1", "b"}) {
		t.Fatalf("stopped: %v", log)
	}
}

func TestCompatible(t *testing.T) {
	for want, ok := range map[string]bool{"": true, "^1": true, "^1.0": true, "^1.1": false, "^2": false, "1": false, "^x": false} {
		if Compatible(want) != ok {
			t.Errorf("Compatible(%q) = %v", want, !ok)
		}
	}
}

// Core never imports a plugin, and a plugin never imports another: they
// reach each other through services. main.go and plugins/all alone gather
// them; a test may too.
func TestPluginsImportOnlyCore(t *testing.T) {
	root := filepath.Join("..", "..")
	const mod = "github.com/dieqiyun/uni-switch/backend/plugins/"
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		if d.IsDir() {
			if rel == "site" || rel == "build" || strings.HasPrefix(d.Name(), ".") && rel != "." {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		f, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		for _, im := range f.Imports {
			p, _ := strconv.Unquote(im.Path.Value)
			if !strings.HasPrefix(p, mod) {
				continue
			}
			switch {
			case rel == "main.go" && p == mod+"all":
			case strings.HasPrefix(rel, "plugins/all/"):
			case strings.HasPrefix(rel, "plugins/"):
				t.Errorf("%s imports %s: a plugin never imports another", rel, p)
			default:
				t.Errorf("%s imports %s: core never imports a plugin", rel, p)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	// and every plugin is gathered
	dirs, err := os.ReadDir(filepath.Join(root, "plugins"))
	if err != nil {
		t.Fatal(err)
	}
	all, err := os.ReadFile(filepath.Join(root, "plugins", "all", "all.go"))
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range dirs {
		if d.IsDir() && d.Name() != "all" && !strings.Contains(string(all), `"`+mod+d.Name()+`"`) {
			t.Errorf("plugins/all doesn't import plugins/%s", d.Name())
		}
	}
}

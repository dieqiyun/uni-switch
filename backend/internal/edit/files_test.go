package edit

import (
	"os"
	"path/filepath"
	"testing"
)

func ptr(s string) *string { return &s }
func TestAtomicReplaceDeleteAndPrivateMode(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "中文 config.json")
	for _, s := range []string{"old", "new"} {
		if e := WriteAtomic(path, &s); e != nil {
			t.Fatal(e)
		}
		b, e := os.ReadFile(path)
		if e != nil || string(b) != s {
			t.Fatal(string(b), e)
		}
	}
	if e := WriteAtomic(path, nil); e != nil {
		t.Fatal(e)
	}
	if _, e := os.Stat(path); !os.IsNotExist(e) {
		t.Fatal(e)
	}
	files, _ := os.ReadDir(dir)
	if len(files) != 0 {
		t.Fatal("temporary files leaked")
	}
}
func TestTransactionRollsBackSecondFileFailure(t *testing.T) {
	dir := t.TempDir()
	a, b := filepath.Join(dir, "a"), filepath.Join(dir, "b")
	_ = WriteAtomic(a, ptr("old"))
	_ = WriteAtomic(b, ptr("old"))
	calls := 0
	e := applyWith([]Change{{a, ptr("old"), ptr("new")}, {b, ptr("old"), ptr("new")}}, func(path string, content *string) error {
		calls++
		if calls == 2 {
			return fail("file_error", "injected second-file failure")
		}
		return WriteAtomic(path, content)
	})
	if e == nil || calls != 2 {
		t.Fatal(e, calls)
	}
	for _, path := range []string{a, b} {
		got, _ := Read(path)
		if !same(got, ptr("old")) {
			t.Fatal(path, got)
		}
	}
}
func TestApplyRejectsStaleSnapshotBeforeAnyWrite(t *testing.T) {
	dir := t.TempDir()
	a, b := filepath.Join(dir, "a"), filepath.Join(dir, "b")
	_ = WriteAtomic(a, ptr("old"))
	_ = WriteAtomic(b, ptr("external"))
	if Apply([]Change{{a, ptr("old"), ptr("new")}, {b, nil, ptr("new")}}) == nil {
		t.Fatal("ignored conflict")
	}
	got, _ := Read(a)
	if !same(got, ptr("old")) {
		t.Fatal("partial write")
	}
}
func TestRollbackKeepsConflictAndRestoresOtherFiles(t *testing.T) {
	dir := t.TempDir()
	a, b := filepath.Join(dir, "a"), filepath.Join(dir, "b")
	_ = WriteAtomic(a, ptr("after"))
	_ = WriteAtomic(b, ptr("external"))
	if Rollback([]Change{{a, ptr("before"), ptr("after")}, {b, ptr("before"), ptr("after")}}) == nil {
		t.Fatal("conflict ignored")
	}
	got, _ := Read(a)
	other, _ := Read(b)
	if !same(got, ptr("before")) || !same(other, ptr("external")) {
		t.Fatal(got, other)
	}
}

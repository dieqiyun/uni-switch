package sessions

import (
	"fmt"
	"reflect"
	"testing"
)

// byKey groups as appending file by file did: the kept files of each
// session, in their order, and a group grown later doesn't write into
// another's files.
func TestByKeyGroupsAsAppending(t *testing.T) {
	var files []file
	for i := range 40 {
		files = append(files, file{agent: []string{"codex", "claude"}[i%2], key: fmt.Sprint("k", i%7), path: fmt.Sprint("p", i)})
	}
	for _, keep := range []func(*file) bool{nil, func(f *file) bool { return f.agent == "codex" }} {
		want := map[string][]file{}
		for _, f := range files {
			if keep == nil || keep(&f) {
				want[f.key] = append(want[f.key], f)
			}
		}
		got := byKey(files, keep)
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("byKey = %v\nwant %v", got, want)
		}
		for k := range got {
			_ = append(got[k], file{key: "grown", path: "grown"})
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("after each group grew, byKey's groups = %v\nwant %v", got, want)
		}
	}
}

package settings

import (
	"strings"
	"testing"
)

// SearcherThen is kept in its order, without the searcher it follows, a
// repeat, "off" or a blank.
func TestSearcherThenSaved(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	st := Load()
	st.Searcher = "a/m"
	st.SearcherThen = []string{" b/m ", "a/m", "off", "", "c", "b/m"}
	if err := Save(st); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(Load().SearcherThen, ","); got != "b/m,c" {
		t.Errorf("SearcherThen = %q", got)
	}
}

package gateway

import (
	"strings"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
	"github.com/dieqiyun/uni-switch/backend/internal/settings"
)

// The searcher Settings names that fails, or answers without searching,
// gives way to the ones named after it, in their order (SearcherThen;
// axelburks on Discord: 代搜供应商能不能支持设置多个，一个失败了换另一个),
// and to no other: one named after it that is gone or can't search is
// passed over, and Settings says who searched in the first one's place.
func TestPickedSearchersInTheirOrder(t *testing.T) {
	for _, tc := range []struct {
		name    string
		failing bool
	}{{"answered without searching", false}, {"failed", true}} {
		t.Run(tc.name, func(t *testing.T) {
			_, next, result := pickedSearchSetup(t, tc.failing, provider.Anthropic)
			st := settings.Load()
			st.Searcher = "ds/deepseek-flash"
			st.SearcherThen = []string{"gone/m1", "glm", "ds2/deepseek-flash"}
			if err := settings.Save(st); err != nil {
				t.Fatal(err)
			}
			searchForGLM(t, New())
			if got := next.searches(); len(got) != 1 {
				t.Fatalf("ds2 searched %q", got)
			}
			if r := result(); !strings.Contains(r, "22/17℃") {
				t.Fatalf("the model was told %q", r)
			}
			f, ok := SearcherFailed()
			if !ok || f.Searcher != "ds/deepseek-flash" || f.Instead != "ds2/deepseek-flash" {
				t.Fatalf("SearcherFailed = %+v %v", f, ok)
			}

			// none after it that can search: as with none named after it
			st.SearcherThen = []string{"gone/m1", "glm"}
			if err := settings.Save(st); err != nil {
				t.Fatal(err)
			}
			searchForGLM(t, New())
			if got := next.searches(); len(got) != 1 {
				t.Fatalf("ds2, not named, searched %q", got)
			}
			if r := result(); strings.Contains(r, "22/17℃") || !strings.Contains(r, "no other provider searches in their place") {
				t.Fatalf("the model was told %q", r)
			}
			if f, ok := SearcherFailed(); !ok || f.Instead != "" {
				t.Fatalf("SearcherFailed = %+v %v", f, ok)
			}
		})
	}
}

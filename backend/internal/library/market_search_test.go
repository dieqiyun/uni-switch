package library

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
)

// skills.sh's search answering 503 while its front page answers (Ti800's
// "prototype", #1589): the search is asked once more, and when it fails
// again what the most installed list matched is still offered, with the
// error naming skills.sh as the source that didn't answer.
func TestSkillsSearch503(t *testing.T) {
	sandbox(t)
	t.Setenv("LOCALAPPDATA", t.TempDir())
	was := retryPause
	retryPause = 0
	t.Cleanup(func() { retryPause = was })
	popular.Lock()
	popular.list = nil
	popular.Unlock()
	t.Cleanup(func() { popular.Lock(); popular.list = nil; popular.Unlock() })

	var searches, fails atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/":
			list := `[{"source":"mattpocock/skills","skillId":"prototype","name":"prototype","installs":917000}]`
			js, _ := json.Marshal(`{"initialSkills":` + list + `}`)
			w.Write([]byte(`<html><script>self.__next_f.push([1,` + string(js) + `])</script></html>`))
		case "/api/search":
			if searches.Add(1) <= fails.Load() {
				http.Error(w, "", http.StatusServiceUnavailable)
				return
			}
			w.Write([]byte(`{"skills":[{"source":"acme/skills","skillId":"prototype","name":"prototype","installs":5}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	wasURL := skillsShURL
	skillsShURL = srv.URL
	t.Cleanup(func() { skillsShURL = wasURL })
	ids := func(l []MarketSkill) (out []string) {
		for _, m := range l {
			out = append(out, m.ID)
		}
		return
	}

	// a 503 then a 200: the search's own results, and no error
	fails.Store(1)
	list, err := MarketSkills("prototype")
	if err != nil || searches.Load() != 2 || !slices.Equal(ids(list), []string{"mattpocock/skills/prototype", "acme/skills/prototype"}) {
		t.Fatalf("after one 503: %v %v, %d searches", ids(list), err, searches.Load())
	}

	// 503 every time: asked twice and no more, the popular match kept, and
	// the error says which source didn't answer
	searches.Store(0)
	fails.Store(5)
	list, err = MarketSkills("prototype")
	var se *SearchError
	if !errors.As(err, &se) || se.Source != "skills.sh" || !strings.Contains(se.Err.Error(), "503") || searches.Load() != 2 {
		t.Fatalf("503 every time: %v (%T), %d searches", err, err, searches.Load())
	}
	if !slices.Equal(ids(list), []string{"mattpocock/skills/prototype"}) {
		t.Fatalf("503 every time: %v", ids(list))
	}
}

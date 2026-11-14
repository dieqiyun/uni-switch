package library

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
)

// The owner: the skill market had too little in it. Every skill skills.sh's
// front page lists is offered, then its trending page's that aren't there
// (with no count, theirs being only recent installs), each repository's
// most installed first and its others after, and a search asks for 200.
func TestMarketSkillsAll(t *testing.T) {
	sandbox(t)
	t.Setenv("LOCALAPPDATA", t.TempDir())
	popular.Lock()
	popular.list = nil
	popular.Unlock()
	t.Cleanup(func() { popular.Lock(); popular.list = nil; popular.Unlock() })

	var front, trending []string
	// big/r has 100 skills, the most installed; 50 other repositories one each
	for i := range 100 {
		front = append(front, fmt.Sprintf(`{"source":"big/r","skillId":"b%d","name":"b%d","installs":%d}`, i, i, 100000-i))
	}
	for i := range 50 {
		front = append(front, fmt.Sprintf(`{"source":"o%d/r","skillId":"s","name":"s","installs":%d}`, i, 1000-i))
	}
	// trending: one on the front page already, two new
	trending = append(trending, `{"source":"big/r","skillId":"b3","name":"b3","installs":7}`,
		`{"source":"new/r","skillId":"t1","name":"t1","installs":40}`, `{"source":"new/r","skillId":"t2","name":"t2","installs":30}`)
	page := func(list []string) []byte {
		js, _ := json.Marshal(`{"initialSkills":[` + strings.Join(list, ",") + `]}`)
		return []byte(`<html><script>self.__next_f.push([1,` + string(js) + `])</script></html>`)
	}
	var limit string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/":
			w.Write(page(front))
		case "/trending":
			w.Write(page(trending))
		case "/api/search":
			limit = r.URL.Query().Get("limit")
			w.Write([]byte(`{"skills":[]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	was := skillsShURL
	skillsShURL = srv.URL
	t.Cleanup(func() { skillsShURL = was })

	list, err := MarketSkills("")
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, m := range list {
		if !m.Featured {
			ids = append(ids, m.ID)
		}
	}
	if len(ids) != 152 {
		t.Fatalf("%d offered, want all 152: %v", len(ids), ids)
	}
	// the first round has each repository's most installed: big/r's b0,
	// the 50 others, new/r's t1; then big/r's b1 and new/r's t2
	if ids[0] != "big/r/b0" || ids[1] != "o0/r/s" || ids[50] != "o49/r/s" || ids[51] != "new/r/t1" || ids[52] != "big/r/b1" || ids[53] != "new/r/t2" || ids[151] != "big/r/b99" {
		t.Fatalf("order: %v", ids[:60])
	}
	i := slices.IndexFunc(list, func(m MarketSkill) bool { return m.ID == "new/r/t1" })
	j := slices.IndexFunc(list, func(m MarketSkill) bool { return m.ID == "big/r/b3" })
	if list[i].Installs != 0 || list[j].Installs != 100000-3 {
		t.Fatalf("installs: trending %d, front page's %d", list[i].Installs, list[j].Installs)
	}

	// a search: the matches among them, all of them, and skills.sh asked for 200
	list, _ = MarketSkills("big")
	if len(list) != 100 || limit != "200" {
		t.Fatalf("search: %d, limit %q", len(list), limit)
	}

	// the trending page failing leaves the front page's
	popular.Lock()
	popular.list = nil
	popular.Unlock()
	trending = nil
	srv.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" {
			w.Write(page(front))
			return
		}
		http.Error(w, "", http.StatusBadGateway)
	})
	if list, err := fetchPopular(); err != nil || len(list) != 150 {
		t.Fatalf("no trending: %d %v", len(list), err)
	}
}

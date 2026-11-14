package library

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
)

// The owner: the MCP market had too little in it. Before anything is
// searched it offers the featured servers and then the registry's most
// starred (a snapshot shipped with uni-switch, the registry not asked); one a
// featured server already is, two that start the same package, and one
// uni-switch can't fill in are offered once or not at all. A word search gets
// the registry's ranked results first, then the popular ones whose words
// have it, and up to 100 of the registry's, not 30.
func TestMarketServersPopular(t *testing.T) {
	sandbox(t)
	i := slices.IndexFunc(featured, func(m MarketServer) bool { return m.server.Remote() })
	if i < 0 {
		t.Fatal("no featured remote")
	}
	entries := []string{
		`{"name":"io.github.big/one","title":"Big One","description":"Reads files on disk.","icons":[{"src":"https://big.dev/i.png"}],"packages":[{"registryType":"npm","identifier":"@big/one"}]}`,
		`{"name":"io.github.big/again","description":"The same package.","packages":[{"registryType":"npm","identifier":"@big/one"}]}`,
		`{"name":"io.github.feat/copy","description":"A featured one.","remotes":[{"type":"streamable-http","url":"` + featured[i].server.URL + `"}]}`,
		`{"name":"io.github.x/named","description":"Needs a named argument.","packages":[{"registryType":"npm","identifier":"x-named","packageArguments":[{"type":"named","name":"--root","isRequired":true}]}]}`,
	}
	for i := range 130 {
		entries = append(entries, fmt.Sprintf(`{"name":"io.github.o%d/m%d","description":"Server %d.","packages":[{"registryType":"npm","identifier":"m%d-pkg"}]}`, i, i, i, i))
	}
	was := popularServers
	popularServers = func() []MarketServer { return readPopular([]byte("[" + strings.Join(entries, ",") + "]")) }
	t.Cleanup(func() { popularServers = was })

	var asked atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		asked.Add(1)
		var list []string
		switch r.URL.Query().Get("search") {
		case "files":
			list = append(list, `{"server":{"name":"io.github.reg/files","title":"files","remotes":[{"type":"streamable-http","url":"https://reg.dev/mcp"}]}}`)
		case "many":
			for i := range 150 {
				list = append(list, fmt.Sprintf(`{"server":{"name":"io.github.r%d/many%d","remotes":[{"type":"streamable-http","url":"https://r%d.dev/mcp"}]}}`, i, i, i))
			}
		}
		w.Write([]byte(`{"servers":[` + strings.Join(list, ",") + `]}`))
	}))
	defer srv.Close()
	wasURL := registryURL
	registryURL = srv.URL
	t.Cleanup(func() { registryURL = wasURL })

	ids := func(list []MarketServer) []string {
		var out []string
		for _, m := range list {
			out = append(out, m.ID)
		}
		return out
	}

	all, err := MarketServers("")
	if err != nil {
		t.Fatal(err)
	}
	got := ids(all)
	if len(got) != len(featured)+131 || asked.Load() != 0 {
		t.Fatalf("%d offered (want %d), registry asked %d times", len(got), len(featured)+131, asked.Load())
	}
	for i, m := range featured {
		if got[i] != m.ID {
			t.Fatalf("%d: %s, want featured %s", i, got[i], m.ID)
		}
	}
	if got[len(featured)] != "io.github.big/one" || got[len(featured)+1] != "io.github.o0/m0" || got[len(got)-1] != "io.github.o129/m129" {
		t.Fatalf("popular order: %v", got[len(featured):])
	}
	for _, id := range []string{"io.github.big/again", "io.github.feat/copy", "io.github.x/named"} {
		if slices.Contains(got, id) {
			t.Fatalf("%s offered: %v", id, got)
		}
	}
	// nor kept to be installed by its id
	if _, ok := marketServer("io.github.feat/copy"); ok {
		t.Fatal("a featured one's copy is kept")
	}
	// a popular one is installed by its id, and its icon is fetched
	if m, ok := marketServer("io.github.o5/m5"); !ok || m.Runs != "npx" {
		t.Fatalf("by id: %+v %v", m, ok)
	}
	if !offered("https://big.dev/i.png") {
		t.Fatal("popular icon not offered")
	}

	found, _ := MarketServers("files")
	got = ids(found)
	r, p := slices.Index(got, "io.github.reg/files"), slices.Index(got, "io.github.big/one")
	if r < 0 || p < 0 || r > p || slices.Contains(got, "io.github.o0/m0") {
		t.Fatalf("files: %v", got)
	}

	if found, _ := MarketServers("many"); len(found) != 100 {
		t.Fatalf("many: %d offered, want 100", len(found))
	}
}

// What uni-switch ships: hundreds of the registry's servers, each one uni-switch
// can add, none a Smithery wrapper, and none twice.
func TestPopularServersSnapshot(t *testing.T) {
	list := readPopular(serversSnapshot)
	t.Logf("%d popular servers", len(list))
	if len(list) < 300 {
		t.Fatalf("%d popular servers", len(list))
	}
	seen := map[string]bool{}
	for _, m := range list {
		if strings.HasPrefix(m.ID, "ai.smithery/") || m.Description == "" || seen[m.ID] {
			t.Fatalf("shipped: %+v", m)
		}
		seen[m.ID] = true
	}
}

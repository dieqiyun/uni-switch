package library

import (
	"net/http"
	"net/http/httptest"
	"os"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
)

const askechoDir = "server/mcp_server_askecho_search_infinity"

// fakeSources stands in for raw.githubusercontent.com, PyPI and the MCP
// Registry, which lists none of what's asked; they hand out volcengine's
// real pyproject.toml, README.md and PyPI entry for the askecho server,
// kept in testdata as fetched on 2026-10-30. It counts the registry's
// searches.
func fakeSources(t *testing.T) *atomic.Int64 {
	t.Helper()
	file := func(n string) []byte {
		b, err := os.ReadFile("testdata/askecho/" + n)
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	var searches atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/registry":
			searches.Add(1)
			w.Write([]byte(`{"servers":[]}`))
		case "/raw/volcengine/mcp-server/HEAD/" + askechoDir + "/pyproject.toml", "/raw/volcengine/mcp-server/main/" + askechoDir + "/pyproject.toml":
			w.Write(file("pyproject.toml"))
		case "/raw/volcengine/mcp-server/HEAD/" + askechoDir + "/README.md", "/raw/volcengine/mcp-server/main/" + askechoDir + "/README.md":
			w.Write(file("README.md"))
		case "/pypi/mcp-server-askecho-search-infinity/json":
			w.Write(file("pypi.json"))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	wasRaw, wasReg, wasPyPI := rawURL, registryURL, pypiURL
	rawURL = func(repo, ref, path string) string {
		if ref == "" {
			ref = "HEAD"
		}
		return srv.URL + "/raw/" + repo + "/" + ref + "/" + path
	}
	registryURL, pypiURL = srv.URL+"/registry", srv.URL+"/pypi"
	t.Cleanup(func() { rawURL, registryURL, pypiURL = wasRaw, wasReg, wasPyPI })
	return &searches
}

func inputKeys(m MarketServer) (keys []string) {
	for _, in := range m.Inputs {
		keys = append(keys, in.Key)
	}
	return
}

var askechoEnv = []string{"VOLCENGINE_ACCESS_KEY", "VOLCENGINE_SECRET_KEY", "ASK_ECHO_SEARCH_INFINITY_API_KEY"}

// CyberCaius on Discord pasted
// https://github.com/volcengine/mcp-server#subdirectory=server/mcp_server_askecho_search_infinity
// into the Library's MCP search: the registry lists no server there, so
// the page offered only a blank command named "server". The folder's
// pyproject.toml names the program and its README the keys it reads, so
// the server is offered as uv runs it from GitHub, with the README's
// three keys to fill in (an API key, or an access key and secret: none
// of them required alone), and once installed reaches Claude Desktop.
func TestMarketServerInRepoFolder(t *testing.T) {
	h := sandbox(t)
	p, _ := desktopFiles(t, h)
	write(t, p, `{"mcpServers": {}}`)
	searches := fakeSources(t)

	q := "https://github.com/volcengine/mcp-server#subdirectory=" + askechoDir
	list, err := MarketServers(q)
	if err != nil || len(list) == 0 {
		t.Fatalf("%s: %v %v", q, list, err)
	}
	m := list[0]
	wantArgs := []string{"--from", "git+https://github.com/volcengine/mcp-server#subdirectory=" + askechoDir, "mcp-server-askecho-search-infinity"}
	if !m.Match || m.server.Command != "uvx" || !slices.Equal(m.server.Args, wantArgs) || m.Name != "askecho-search-infinity" || m.Runs != "uvx" {
		t.Fatalf("offered %+v (%v %v)", m, m.server.Command, m.server.Args)
	}
	if !slices.Equal(inputKeys(m), askechoEnv) {
		t.Fatalf("inputs %v", inputKeys(m))
	}
	for _, in := range m.Inputs {
		if in.Where != "env" || !in.Secret || in.Required {
			t.Errorf("input %+v", in)
		}
	}
	if d := m.Inputs[2].Description; !strings.Contains(d, "二选一") {
		t.Errorf("the API key's description: %q", d)
	}
	if searches.Load() != 0 {
		t.Errorf("the registry was searched %d times for a folder it can't list", searches.Load())
	}
	if c := CustomAt(q, list); c != nil {
		t.Errorf("a blank server offered beside it: %+v", c)
	}

	// installed with only the API key, it goes to Claude Desktop as uv
	// runs it, with that key alone in its environment
	ok(t)(InstallServer(m.ID, map[string]string{"ASK_ECHO_SEARCH_INFINITY_API_KEY": "k-1"}, []string{"claude-desktop"}))
	got := desktopServers(t, p)[m.Name]
	if got["command"] != "uvx" || !slices.Equal(strList(got["args"]), wantArgs) {
		t.Fatalf("Claude Desktop's entry: %v", got)
	}
	if env, _ := got["env"].(map[string]any); len(env) != 1 || env["ASK_ECHO_SEARCH_INFINITY_API_KEY"] != "k-1" {
		t.Errorf("Claude Desktop's env: %v", got["env"])
	}
	// and the search says the library has it
	if list, _ = MarketServers(q); len(list) == 0 || list[0].Have != m.Name {
		t.Errorf("searched again: %+v", list)
	}
}

// The folder's page on GitHub (…/tree/main/server/…) and a file in it
// are the same folder, at that branch.
func TestMarketServerRepoFolderPage(t *testing.T) {
	sandbox(t)
	fakeSources(t)
	for _, q := range []string{
		"https://github.com/volcengine/mcp-server/tree/main/" + askechoDir,
		"github.com/volcengine/mcp-server/blob/main/" + askechoDir + "/README.md",
	} {
		list, _ := MarketServers(q)
		if len(list) == 0 || list[0].server.Args[1] != "git+https://github.com/volcengine/mcp-server@main#subdirectory="+askechoDir {
			t.Errorf("%s: %+v", q, list)
		}
	}
}

// Searched for by its PyPI name, which the registry doesn't list, it is
// offered as uvx runs the package, with the keys its description gives.
func TestMarketServerOnPyPI(t *testing.T) {
	sandbox(t)
	fakeSources(t)
	for _, q := range []string{"mcp-server-askecho-search-infinity", "https://pypi.org/project/mcp-server-askecho-search-infinity/"} {
		list, _ := MarketServers(q)
		if len(list) == 0 || list[0].server.Command != "uvx" || !slices.Equal(list[0].server.Args, []string{"mcp-server-askecho-search-infinity"}) ||
			!slices.Equal(inputKeys(list[0]), askechoEnv) {
			t.Fatalf("%s: %+v", q, list)
		}
		if c := CustomAt(q, list); strings.HasPrefix(q, "https://") && c != nil {
			t.Errorf("%s: a blank server offered beside it: %+v", q, c)
		}
	}
}

func TestParseRepoAt(t *testing.T) {
	for q, want := range map[string]repoAt{
		"https://github.com/volcengine/mcp-server#subdirectory=server/x": {Repo: "volcengine/mcp-server", Path: "server/x"},
		"git+https://github.com/o/R.git@v1#egg=x&subdirectory=a/b/":      {Repo: "o/R", Ref: "v1", Path: "a/b"},
		"github.com/o/r/tree/dev/src/Srv":                                {Repo: "o/r", Ref: "dev", Path: "src/Srv"},
		"git@github.com:o/r.git":                                         {Repo: "o/r"},
		"https://www.github.com/o/r/":                                    {Repo: "o/r"},
	} {
		if got, ok := parseRepoAt(q); !ok || got != want {
			t.Errorf("%s: %+v %v", q, got, ok)
		}
	}
	for _, q := range []string{"https://gitlab.com/o/r", "github.com/o", "o/r", "https://github.com/o/r#subdirectory=../x"} {
		if got, ok := parseRepoAt(q); ok {
			t.Errorf("%s read as %+v", q, got)
		}
	}
}

// One key a README gives is required; a placeholder value is no hint.
func TestEnvInputsOne(t *testing.T) {
	ins := envInputs([]byte("Set `FOO_TOKEN` first.\n```json\n{\"mcpServers\":{\"x\":{\"command\":\"uvx\",\"env\":{\"FOO_TOKEN\":\"<your token>\",\"lower\":\"x\"}}}}\n```\n"))
	if len(ins) != 1 || !ins[0].Required || !ins[0].Secret || ins[0].Placeholder != "" || ins[0].Description != "Set `FOO_TOKEN` first." {
		t.Fatalf("%+v", ins)
	}
}

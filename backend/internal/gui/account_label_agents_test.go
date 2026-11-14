package gui

import (
	"path/filepath"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/agent"
	"github.com/dieqiyun/uni-switch/backend/internal/filememo"
	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// Each account's row on the Providers page is named for its agent, and
// agent.Find built every agent uni-switch knows (about a hundred, each with
// its files looked at) again for each row: with the owner's config 58 MB
// of the 1.4 GB a uni-switch allocated over 20 window polls. A request builds
// them once for all its rows.
func TestAccountLabelsBuildTheAgentsOnce(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
	codex, err := agent.Find("codex")
	if err != nil {
		t.Fatal(err)
	}
	p := provider.Provider{ID: "codex-a", Name: "Codex A", Account: &provider.Account{Agent: "codex", User: "a"}}
	if name, icon := accountLabel(p); name != codex.Name || icon != codex.Icon {
		t.Fatalf("not held: %q %q, want %q %q", name, icon, codex.Name, codex.Icon)
	}
	all := testing.AllocsPerRun(3, func() { agent.All() })

	// as a GET /api/* is served (held)
	defer provider.Hold()()
	defer filememo.Hold()()
	accountLabel(p) // the request's first row builds them
	row := testing.AllocsPerRun(50, func() {
		if name, icon := accountLabel(p); name != codex.Name || icon != codex.Icon {
			t.Errorf("held: %q %q, want %q %q", name, icon, codex.Name, codex.Icon)
		}
	})
	t.Logf("a row: %.0f allocations; building the agents: %.0f", row, all)
	if row > all/20 {
		t.Errorf("a row took %.0f allocations against %.0f to build every agent: the agents were built for it", row, all)
	}
}

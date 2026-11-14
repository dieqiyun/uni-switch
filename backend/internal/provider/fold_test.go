package provider

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func foldHome(t *testing.T) string {
	t.Helper()
	isolate(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("APPDATA", filepath.Join(home, "AppData", "Roaming"))
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(home, ".local", "share"))
	return home
}

// nvidiaCopy is one of yuluo_yx's "NVIDIA #NNN": NVIDIA's preset with a
// key of its own, saved a provider each.
func nvidiaCopy(id, name, key string) Provider {
	return Provider{ID: id, Name: name, Key: key, Preset: "nvidia", Icon: "nvidia-color", Catalog: "nvidia",
		Chat: "https://integrate.api.nvidia.com/v1", Models: []string{"z-ai/glm-5.3", "deepseek-ai/deepseek-v4"}}
}

func saveAll(t *testing.T, ps ...Provider) {
	t.Helper()
	for _, p := range ps {
		if err := Save(p); err != nil {
			t.Fatal(err)
		}
	}
}

// The first that is on takes the others in: off, the first is a key that
// is off, and the provider stays on.
func TestFoldCopiesIntoOneOn(t *testing.T) {
	foldHome(t)
	one := nvidiaCopy("nvidia", "NVIDIA #001", "nvapi-1")
	one.Off = true
	saveAll(t, one, nvidiaCopy("nvidia-2", "NVIDIA #002", "nvapi-2"))
	if n, err := FoldCopies(); n != 1 || err != nil {
		t.Fatalf("%d %v", n, err)
	}
	p, err := Find("nvidia-2")
	if err != nil || p.Off || p.Name != "NVIDIA" || !slices.Equal(p.Keys, []KeyAccount{{Name: "NVIDIA #001", Key: "nvapi-1", Off: true}}) {
		t.Fatalf("%+v %v", p, err)
	}
	if q, _ := Find("nvidia"); q.ID != "nvidia-2" {
		t.Fatalf("old id reaches %q", q.ID)
	}
}

// A providers.json that can't be read is never written over.
func TestFoldCopiesUnreadable(t *testing.T) {
	foldHome(t)
	saveAll(t, nvidiaCopy("nvidia", "NVIDIA #001", "nvapi-1"), nvidiaCopy("nvidia-2", "NVIDIA #002", "nvapi-2"))
	b, _ := os.ReadFile(Path())
	bad := append(slices.Clone(b[:len(b)/2]), '}')
	if err := os.WriteFile(Path(), bad, 0o600); err != nil {
		t.Fatal(err)
	}
	if n, err := FoldCopies(); n != 0 || err == nil {
		t.Fatalf("%d %v", n, err)
	}
	if got, _ := os.ReadFile(Path()); string(got) != string(bad) {
		t.Fatal("written over")
	}
}

// An app that keeps an entry a key (CC Switch) brings in one provider
// with those keys, whatever the entries are called.
func TestImportFoldsAKeyAnEntry(t *testing.T) {
	home := foldHome(t)
	sqliteFixture(t, filepath.Join(home, ".cc-switch", "cc-switch.db"),
		`CREATE TABLE providers (id TEXT, app_type TEXT, name TEXT, settings_config TEXT, website_url TEXT, category TEXT, created_at INTEGER, sort_index INTEGER, PRIMARY KEY (id, app_type))`,
		`INSERT INTO providers VALUES ('a','claude','Relay #1','{"env":{"ANTHROPIC_BASE_URL":"https://relay.example.com","ANTHROPIC_AUTH_TOKEN":"sk-a","ANTHROPIC_MODEL":"claude-sonnet-5"}}','','custom',1,0)`,
		`INSERT INTO providers VALUES ('b','claude','Relay #2','{"env":{"ANTHROPIC_BASE_URL":"https://relay.example.com","ANTHROPIC_AUTH_TOKEN":"sk-b","ANTHROPIC_MODEL":"claude-sonnet-5"}}','','custom',2,1)`,
		`INSERT INTO providers VALUES ('c','claude','spare','{"env":{"ANTHROPIC_BASE_URL":"https://relay.example.com","ANTHROPIC_AUTH_TOKEN":"sk-c","ANTHROPIC_MODEL":"claude-sonnet-5"}}','','custom',3,2)`,
		`INSERT INTO providers VALUES ('d','claude','Other','{"env":{"ANTHROPIC_BASE_URL":"https://other.example.com","ANTHROPIC_AUTH_TOKEN":"sk-d","ANTHROPIC_MODEL":"claude-sonnet-5"}}','','custom',4,3)`,
	)
	var picks []AppPick
	for _, r := range []string{"claude/a", "claude/b", "claude/c", "claude/d"} {
		picks = append(picks, AppPick{Source: "cc-switch", Ref: r})
	}
	if added, err := ImportFromApps(picks); err != nil || len(added) != 4 {
		t.Fatalf("%v %v", added, err)
	}
	ps := load().Providers
	if len(ps) != 2 {
		t.Fatalf("%d providers: %+v", len(ps), ps)
	}
	p := ps[0]
	if p.Key != "sk-a" || len(p.Keys) != 2 || p.Keys[0].Key != "sk-b" || p.Keys[0].Name != "Relay #2" || p.Keys[1].Key != "sk-c" || p.Keys[1].Name != "spare" {
		t.Fatalf("%+v", p)
	}
	if ps[1].Key != "sk-d" || len(ps[1].Keys) != 0 {
		t.Fatalf("other %+v", ps[1])
	}
}

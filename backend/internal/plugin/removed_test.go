package plugin

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/appdir"
	"github.com/dieqiyun/uni-switch/backend/internal/testenv"
)

func TestMain(m *testing.M) { testenv.Run(m) }

func TestUniSwitchRemovedExtensionsIgnoreSavedPackages(t *testing.T) {
	home := t.TempDir()
	if err := appdir.UseUniSwitchDirectory(home); err != nil {
		t.Fatal(err)
	}
	files := map[string]string{
		"plugins.json":                    `{"plugins":[{"spec":"./extensions/fixture"}]}`,
		"plugin-auth.json":                `{"fixture":{"key":"synthetic-removed-key"}}`,
		"extensions/fixture/package.json": `{"name":"fixture","uni-switch":{"gateway":"index.js","agent":"index.js"}}`,
		"extensions/fixture/index.js":     `throw new Error("legacy package must never execute");`,
	}
	for name, data := range files {
		p := filepath.Join(home, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(data), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	ctx := context.Background()
	if len(Load().Plugins) != 0 || len(Cached()) != 0 || Running() || HasBun() || HasGateway() {
		t.Fatal("a removed extension was discovered or started")
	}
	if ps, err := Providers(ctx); err != nil || len(ps) != 0 {
		t.Fatalf("providers = %v, %v", ps, err)
	}
	if ts, err := Tools(ctx); err != nil || len(ts) != 0 {
		t.Fatalf("tools = %v, %v", ts, err)
	}
	_, install := Add(ctx, "./extensions/fixture")
	_, fetch := Fetch(ctx, FetchRequest{Provider: "fixture", URL: "http://127.0.0.1:9"})
	_, auth := Authorize(ctx, "fixture", 0, nil, NewAccount)
	_, tool := RunTool(ctx, "fixture", "run", nil, "synthetic-client")
	for _, err := range []error{install, fetch, auth, tool} {
		if !errors.Is(err, ErrUnavailable) {
			t.Fatalf("extension operation did not fail closed: %v", err)
		}
	}
	for name, want := range files {
		got, err := os.ReadFile(filepath.Join(home, name))
		if err != nil || string(got) != want {
			t.Fatalf("legacy data was modified: %s", name)
		}
	}
}

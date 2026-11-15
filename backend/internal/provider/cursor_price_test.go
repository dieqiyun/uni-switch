package provider

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/catalog"
)

// windowCatalog is a catalog with a Claude model priced past 200K as
// models.dev writes it, a GPT model, and a model whose id has an @ in it of
// its own.
const windowCatalog = `{
  "anthropic":{"id":"anthropic","models":{
    "claude-opus-5-5":{"id":"claude-opus-5-5","cost":{"input":4,"output":20,"cache_read":0.2,"cache_write":5,
      "tiers":[{"tier":{"type":"context","size":200000},"input":8,"output":30}]}},
    "claude-opus-4-5@20251101":{"id":"claude-opus-4-5@20251101","cost":{"input":5,"output":25}}}},
  "openai":{"id":"openai","models":{
    "gpt-5.5":{"id":"gpt-5.5","cost":{"input":1.25,"output":10,"cache_read":0.125}}}}}`

func windowHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
	os.MkdirAll(filepath.Dir(catalog.CachePath()), 0o755)
	os.WriteFile(catalog.CachePath(), []byte(windowCatalog), 0o644)
	catalog.Reset()
	t.Cleanup(catalog.Reset)
}

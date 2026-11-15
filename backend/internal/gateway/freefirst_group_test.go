package gateway

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/catalog"
)

// liveModels writes the model list a provider's vendor would have answered,
// where catalog.Live reads it: the truth provider.Available goes by, and so
// what FreeModel judges a model free from.
func liveModels(t *testing.T, id, json string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(catalog.LivePath(id)), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(catalog.LivePath(id), []byte(json), 0o644); err != nil {
		t.Fatal(err)
	}
	catalog.Reset()
}

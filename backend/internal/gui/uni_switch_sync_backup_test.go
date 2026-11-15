package gui

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/settings"
)

func TestUniSwitchSyncBackupEndpointsRemoved(t *testing.T) {
	h := builtinHome(t)
	dir := settings.Dir()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	// Retired settings and existing backups are inert and remain the user's files.
	files := map[string][]byte{
		"sync.json":                    []byte(`{"url":"http://127.0.0.1:1/dav","passphrase":"synthetic-only"}`),
		"sync-state.json":              []byte(`{"last":"2020-01-01T00:00:00Z"}`),
		"autobackup.json":              []byte(`{"hours":1,"keep":3}`),
		"historical.uni-switch-backup": []byte("synthetic historical backup"),
	}
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	for _, path := range []string{
		"/api/davsync", "/api/davsync/save", "/api/davsync/now", "/api/davsync/auto",
		"/api/davsync/restore", "/api/davsync/undo", "/api/davsync/upload", "/api/davsync/off",
		"/api/davsync/follow", "/api/davsync/reveal", "/api/davsync/dismiss",
		"/api/backup/export", "/api/backup/import", "/api/autobackup",
		"/api/autobackup/set", "/api/autobackup/now", "/api/autobackup/restore",
		"/api/autobackup/choose", "/api/autobackup/reveal", "/api/agents/here",
		"/syncfiles.js", "/autobackup.js",
	} {
		for _, method := range []string{"GET", "POST"} {
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, httptest.NewRequest(method, path, strings.NewReader(`{}`)))
			if rec.Code != http.StatusNotFound {
				t.Errorf("%s %s = %d, want 404", method, path, rec.Code)
			}
		}
	}
	for name, want := range files {
		got, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil || !bytes.Equal(got, want) {
			t.Errorf("legacy file %s was changed: %v", name, err)
		}
	}
}

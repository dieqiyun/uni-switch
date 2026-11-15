package agent

import (
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCindyLink(t *testing.T) {
	link := CindyLink("http://127.0.0.1:3425")
	data, ok := strings.CutPrefix(link, CindyScheme+"v=1&data=")
	if !ok {
		t.Fatalf("link = %s", link)
	}
	b, err := base64.RawURLEncoding.DecodeString(data)
	if err != nil {
		t.Fatal(err)
	}
	var p struct {
		Kind, Name, ID string
		Auth           struct{ Method, APIKey string }
		Endpoints      []struct {
			Protocol, BaseURL string
			Targets           []string
		}
	}
	if err := json.Unmarshal(b, &p); err != nil {
		t.Fatal(err)
	}
	if p.Kind != "custom" || p.ID != "uni-switch" || p.Auth.Method != "apiKey" || p.Auth.APIKey != "uni-switch-cindy" || len(p.Endpoints) != 3 {
		t.Fatalf("payload = %+v", p)
	}
	if p.Endpoints[0].BaseURL != "http://127.0.0.1:3425" || p.Endpoints[1].BaseURL != "http://127.0.0.1:3425/v1" {
		t.Fatalf("endpoints = %+v", p.Endpoints)
	}
}

func TestCindyHas(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cindy-local-v1.db")
	gw := "http://127.0.0.1:3425"
	if has, known := cindyHas(path, gw); has || !known {
		t.Fatal("no database, yet has uni-switch or unknown")
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`CREATE TABLE custom_providers (id text PRIMARY KEY, name text, runtimes text DEFAULT '{}')`); err != nil {
		t.Fatal(err)
	}
	if has, known := cindyHas(path, gw); has || !known {
		t.Fatal("empty, yet has uni-switch or unknown")
	}
	if _, err := db.Exec(`INSERT INTO custom_providers VALUES ('p1', 'Other', '{"codex":{"baseUrl":"https://x"}}')`); err != nil {
		t.Fatal(err)
	}
	if has, _ := cindyHas(path, gw); has {
		t.Fatal("another provider taken for uni-switch")
	}
	if _, err := db.Exec(`INSERT INTO custom_providers VALUES ('p2', 'Mine', '{"codex":{"baseUrl":"http://127.0.0.1:3425/v1"}}')`); err != nil {
		t.Fatal(err)
	}
	if has, known := cindyHas(path, gw); !has || !known {
		t.Fatal("the gateway's provider not found")
	}
}

// Cindy's global build keeps a database per signed-in account in
// CindyGlobal: uni-switch added there is added.
func TestCindyAdded(t *testing.T) {
	local, global := t.TempDir(), t.TempDir()
	gw := "http://127.0.0.1:3425"
	if added, known := cindyAdded([]string{local, global}, gw); added || !known {
		t.Fatal("no databases, yet added or unknown")
	}
	db, err := sql.Open("sqlite", filepath.Join(global, "cindy-cmubb4059023mzr0152yezm4r.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`CREATE TABLE custom_providers (id text PRIMARY KEY, name text, runtimes text DEFAULT '{}');
		INSERT INTO custom_providers VALUES ('uni-switch-73271b6d', 'uni-switch', '{}')`); err != nil {
		t.Fatal(err)
	}
	if added, _ := cindyAdded([]string{local, global}, gw); !added {
		t.Fatal("uni-switch in an account's database not found")
	}
}

// Cindy closed leaves its WAL-mode database with no -shm beside it, which
// SQLite can't open read-only: uni-switch in it is still found, and nothing is
// written in Cindy's folder. A file that is no database is unknown, never
// "not added".
func TestCindyHasClosedWALAndUnreadable(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cindy-local-v1.db")
	gw := "http://127.0.0.1:3425"
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`PRAGMA journal_mode=WAL;
		CREATE TABLE custom_providers (id text PRIMARY KEY, name text, runtimes text DEFAULT '{}');
		INSERT INTO custom_providers VALUES ('uni-switch', 'uni-switch', '{}')`); err != nil {
		t.Fatal(err)
	}
	db.Close()
	before := dirNames(t, dir)
	if has, known := cindyHas(path, gw); !has || !known {
		t.Fatalf("closed WAL database: has %v known %v", has, known)
	}
	if after := dirNames(t, dir); after != before {
		t.Fatalf("Cindy's folder changed: %s -> %s", before, after)
	}
	bad := filepath.Join(dir, "cindy-bad.db")
	if err := os.WriteFile(bad, []byte("not a database, a half-written one"), 0600); err != nil {
		t.Fatal(err)
	}
	if has, known := cindyHas(bad, gw); has || known {
		t.Fatalf("unreadable database: has %v known %v, want unknown", has, known)
	}
	if added, known := cindyAdded([]string{dir}, gw); !added || !known {
		t.Fatal("uni-switch in one database, beside one unreadable, not found")
	}
	os.Remove(path)
	if added, known := cindyAdded([]string{dir}, gw); added || known {
		t.Fatalf("only an unreadable database: added %v known %v, want unknown", added, known)
	}
}

func dirNames(t *testing.T, dir string) string {
	t.Helper()
	es, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range es {
		names = append(names, e.Name())
	}
	return strings.Join(names, ",")
}

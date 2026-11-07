package configuration

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/agent"
	"github.com/dieqiyun/uni-switch/backend/internal/edit"
	"github.com/dieqiyun/uni-switch/backend/internal/localgateway"
)

func extraFixture(t *testing.T, client string) (*Service, agent.Provider, string) {
	t.Helper()
	s, p := fixture(t)
	dir := filepath.Join(s.Directory, "clients", client)
	if e := s.SetDirectory(client, dir, "", false); e != nil {
		t.Fatal(e)
	}
	s.Route = &Route{Port: 12345, Token: strings.Repeat("a", 64)}
	return s, p, dir
}

func TestNewMaxUpdatesOnlyAppRowAndRestoresExactValue(t *testing.T) {
	s, p, dir := extraFixture(t, "newmax")
	if e := os.MkdirAll(dir, 0700); e != nil {
		t.Fatal(e)
	}
	path := filepath.Join(dir, "newmax.db")
	db, e := sql.Open("sqlite", path)
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	original := `{ "providers":[{"id":"personal","apiKey":"synthetic-personal","name":"keep"}],"providerOrder":["personal"],"font":14 }`
	if _, e = db.Exec("CREATE TABLE settings(key TEXT PRIMARY KEY,value TEXT);INSERT INTO settings VALUES('app',?);INSERT INTO settings VALUES('untouched','keep')", original); e != nil {
		t.Fatal(e)
	}
	if _, e = s.ApplyClient("newmax", p.ID(), "messages", nil); e != nil {
		t.Fatal(e)
	}
	got, e := readClientResource(path, "newmax")
	if e != nil || !strings.Contains(*got, `"apiKey":"synthetic-personal"`) {
		t.Fatal(got, e)
	}
	var data Object
	_ = parse(*got, &data)
	providers := arr(data["providers"])
	if len(providers) != 2 || obj(providers[1])["id"] != "uni-switch-gateway" || obj(providers[1])["apiFormat"] != "anthropic" {
		t.Fatal(*got)
	}
	if _, e = s.ApplyClient("newmax", p.ID(), "messages", nil); e != nil {
		t.Fatal(e)
	}
	if _, e = s.RestoreClient("newmax"); e != nil {
		t.Fatal(e)
	}
	got, e = readClientResource(path, "newmax")
	if e != nil || got == nil || *got != original {
		t.Fatal("row was not restored exactly", got, e)
	}
	var untouched string
	if e = db.QueryRow("SELECT value FROM settings WHERE key='untouched'").Scan(&untouched); e != nil || untouched != "keep" {
		t.Fatal("other row changed")
	}
	// A DB transaction performs its own compare-and-swap, beyond the journal.
	if e = writeClientResource(path, "newmax", ptr("stale"), ptr(`{}`)); code(e) != "config_conflict" {
		t.Fatal("accepted stale database row", e)
	}
}

func TestHanakoPreservesOtherProvidersAndAgentFields(t *testing.T) {
	s, p, dir := extraFixture(t, "hanako")
	catalog := filepath.Join(dir, "provider-catalog.json")
	chat := filepath.Join(dir, "agents", "primary", "config.yaml")
	original := `{"catalogVersion":2,"providers":{"personal":{"api_key":"synthetic-personal"}}}`
	_ = edit.WriteAtomic(catalog, &original)
	_ = edit.WriteAtomic(filepath.Join(dir, "user", "preferences.json"), ptr(`{"primaryAgent":"primary"}`))
	_ = edit.WriteAtomic(chat, ptr("# 保留\nmodels:\n  chat:\n    provider: personal\n    id: keep\nworkspace: keep\n"))
	if _, e := s.ApplyClient("hanako", p.ID(), "chat_completions", nil); e != nil {
		t.Fatal(e)
	}
	if _, e := s.RestoreClient("hanako"); e != nil {
		t.Fatal(e)
	}
	got, _ := edit.Read(catalog)
	var restored Object
	if got == nil || parse(*got, &restored) != nil || agent.Get(restored, "providers.personal.api_key") != "synthetic-personal" || agent.Get(restored, "providers.uni-switch") != nil {
		t.Fatal(*got)
	}
	got, _ = edit.Read(chat)
	if !strings.Contains(*got, "workspace: keep") || !strings.Contains(*got, "provider: personal") || !strings.Contains(*got, "# 保留") {
		t.Fatal(*got)
	}
}

func TestZedCredentialsUseIsolatedStoreAndRollbackWithFiles(t *testing.T) {
	s, p, dir := extraFixture(t, "zed")
	keys := map[string]string{}
	read, write := credentialRead, credentialWrite
	t.Cleanup(func() { credentialRead = read; credentialWrite = write; localgateway.CloseAll(context.Background()) })
	credentialRead = func(url string) (*string, error) {
		if v, ok := keys[url]; ok {
			return &v, nil
		}
		return nil, nil
	}
	credentialWrite = func(url string, v *string) error {
		if v == nil {
			delete(keys, url)
		} else {
			keys[url] = *v
		}
		return nil
	}
	path := filepath.Join(dir, "settings.json")
	original := "{\n// personal\n\"editor\":{\"font_size\":16}\n}\n"
	_ = edit.WriteAtomic(path, &original)
	if _, e := s.ApplyClient("zed", p.ID(), "chat_completions", nil); e != nil {
		t.Fatal(e)
	}
	if len(keys) != 1 {
		t.Fatal("credential was not stored")
	}
	if _, e := s.RestoreClient("zed"); e != nil {
		t.Fatal(e)
	}
	got, _ := edit.Read(path)
	if got == nil || *got != original || len(keys) != 0 {
		t.Fatal("settings/credential restore failed")
	}
	credentialWrite = func(string, *string) error { return fail("credential_store", "synthetic failure") }
	if _, e := s.ApplyClient("zed", p.ID(), "chat_completions", nil); code(e) != "credential_store" {
		t.Fatal(e)
	}
	got, _ = edit.Read(path)
	if got == nil || *got != original {
		t.Fatal("credential failure left settings changed")
	}
}

type fakeAlma struct {
	providers  []Object
	settings   Object
	failModels bool
	encryptKey bool
}

func (a *fakeAlma) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	path := r.URL.Path
	var data Object
	if r.Method != "GET" && r.Method != "DELETE" {
		_ = json.NewDecoder(r.Body).Decode(&data)
	}
	switch {
	case path == "/api/providers" && r.Method == "GET":
		providers := agent.Clone(a.providers)
		if a.encryptKey {
			for _, p := range providers {
				if p["name"] == "uni-switch" {
					p["apiKey"] = "encrypted:synthetic"
				}
			}
		}
		_ = json.NewEncoder(w).Encode(providers)
	case path == "/api/providers" && r.Method == "POST":
		data["id"] = "server-assigned"
		a.providers = append(a.providers, data)
		_ = json.NewEncoder(w).Encode(Object{}) // exercise lookup after create
	case path == "/api/settings" && r.Method == "GET":
		_ = json.NewEncoder(w).Encode(a.settings)
	case path == "/api/settings" && r.Method == "PUT":
		a.settings = data
		w.WriteHeader(204)
	case strings.HasPrefix(path, "/api/providers/server-assigned"):
		if strings.HasSuffix(path, "/models") && a.failModels {
			a.failModels = false
			http.Error(w, "synthetic failure", 500)
			return
		}
		for i, p := range a.providers {
			if p["id"] != "server-assigned" {
				continue
			}
			if r.Method == "DELETE" {
				a.providers = append(a.providers[:i], a.providers[i+1:]...)
				break
			}
			for k, v := range data {
				p[k] = v
			}
		}
		w.WriteHeader(204)
	default:
		http.NotFound(w, r)
	}
}
func TestAlmaAPIServerAssignedIDsPreservationRestoreAndPartialFailure(t *testing.T) {
	for _, test := range []struct {
		name            string
		fail, encrypted bool
	}{{"success", false, false}, {"partial_failure", true, false}, {"encrypted", false, true}, {"encrypted_partial_failure", true, true}} {
		t.Run(test.name, func(t *testing.T) {
			a := &fakeAlma{providers: []Object{{"id": "personal", "name": "personal", "apiKey": "synthetic-personal"}}, settings: Object{"chat": Object{"defaultModel": "personal:keep"}, "theme": "keep"}, failModels: test.fail, encryptKey: test.encrypted}
			api := httptest.NewServer(a)
			defer api.Close()
			old := almaEndpoint
			almaEndpoint = api.URL
			t.Cleanup(func() { almaEndpoint = old })
			s, p, _ := extraFixture(t, "alma")
			_, e := s.ApplyClient("alma", p.ID(), "chat_completions", nil)
			if test.fail {
				if e == nil {
					t.Fatal("failure reported as applied")
				}
			} else {
				if e != nil {
					t.Fatal(e)
				}
				if agent.Get(a.settings, "chat.defaultModel") != "server-assigned:fixture-model" {
					t.Fatal(a.settings)
				}
				if _, e = s.ApplyClient("alma", p.ID(), "chat_completions", nil); e != nil {
					t.Fatal("encrypted reapply", e)
				}
				if _, e = s.RestoreClient("alma"); e != nil {
					t.Fatal(e)
				}
			}
			if len(a.providers) != 1 || a.providers[0]["id"] != "personal" || a.settings["theme"] != "keep" || agent.Get(a.settings, "chat.defaultModel") != "personal:keep" {
				t.Fatalf("did not preserve/restore API state: %+v %+v", a.providers, a.settings)
			}
		})
	}
}

func TestAlmaUnownedProviderIsNotTakenOver(t *testing.T) {
	a := &fakeAlma{providers: []Object{{"id": "server-assigned", "name": "uni-switch", "type": "openai", "apiKey": "encrypted:synthetic"}}, settings: Object{"theme": "keep"}, encryptKey: true}
	before := agent.Canonical(Object{"providers": a.providers, "settings": a.settings})
	api := httptest.NewServer(a)
	defer api.Close()
	old := almaEndpoint
	almaEndpoint = api.URL
	t.Cleanup(func() { almaEndpoint = old })
	s, p, _ := extraFixture(t, "alma")
	_, e := s.ApplyClient("alma", p.ID(), "chat_completions", nil)
	if code(e) != "config_collision" || before != agent.Canonical(Object{"providers": a.providers, "settings": a.settings}) {
		t.Fatal("unowned encrypted provider was changed", e)
	}
}

func TestHanakoLiveSealedCatalogAndRestoreModelBeforeProvider(t *testing.T) {
	for _, saved := range []string{"null", `"keep"`, `{"provider":"personal","id":"missing"}`} {
		t.Run(saved, func(t *testing.T) {
			s, p, dir := extraFixture(t, "hanako")
			catalog := filepath.Join(dir, "provider-catalog.json")
			chat := filepath.Join(dir, "agents", "primary", "config.yaml")
			plugin := filepath.Join(dir, "provider-plugins", "uni-switch", "providers", "uni-switch.json")
			cipher := "HANA-SECRET-1.AES-256-GCM.synthetic-do-not-overwrite"
			if e := edit.WriteAtomic(catalog, &cipher); e != nil {
				t.Fatal(e)
			}
			_ = edit.WriteAtomic(filepath.Join(dir, "user", "preferences.json"), ptr(`{"primaryAgent":"primary"}`))
			var oldChat any
			_ = parse(saved, &oldChat)
			writeChat := func(v any) {
				raw, _ := edit.Read(chat)
				out, e := agent.Mutate("yaml", raw, []agent.Mutation{{Path: []string{"models", "chat"}, Value: v, Remove: v == nil}})
				if e != nil {
					t.Error(e)
					return
				}
				if e = edit.WriteAtomic(chat, &out); e != nil {
					t.Error(e)
				}
			}
			writeChat(oldChat)
			calls := []string{}
			api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Authorization") != "Bearer synthetic-hanako" {
					http.Error(w, "auth", 401)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				if r.URL.Path == "/api/server/identity" {
					_ = json.NewEncoder(w).Encode(Object{"serverId": "fixture"})
					return
				}
				if r.URL.Path == "/api/models" {
					_ = json.NewEncoder(w).Encode(Object{"models": []Object{{"provider": "uni-switch", "id": "fixture-model"}, {"provider": "personal", "id": "keep"}}})
					return
				}
				var data Object
				_ = json.NewDecoder(r.Body).Decode(&data)
				if r.Method != "PUT" {
					http.NotFound(w, r)
					return
				}
				switch r.URL.Path {
				case "/api/config":
					entry := obj(agent.Get(data, "providers.uni-switch"))
					if entry == nil {
						current, _ := readHanako(chat, "hanako_chat")
						if current != nil && strings.Contains(*current, "uni-switch") {
							http.Error(w, "current model depends on provider", 409)
							return
						}
						calls = append(calls, "remove-provider")
						_ = edit.WriteAtomic(plugin, nil)
					} else {
						calls = append(calls, "add-provider")
						_ = edit.WriteAtomic(plugin, ptr(encode(Object{"displayName": entry["display_name"], "defaultBaseUrl": entry["base_url"], "defaultApi": entry["api"], "models": entry["models"]})))
					}
				case "/api/agents/primary/config":
					v := obj(agent.Get(data, "models.chat"))
					if v == nil || str(v["provider"]) == "" || str(v["id"]) == "" {
						http.Error(w, "requires provider/id", 400)
						return
					}
					calls = append(calls, "chat:"+str(v["provider"]))
					writeChat(v)
				default:
					http.NotFound(w, r)
					return
				}
				w.WriteHeader(204)
			}))
			defer api.Close()
			u, _ := url.Parse(api.URL)
			_ = edit.WriteAtomic(filepath.Join(dir, "server-info.json"), ptr(`{"port":`+u.Port()+`,"token":"synthetic-hanako"}`))
			if _, e := s.ApplyClient("hanako", p.ID(), "chat_completions", nil); e != nil {
				t.Fatal(e)
			}
			if _, e := s.ApplyClient("hanako", p.ID(), "chat_completions", nil); e != nil {
				t.Fatal("sealed reapply failed", e)
			}
			if _, e := s.RestoreClient("hanako"); e != nil {
				t.Fatal(e)
			}
			got, _ := edit.Read(catalog)
			if got == nil || *got != cipher {
				t.Fatal("sealed catalog was overwritten")
			}
			if len(calls) < 4 || calls[len(calls)-2] != "chat:personal" || calls[len(calls)-1] != "remove-provider" {
				t.Fatal(calls)
			}
			got, _ = readHanako(chat, "hanako_chat")
			if got == nil || !strings.Contains(*got, `"id":"keep"`) {
				t.Fatal(got)
			}
		})
	}
}

func TestUnavailableDiscoveryDoesNotBlockOtherClientsAndManualDirectoryWorks(t *testing.T) {
	root := t.TempDir()
	t.Setenv("DS_BOT_HOME", "")
	t.Setenv("DS_BOT_SHELL_DATA", root)
	_ = edit.WriteAtomic(filepath.Join(root, "locations.json"), ptr("broken"))
	s, p := fixture(t)
	var directory string
	if e := s.db.QueryRow("SELECT directory FROM client_configs WHERE id='dsbot'").Scan(&directory); e != nil || directory != "" {
		t.Fatal(directory, e)
	}
	if _, _, e := s.binding("dsbot"); code(e) != "invalid_config" {
		t.Fatal("unsafe fallback", e)
	}
	if e := s.SetDirectory("dsbot", filepath.Join(root, "chosen"), "", false); e != nil {
		t.Fatal(e)
	}
	if _, e := s.ApplyClient("pi", p.ID(), "chat_completions", nil); code(e) != "bridge_unavailable" {
		t.Fatal("unrelated client was blocked", e)
	}
}

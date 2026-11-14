package provider

import (
	"path/filepath"
	"testing"
)

// A company's internal Codex relay at a LAN address that takes no key
// (X, iSnow_999: http://<LAN IP>:8080, imported from CC Switch) is taken
// as a local server at 127.0.0.1 is: imported, and saved by hand, keyless.
// A keyless server out on the internet still needs its key.
func TestKeylessRelayOnOwnNetwork(t *testing.T) {
	isolate(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("APPDATA", filepath.Join(home, "AppData", "Roaming"))
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
	codex := func(base string) string {
		return `{"auth":{},"config":"model_provider = \"corp\"\nmodel = \"gpt-5.5\"\n\n[model_providers.corp]\nname = \"corp\"\nbase_url = \"` + base + `\"\nwire_api = \"responses\"\nrequires_openai_auth = false\n"}`
	}
	sqliteFixture(t, filepath.Join(home, ".cc-switch", "cc-switch.db"),
		`CREATE TABLE providers (id TEXT, app_type TEXT, name TEXT, settings_config TEXT, website_url TEXT, category TEXT, created_at INTEGER, sort_index INTEGER, PRIMARY KEY (id, app_type))`,
		`INSERT INTO providers VALUES ('lan','codex','公司中转','`+codex("http://192.168.1.10:8080/v1")+`','','custom',1,0)`,
		`INSERT INTO providers VALUES ('far','codex','Far relay','`+codex("https://relay.example.com/v1")+`','','custom',2,0)`,
	)
	items := itemsOf(t, "cc-switch")
	if it := items["codex/lan"]; it.Skip != "" || it.Provider.Responses != "http://192.168.1.10:8080/v1" || it.Provider.Key != "" {
		t.Fatalf("keyless LAN relay: %+v", it)
	}
	if it := items["codex/far"]; it.Skip != "it has no API key" {
		t.Fatalf("keyless internet relay offered: %+v", it)
	}
	if _, err := ImportFromApps([]AppPick{{Source: "cc-switch", Ref: "codex/lan"}}); err != nil {
		t.Fatal(err)
	}

	for _, base := range []string{"http://192.168.1.10:8080/v1", "http://10.0.0.5:8080/v1", "http://172.16.3.4:8080", "http://100.101.102.103:8080/v1", "http://gpu.local:8080/v1", "http://[fd00::5]:8080/v1"} {
		p := Provider{ID: "lan-relay", Name: "LAN relay", Responses: base}
		if err := Save(p); err != nil {
			t.Errorf("%s: %v", base, err)
		} else if q, _ := Find("lan-relay"); !q.Ready() {
			t.Errorf("%s: saved but not ready", base)
		}
	}
	for _, base := range []string{"https://relay.example.com/v1", "http://8.8.8.8:8080/v1"} {
		if err := Save(Provider{ID: "far-relay", Name: "Far relay", Responses: base}); err == nil {
			t.Errorf("%s saved with no key", base)
		}
	}
}

package provider

import (
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// ccSwitchSchema is CC Switch's own providers table (src-tauri/src/database/
// schema.rs), so the fixture is read as the app's database is.
const ccSwitchSchema = `CREATE TABLE IF NOT EXISTS providers (
    id TEXT NOT NULL, app_type TEXT NOT NULL, name TEXT NOT NULL, settings_config TEXT NOT NULL,
    website_url TEXT, category TEXT, created_at INTEGER, sort_index INTEGER, notes TEXT, icon TEXT, icon_color TEXT,
    meta TEXT NOT NULL DEFAULT '{}', is_current BOOLEAN NOT NULL DEFAULT 0, in_failover_queue BOOLEAN NOT NULL DEFAULT 0,
    PRIMARY KEY (id, app_type))`

// reservedHome is a home signed in to Claude, with a CC Switch whose
// third-party providers are named as uni-switch's subscriptions are: a Pi
// "workbuddy" at a local proxy and an OpenCode "Claude" at a relay uni-switch
// has under another name, as #1487's screenshots show, a Claude Code
// "claude" at a relay of its own, and a "Kiro" at another.
func reservedHome(t *testing.T) string {
	t.Helper()
	home := claudeHome(t)
	t.Setenv("APPDATA", filepath.Join(home, "AppData", "Roaming")) // Windows: never the real Alma
	t.Setenv("CLAUDE_CONFIG_DIR", filepath.Join(home, ".claude"))
	claudeSignIn(t, home, time.Now().Add(24*time.Hour))
	sqliteFixture(t, filepath.Join(home, ".cc-switch", "cc-switch.db"), ccSwitchSchema,
		`INSERT INTO providers (id,app_type,name,settings_config,website_url,category,created_at,sort_index) VALUES
		 ('wb','pi','workbuddy','{"api":"openai-completions","apiKey":"sk-5wb-3432","baseUrl":"http://127.0.0.1:7863/v1","models":[{"id":"glm-5"},{"id":"kimi-k2"}]}','','custom',1,0),
		 ('cl','opencode','Claude','{"npm":"@ai-sdk/anthropic","options":{"baseURL":"https://www.findcg.example/v1","apiKey":"sk-dcl-a39e"},"models":{"claude-sonnet-5":{"name":"Sonnet"},"claude-opus-5":{"name":"Opus"}}}','','custom',2,0),
		 ('cl2','claude','claude','{"env":{"ANTHROPIC_BASE_URL":"https://relay-1487.example.com","ANTHROPIC_AUTH_TOKEN":"sk-relay-beef","ANTHROPIC_MODEL":"claude-sonnet-5"}}','','custom',3,0),
		 ('kr','claude-desktop','Kiro','{"env":{"ANTHROPIC_BASE_URL":"https://kiro-relay.example.com","ANTHROPIC_AUTH_TOKEN":"sk-kiro-relay"}}','','custom',4,0)`,
	)
	if err := Save(Provider{ID: "findcg", Name: "findcg", Anthropic: "https://www.findcg.example/v1", Key: "sk-findcg-own"}); err != nil {
		t.Fatal(err)
	}
	return home
}

// A third-party provider named as a subscription is, whether or not that
// subscription is signed in, comes in beside it as a provider of its own,
// with its own key at its own address (#1487, Dudung1018).
func TestImportNamedAsSubscription(t *testing.T) {
	reservedHome(t)
	if _, ok := find(Accounts(), "claude"); !ok {
		t.Fatal("the fixture's Claude sign-in isn't read")
	}
	items := itemsOf(t, "cc-switch")
	wb := items["pi/wb"]
	if wb.Status != "taken" || !wb.Reserved {
		t.Fatalf("workbuddy isn't told apart from the subscription: %+v", wb)
	}
	cl := items["claude/cl2"]
	if cl.Status != "taken" || !cl.Reserved || cl.KeyOf != "" {
		t.Fatalf("claude: %+v", cl)
	}
	// what the picker turns on for them: added, never "pick another name"
	added, err := ImportFromApps([]AppPick{{Source: "cc-switch", Ref: "pi/wb", Mode: "add"}, {Source: "cc-switch", Ref: "claude/cl2", Mode: "add"}})
	if err != nil || len(added) != 2 {
		t.Fatalf("import: %v %v", added, err)
	}
	var wbp, clp *Provider
	for _, p := range load().Providers {
		switch p.Key {
		case "sk-5wb-3432":
			wbp = &p
		case "sk-relay-beef":
			clp = &p
		}
	}
	if wbp == nil || wbp.ID == "workbuddy" || wbp.Chat != "http://127.0.0.1:7863/v1" {
		t.Fatalf("workbuddy saved as %+v", wbp)
	}
	if clp == nil || clp.ID == "claude" || clp.Anthropic != "https://relay-1487.example.com" {
		t.Fatalf("claude relay saved as %+v", clp)
	}
	// the subscription is as it was
	if c, ok := find(Accounts(), "claude"); !ok || c.Account == nil || c.Key != "" {
		t.Fatalf("the Claude subscription: %+v", c)
	}
}

// A subscription is never replaced by an imported provider: Replace took
// the relay's models as the Claude subscription's picks and dropped its key
// and address, and a Kiro would have kept the relay's key, to be sent to
// Kiro's host (#1487).
func TestImportNeverReplacesASubscription(t *testing.T) {
	reservedHome(t)
	for _, ref := range []string{"claude/cl2", "claude-desktop/kr"} {
		if _, err := ImportFromApps([]AppPick{{Source: "cc-switch", Ref: ref, Mode: "replace"}}); err == nil {
			t.Errorf("%s replaced a subscription", ref)
		}
	}
	for _, p := range load().Providers {
		if p.ID == "claude" || p.ID == "kiro" || strings.Contains(p.Key, "relay") {
			t.Fatalf("written: %+v", p)
		}
	}
}

// A name that collides can be changed in the picker: the provider comes in
// under it, at its own address (#1487).
func TestImportRenamedInPicker(t *testing.T) {
	reservedHome(t)
	// renamed, a "replace" is an add: the name is the user's own now
	added, err := ImportFromApps([]AppPick{{Source: "cc-switch", Ref: "claude/cl2", Mode: "replace", Name: "My Claude Relay"}})
	if err != nil || len(added) != 1 {
		t.Fatalf("import: %v %v", added, err)
	}
	p, err := Find("my-claude-relay")
	if err != nil || p.Name != "My Claude Relay" || p.Key != "sk-relay-beef" || p.Anthropic != "https://relay-1487.example.com" {
		t.Fatalf("renamed: %+v %v", p, err)
	}
	// the OpenCode "Claude" joins findcg, at its address, as one more key;
	// the answer names where it went
	added, err = ImportFromApps([]AppPick{{Source: "cc-switch", Ref: "opencode/cl", Mode: "key"}})
	if err != nil || len(added) != 1 || !strings.Contains(added[0], "findcg") {
		t.Fatalf("as a key: %v %v", added, err)
	}
	if h, _ := Find("findcg"); len(h.Keys) != 1 || h.Keys[0].Key != "sk-dcl-a39e" {
		t.Fatalf("findcg: %+v", h)
	}
}

// An import link named as a subscription is a provider of its own too:
// never in the subscription's place (#1487).
func TestImportLinkNamedAsSubscription(t *testing.T) {
	claudeHome(t)
	for _, link := range []string{
		"uni-switch://import?name=Claude&anthropic=https://relay-1487.example.com&key=sk-x",
		"uni-switch://import?name=Relay&id=kiro&anthropic=https://relay-1487.example.com&key=sk-x",
	} {
		p, err := ParseImport(link)
		if err != nil {
			t.Fatal(err)
		}
		if subscriptionID(p.ID) {
			t.Errorf("%s: id %q is a subscription's", link, p.ID)
		}
	}
}

package provider

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/agentenv"
	"github.com/dieqiyun/uni-switch/backend/internal/catalog"
	"github.com/dieqiyun/uni-switch/backend/internal/filememo"
)

// mythosCatalogs puts models.dev's catalogs as they were on 10-27, cut to
// Claude, where uni-switch keeps them: Claude Mythos 5.1 is in the anthropic
// lab (models.json) and in no provider's list (api.json).
func mythosCatalogs(t *testing.T) {
	t.Helper()
	for src, dst := range map[string]string{
		"../catalog/testdata/models.dev-2026-10-27-claude.json":         catalog.CachePath(),
		"../catalog/testdata/models.dev-labs-2026-10-27-anthropic.json": catalog.LabsPath(),
	} {
		b, err := os.ReadFile(src)
		if err != nil {
			t.Fatal(err)
		}
		os.MkdirAll(filepath.Dir(dst), 0o755)
		if err := os.WriteFile(dst, b, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	catalog.Reset()
	t.Cleanup(catalog.Reset)
	filememo.Forget()
}

// what Claude Code keeps in .claude.json from Anthropic's answer to its
// bootstrap, in the shape its 2.1.311 reads it
func claudeCodeState(t *testing.T, path, email string, options []map[string]any, access []map[string]any, costs map[string]any) {
	t.Helper()
	writeFile(t, path, map[string]any{
		"oauthAccount":                     map[string]any{"emailAddress": email, "accountUuid": "u-" + email},
		"additionalModelOptionsCache":      options,
		"additionalModelOptionsAnsweredAt": time.Now().UnixMilli(),
		"modelAccessCache":                 access,
		"additionalModelCostsCache":        costs,
	})
}

// A Claude account lists a gated model Anthropic told its Claude Code it
// has, though models.dev's anthropic list no longer does: Claude Mythos
// 5.1, with its lab file's window and name and Anthropic's own price; an
// account not entitled to it isn't routed it (iebb on Discord).
func TestClaudeAccountListsGatedModels(t *testing.T) {
	home := claudeHome(t)
	claudeSignIn(t, home, time.Now().Add(time.Hour))
	claudeBase = "http://127.0.0.1:1" // asked nothing
	mythosCatalogs(t)
	if slices.Contains(modelIDs(catalog.Provider("anthropic")), "claude-mythos-5-1") {
		t.Fatal("fixture: models.dev lists Mythos 5.1 under anthropic again")
	}

	no := false
	claudeCodeState(t, filepath.Join(home, ".claude.json"), "iebb@example.com",
		[]map[string]any{
			{"value": "claude-mythos-5-1[1m]", "label": "Mythos 5.1", "description": "Restricted research model"},
			{"value": nil, "label": "Default", "description": "Recommended"},
			{"value": "claude-opus-3", "label": "Opus 3", "description": "Retired", "disabled": true},
		},
		[]map[string]any{{"apiName": "claude-mythos-5-1", "entitled": true}, {"apiName": "claude-opus-4-5", "entitled": no}},
		map[string]any{"claude-mythos-5-1": map[string]any{
			"inputTokens": 25, "outputTokens": 125, "promptCacheWriteTokens": 31.25,
			"promptCacheWrite1hTokens": 50, "promptCacheReadTokens": 2.5, "webSearchRequests": 0.01}})
	filememo.Forget()

	p, ok := find(All(), "claude")
	if !ok || p.Account == nil {
		t.Fatal("no Claude account")
	}
	ms := p.Available()
	i := slices.IndexFunc(ms, func(m catalog.Model) bool { return m.ID == "claude-mythos-5-1" })
	if i < 0 {
		t.Fatalf("Claude account's list has no Mythos 5.1: %v", modelIDs(ms))
	}
	if m := ms[i]; m.Name != "Mythos 5.1" || m.Context != 1_000_000 || m.Output != 128_000 || !m.Reasoning || len(m.Efforts) == 0 {
		t.Errorf("Mythos 5.1 = %+v", m)
	}
	if slices.Contains(modelIDs(ms), "claude-opus-3") {
		t.Error("a disabled option listed")
	}
	if pr, ok := p.ListPrice("claude-mythos-5-1"); !ok || pr.Input != 25 || pr.Output != 125 || pr.CacheWrite1h != 50 {
		t.Errorf("price = %+v %v", pr, ok)
	}
	fetched, err := p.Fetch(context.Background())
	if err != nil || !slices.Contains(modelIDs(fetched), "claude-mythos-5-1") {
		t.Fatalf("fetch: %v %v", modelIDs(fetched), err)
	}
	if e, ok := EntryOf("claude/claude-mythos-5-1"); !ok || e.Context != 1_000_000 {
		t.Errorf("entry: %+v %v", e, ok)
	}

	// routed to the account Anthropic entitles to it, not one it doesn't
	if !p.Account.Lists("claude-mythos-5-1") {
		t.Error("the entitled account doesn't list Mythos 5.1")
	}
	if p.Account.Lists("claude-opus-4-5") {
		t.Error("a model the account isn't entitled to is listed")
	}
	if !p.Account.Lists("claude-sonnet-5") {
		t.Error("a model models.dev lists for every account isn't listed")
	}
	other := &Account{Agent: "claude", User: "someone@example.com"}
	claudeCodeState(t, filepath.Join(claudeAccountDir(other.User), ".claude.json"), other.User,
		nil, []map[string]any{{"apiName": "claude-mythos-5-1", "entitled": no}}, nil)
	filememo.Forget()
	if other.Lists("claude-mythos-5-1") {
		t.Error("an account not entitled to Mythos 5.1 lists it")
	}
	if !other.Lists("claude-sonnet-5") {
		t.Error("the other account lost what every account has")
	}
	if nobody := (&Account{Agent: "claude", User: "unknown@example.com"}); !nobody.Lists("claude-mythos-5-1") {
		t.Error("an account no Claude Code has said of is taken to lack it")
	}

	// Claude Code's own picker on this computer offers it too
	if own := modelIDs(ClaudeCodeModels()); !slices.Contains(own, "claude-mythos-5-1") || slices.Contains(own, "claude-opus-4-5") {
		t.Errorf("ClaudeCodeModels = %v", own)
	}
}

// A vendor's own list that has a model models.dev keeps under its lab
// alone gets the lab's window, reply limit, name and thinking: an
// Anthropic API key whose /v1/models has Claude Mythos 5.1.
func TestAnthropicKeyGatedModelDescribed(t *testing.T) {
	isolate(t)
	h := t.TempDir()
	t.Setenv("HOME", h)
	t.Setenv("USERPROFILE", h)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(h, ".config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(h, ".cache"))
	t.Setenv("PATH", h)
	for _, v := range agentenv.Vars {
		t.Setenv(v, "")
	}
	mythosCatalogs(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" {
			http.NotFound(w, r)
			return
		}
		w.Write([]byte(`{"data":[{"type":"model","id":"claude-mythos-5-1","created_at":"2026-09-01T00:00:00Z"},` +
			`{"type":"model","id":"claude-sonnet-5","display_name":"Claude Sonnet 5"}],"has_more":false}`))
	}))
	defer srv.Close()
	if err := Save(Provider{ID: "anth", Name: "Anthropic key", Anthropic: srv.URL + "/v1", Key: "sk-ant-test"}); err != nil {
		t.Fatal(err)
	}
	q, err := Find("anth")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := q.Fetch(context.Background()); err != nil {
		t.Fatal(err)
	}
	e, ok := EntryOf("anth/claude-mythos-5-1")
	if !ok {
		t.Fatal("Mythos 5.1 not listed")
	}
	if e.Context != 1_000_000 || e.Output != 128_000 || e.Default != "Claude Mythos 5.1" && e.Name != "Claude Mythos 5.1" || len(e.Efforts) == 0 {
		t.Errorf("entry = id %s name %q default %q context %d output %d efforts %v", e.ID, e.Name, e.Default, e.Context, e.Output, e.Efforts)
	}
}

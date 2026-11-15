package usage

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/catalog"
	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// #1839: the calls uni-switch made by itself — its routing classifier's (UA
// uni-switch-router/1), a web search's, System One's decision — go to the same
// accounts and keys as the agents' and are counted in their totals, and
// each account, key and model says how many of them were uni-switch's own,
// what they came to and cost. A group with none says nothing of it.
func TestSummaryCountsUniSwitchsOwnCallsApart(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	os.MkdirAll(filepath.Dir(catalog.CachePath()), 0o755)
	os.WriteFile(catalog.CachePath(), []byte(`{"openai":{"id":"openai","models":{"gpt-5":{"id":"gpt-5","cost":{"input":1,"output":10}}}}}`), 0o644)
	catalog.Reset()
	t.Cleanup(catalog.Reset)
	if err := provider.Save(provider.Provider{ID: "cx", Name: "Codex", Key: "cx-secret", Chat: "https://chatgpt.example/v1", Catalog: "openai"}); err != nil {
		t.Fatal(err)
	}
	if err := provider.Save(provider.Provider{ID: "relay", Name: "Relay", Key: "relay-secret", Chat: "https://relay.example/v1", Catalog: "openai"}); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	key := provider.KeyID("relay-secret")
	recs := []Record{
		{Time: now, Agent: "codex_cli_rs/0.50", Provider: "cx", Model: "gpt-5", Host: "chatgpt.com as a@example.com", ProviderAccount: "a@example.com", Input: 1000, Output: 100, Status: 200},
		// the classifier's two asks of the turn, on the same account
		{Time: now, Agent: "uni-switch-router/1", Kind: "routing", Provider: "cx", Model: "gpt-5", Host: "chatgpt.com as a@example.com", ProviderAccount: "a@example.com", Input: 300, Output: 2, Status: 200},
		{Time: now, Agent: "uni-switch-router/1", Kind: "routing", Provider: "cx", Model: "gpt-5", Host: "chatgpt.com as a@example.com", ProviderAccount: "a@example.com", Input: 200, Output: 1, Status: 200},
		// a try of it handed on: its tokens, not a request
		{Time: now, Agent: "uni-switch-router/1", Kind: "routing", Provider: "cx", Model: "gpt-5", Host: "chatgpt.com as a@example.com", ProviderAccount: "a@example.com", Input: 10, Status: 429, HandedOn: true},
		// another account: the agent's alone
		{Time: now, Agent: "codex_cli_rs/0.50", Provider: "cx", Model: "gpt-5", Host: "chatgpt.com as b@example.com", ProviderAccount: "b@example.com", Input: 50, Output: 5, Status: 200},
		// a web search uni-switch made, on a key
		{Time: now, Agent: "uni-switch-search/1", Kind: "web_search", Provider: "relay", Model: "gpt-5", Host: "relay.example", ProviderKeyID: key, Input: 40, Output: 4, Status: 200},
		{Time: now, Agent: "claude-cli/2.1", Provider: "relay", Model: "gpt-5", Host: "relay.example", ProviderKeyID: key, Input: 60, Output: 6, Status: 200},
	}
	s := summarize(Month, now, recs)
	acct := map[string]Group{}
	for _, g := range s.Accounts {
		acct[g.ID] = g
	}
	a := acct["cx@a@example.com"]
	if a.Calls != 3 || a.Input != 1510 {
		t.Fatalf("uni-switch's calls are still the account's: %+v", a.Totals)
	}
	if a.Own.Calls != 2 || a.Own.Tokens != 513 { // the try handed on: its tokens, as Totals has them
		t.Fatalf("account a's own share: %+v", a.Own)
	}
	if want := (510*1.0 + 3*10.0) / 1e6; a.Own.Cost < want*0.99 || a.Own.Cost > want*1.01 {
		t.Fatalf("own cost %v, want %v", a.Own.Cost, want)
	}
	if b := acct["cx@b@example.com"]; b.Own != (OwnShare{}) {
		t.Fatalf("account b has none of uni-switch's: %+v", b.Own)
	}
	var k Group
	for _, g := range s.ProviderKeys {
		if g.Provider == "relay" {
			k = g
		}
	}
	if k.Calls != 2 || k.Own.Calls != 1 || k.Own.Tokens != 44 {
		t.Fatalf("key: %+v own %+v", k.Totals, k.Own)
	}
	// the model's, told apart by the account it went out as
	ownModel := 0
	for _, g := range s.Models {
		if g.Provider == "cx" {
			ownModel += g.Own.Calls
		}
	}
	if ownModel != 2 {
		t.Fatalf("model: %d own calls: %+v", ownModel, s.Models)
	}
	// uni-switch's calls are an agent's row of their own, as before ("uni-switch"
	// where the agents are loaded, its User-Agent's name here)
	mine := 0
	for _, g := range s.Agents {
		if (Record{Agent: g.ID}).ByUniSwitch() {
			mine += g.Calls
		} else if g.Own != (OwnShare{}) {
			t.Fatalf("an agent's row has an own share: %+v", g)
		}
	}
	if mine != 3 {
		t.Fatalf("uni-switch's agent rows: %d calls", mine)
	}
	// none is said as nothing at all
	b, _ := json.Marshal(acct["cx@b@example.com"])
	if strings.Contains(string(b), `"own"`) {
		t.Fatalf("a group with none of uni-switch's calls: %s", b)
	}
	b, _ = json.Marshal(a)
	if !strings.Contains(string(b), `"own":{"calls":2,"tokens":513`) {
		t.Fatalf("a's own share in JSON: %s", b)
	}
}

// Every request uni-switch sends by itself is uni-switch's own: the classifier, the
// search, the image description; an agent's is not.
func TestByUniSwitch(t *testing.T) {
	for ua, want := range map[string]bool{"uni-switch-router/1": true, "uni-switch-search/1": true, "uni-switch-vision/1": true, "uni-switch": true, "claude-cli/2.1": false, "codex_cli_rs/0.50": false, "": false} {
		if got := (Record{Agent: ua}).ByUniSwitch(); got != want {
			t.Errorf("%q: %v, want %v", ua, got, want)
		}
	}
}

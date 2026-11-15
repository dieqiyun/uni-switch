package provider

import (
	"context"
	"errors"
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/agentenv"
)

// noVendor answers no request and keeps what was asked: OpenCode tells
// a Zen key no balance, so there is nothing to ask.
type noVendor struct{ asked *[]string }

func (n noVendor) RoundTrip(r *http.Request) (*http.Response, error) {
	*n.asked = append(*n.asked, r.URL.String())
	return nil, errors.New("no network in this test")
}

// ttmrmx on X: an OpenCode Zen key's balance card wasn't shown at all.
// OpenCode tells no key the workspace's credits, so the card says that,
// and where they are shown, on every surface that asks a key's balance:
// the Usage page's cards (KeyBalances) and the editor, TUI and CLI
// (Balance). One card a provider, however many keys: the credits are the
// workspace's.
func TestOpenCodeZenSaysItTellsNoBalance(t *testing.T) {
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
	keyBalanceCache.data = nil
	t.Cleanup(func() { keyBalanceCache.data = nil })
	old := http.DefaultClient.Transport
	var asked []string
	http.DefaultClient.Transport = noVendor{&asked}
	t.Cleanup(func() { http.DefaultClient.Transport = old })

	zen := Provider{ID: "opencode-zen", Name: "OpenCode Zen", Chat: "https://opencode.ai/zen/v1", Responses: "https://opencode.ai/zen/v1",
		Anthropic: "https://opencode.ai/zen", Key: "zen-a", Keys: []KeyAccount{{Key: "zen-a"}, {Key: "zen-b", Name: "spare"}}}
	for _, p := range []Provider{
		zen,
		// Zen at the inference API, models.dev's URL for it
		{ID: "zen-inference", Name: "Zen", Chat: "https://opencode.ai/inference/openai/v1", Key: "zen-c"},
		// OpenCode Go: its plan windows are its card, not a balance
		{ID: "opencode-go", Name: "OpenCode Go", Chat: "https://opencode.ai/zen/go/v1", Key: "go-k"},
		// a provider of the user's own at the inference API: its vendor's
		{ID: "custom", Name: "Custom", Chat: "https://opencode.ai/inference/custom/acme/v1", Key: "c"},
	} {
		if err := Save(p); err != nil {
			t.Fatal(err)
		}
	}
	var lines []string
	for _, q := range KeyBalances(context.Background()) {
		lines = append(lines, q.Provider+"|"+q.User+"|"+q.Balance+"|"+q.SeeAt)
		if !strings.HasPrefix(q.Error, "OpenCode tells no API key its balance") || !strings.Contains(q.Error, "Overview → Credits") {
			t.Errorf("%s's card says %q", q.Provider, q.Error)
		}
	}
	want := "opencode-zen|||https://opencode.ai/console,zen-inference|||https://opencode.ai/console"
	if got := strings.Join(lines, ","); got != want {
		t.Fatalf("cards:\n%s\nwant\n%s", got, want)
	}
	if len(asked) != 0 {
		t.Fatalf("asked %v", asked)
	}
	amount, ok, err := Balance(context.Background(), zen)
	if !ok || amount != "" || !errors.Is(err, errOpenCodeNoBalance) {
		t.Fatalf("Balance = %q %v %v", amount, ok, err)
	}
	// a Balance URL of the user's own is still asked
	zen.BalanceURL = "https://opencode.ai/somewhere"
	if _, ok, err := Balance(context.Background(), zen); !ok || errors.Is(err, errOpenCodeNoBalance) {
		t.Fatalf("own Balance URL: %v %v", ok, err)
	}
	if strings.Join(asked, ",") != "https://opencode.ai/somewhere" {
		t.Fatalf("asked %v", asked)
	}
}

func TestOpenCodeGateway(t *testing.T) {
	for base, want := range map[string]string{
		"https://opencode.ai/zen/v1":                 "zen",
		"https://opencode.ai/zen":                    "zen",
		"https://opencode.ai/inference/openai/v1":    "zen",
		"https://opencode.ai/inference/anthropic":    "zen",
		"https://opencode.ai/zen/go/v1":              "go",
		"https://opencode.ai/zen/go":                 "go",
		"https://opencode.ai/inference/go/openai/v1": "go",
		"https://opencode.ai/inference/go/anthropic": "go",
		"https://opencode.ai/inference/custom/x/v1":  "",
		"https://opencode.ai/console":                "",
		"https://example.com/zen/go/v1":              "",
		"":                                           "",
	} {
		if got := openCodeGateway(base); got != want {
			t.Errorf("%q: %q, want %q", base, got, want)
		}
	}
}

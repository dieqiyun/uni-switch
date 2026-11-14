package tui

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/dieqiyun/uni-switch/backend/internal/agent"
	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// fakeCodex signs a ChatGPT account in to Codex, its models listed by a
// server of the test's own the way ChatGPT's backend answers. While down is
// there, the list fails, as with no network.
func fakeCodex(t *testing.T, h string) (down string) {
	t.Helper()
	down = filepath.Join(h, "codex-down")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, err := os.Stat(down); err == nil || r.URL.Path != "/backend-api/codex/models" {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		var ms []map[string]any
		for i, id := range []string{"gpt-5.5", "gpt-6-luna"} {
			ms = append(ms, map[string]any{"slug": id, "display_name": id, "visibility": "list", "priority": i + 1})
		}
		b, _ := json.Marshal(map[string]any{"models": ms})
		w.Write(b)
	}))
	old := provider.CodexBase
	provider.CodexBase = srv.URL + "/backend-api/codex"
	jwt := func(claims map[string]any) string {
		b, _ := json.Marshal(claims)
		return "e30." + base64.RawURLEncoding.EncodeToString(b) + ".sig"
	}
	b, _ := json.Marshal(map[string]any{"auth_mode": "chatgpt", "tokens": map[string]any{
		"id_token": jwt(map[string]any{"email": "me@example.com",
			"https://api.openai.com/auth": map[string]any{"chatgpt_plan_type": "pro", "chatgpt_account_id": "acct-1"}}),
		"access_token":  jwt(map[string]any{"exp": float64(time.Now().Add(24 * time.Hour).Unix())}),
		"refresh_token": "r", "account_id": "acct-1",
	}})
	write(t, filepath.Join(h, ".codex", "auth.json"), string(b))
	provider.ForgetAccounts()
	t.Cleanup(func() {
		provider.CodexBase = old
		srv.Close()
		provider.ForgetAccounts()
	})
	if p, err := provider.Find("codex"); err != nil || p.Account == nil || !p.Ready() {
		t.Fatalf("the ChatGPT account isn't signed in: %v", err)
	}
	// a cold models.dev sync answered here: no test request leaves this
	// process
	client := http.DefaultClient
	http.DefaultClient = &http.Client{Transport: modelsTransport{}}
	t.Cleanup(func() { http.DefaultClient = client })
	return down
}

// A subscription's list exists only at its vendor: the terminal asks for it
// at start and fills a model picker opened before it came, with no m on the
// Providers page (#1789, reyoung: Routing offered cursor/auto alone, from
// Cursor's built-in, since retired; Codex's ChatGPT account stands in).
func TestTUIFetchesSubscriptionModels(t *testing.T) {
	for _, view := range []string{"provider", "agent", "agent with catalog sync", "agent with catalog sync last"} {
		t.Run(view, func(t *testing.T) {
			fakeCodex(t, home(t))
			m := model{w: 120, h: 40, syncing: strings.Contains(view, "sync")}
			init := m.Init()
			m, want := openModelPicker(t, m, view)
			m = typeIn(m, "gpt-5.5")
			// the list fetched adds codex/gpt-5.5 under this filter: the
			// model on the cursor stays, not its row
			// (Codex's own picker has none before its list: nothing to hold)
			selected, _ := m.pk.choice()
			if selected == "" && view != "provider" {
				t.Fatalf("no GPT-5.5 in the picker\n%s", m.View())
			}
			m, _ = runModelsCmd(m, init, strings.HasSuffix(view, "last"))
			if got, _ := m.pk.choice(); selected != "" && got != selected || m.pk.input.Value() != "gpt-5.5" || m.mode != modePick {
				t.Fatalf("the list arriving changed the open picker: choice %q, filter %q, mode %v", got, m.pk.input.Value(), m.mode)
			}
			m.pk.input.SetValue("gpt-6-luna")
			m.pk.refilter()
			// a row of the list, not the typed text a picker that takes a
			// value of one's own hands back when no row matches
			if got, _ := m.pk.choice(); got != want || len(m.pk.match) == 0 {
				t.Fatalf("the picker hasn't Codex's listed model: got %q, want %q\n%s", got, want, m.View())
			}
		})
	}
}

// A list that couldn't be had at start (no network yet) is asked for again
// while the terminal stays open, not only after a restart.
func TestTUIAsksAgainForAMissingList(t *testing.T) {
	down := fakeCodex(t, home(t))
	write(t, down, "")
	old := modelsRetry
	modelsRetry = 0
	t.Cleanup(func() { modelsRetry = old })

	m, _ := openModelPicker(t, model{w: 120, h: 40}, "agent")
	m, retry := runModelsCmd(m, m.Init(), false)
	if retry == nil {
		t.Fatal("Codex's list failed and nothing asks for it again")
	}
	if err := os.Remove(down); err != nil {
		t.Fatal(err)
	}
	provider.ForgetAccounts() // the minute FetchNew waits after a failure
	m, again := runModelsCmd(m, retry, false)
	m.pk.input.SetValue("gpt-6-luna")
	m.pk.refilter()
	if got, _ := m.pk.choice(); got != "codex/gpt-6-luna" {
		t.Fatalf("asked again, still no gpt-6-luna: %q\n%s", got, m.View())
	}
	if again != nil {
		t.Fatal("asks again with every list there")
	}
}

// openModelPicker opens one of the terminal's four pickers of models, and
// says what Codex's gpt-6-luna is called in it.
func openModelPicker(t *testing.T, m model, view string) (model, string) {
	t.Helper()
	switch view {
	case "provider":
		m.page = pageProviders
		m.reloadProviders()
		for i, p := range m.provs {
			if p.ID == "codex" {
				m.prow = i
			}
		}
		m.openProviderModels("codex")
		return m, "gpt-6-luna"
	default:
		a, err := agent.Find("codex")
		if err != nil {
			t.Fatal(err)
		}
		m.agents = []*agent.Agent{a}
		m.reload()
		m.openFieldPicker()
		// Codex has no list of its own here (no models_cache.json), so the
		// models uni-switch serves on its ChatGPT account are offered via uni-switch
		// (codexViaUniSwitchFor)
		return m, "codex/gpt-6-luna"
	}
}

// runModelsCmd runs what Init or a retry returned and hands its messages to
// the model, as the program would, held until the picker is open to stand
// for a vendor answering late; reverse delivers a batch's last first. It
// returns the command the models' message left, the retry, if any.
func runModelsCmd(m model, cmd tea.Cmd, reverse bool) (model, tea.Cmd) {
	if cmd == nil {
		return m, nil
	}
	msg := cmd()
	if batch, ok := msg.(tea.BatchMsg); ok {
		var left tea.Cmd
		for i := range batch {
			j := i
			if reverse {
				j = len(batch) - 1 - i
			}
			var c tea.Cmd
			if m, c = runModelsCmd(m, batch[j], reverse); c != nil {
				left = c
			}
		}
		return m, left
	}
	next, left := m.Update(msg)
	return next.(model), left
}

type modelsTransport struct{}

func (modelsTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	if r.URL.Hostname() == "127.0.0.1" {
		return http.DefaultTransport.RoundTrip(r) // the test's own vendor
	}
	if r.URL.String() != "https://models.dev/api.json" {
		return nil, fmt.Errorf("unexpected request to %s", r.URL.Host)
	}
	return &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{"openai":{"id":"openai","models":{}}}`)), Request: r}, nil
}

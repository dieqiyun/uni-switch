package provider

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/catalog"
	"github.com/dieqiyun/uni-switch/backend/internal/settings"
)

func TestCommandCodePlanName(t *testing.T) {
	for id, want := range map[string]string{
		"individual-go": "Go", "individual-goat": "GOAT", "individual-pro-v1": "Pro", "individual_ultra": "Ultra",
		"teams-pro": "Teams Pro", "individual-provider": "Provider", "enterprise": "",
	} {
		if got := cmdPlanName(id); got != want {
			t.Errorf("%s: %q, want %q", id, got, want)
		}
	}
	if tm := cmdTime(float64(1_900_000_000_000)); tm == nil || tm.Unix() != 1_900_000_000 {
		t.Fatalf("ms: %v", tm)
	}
}

// Command Code answers a 200 with success false when it couldn't read the
// subscription ("write CONNECTION_CLOSED …"): the plan is unread, not none.
func TestCommandCodeSubscriptionUnread(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"success": false, "error": "write CONNECTION_CLOSED db.local:5432"})
	}))
	defer srv.Close()
	oldAPI := cmdAPI
	cmdAPI = srv.URL
	defer func() { cmdAPI = oldAPI }()
	if _, plan, _, _, ok := cmdSubscription(context.Background(), cmdAuth{APIKey: "k"}); ok || plan != "" {
		t.Fatalf("plan %q, ok %v: want it unread", plan, ok)
	}
}

// A Command Code model is told to the agents with a reply limit within the
// 200000 Command Code takes: models.dev gives DeepSeek V4 384000, and an
// agent asking for that was refused. Another provider's is left as it is.
func TestCommandCodeOutputCapped(t *testing.T) {
	m := catalog.Model{ID: "deepseek/deepseek-v4-pro", Context: 1_000_000, Output: 384_000}
	if got := entryFor(Provider{ID: CommandCodePlanID}, m, settings.Settings{}).Output; got != CommandCodeMaxOutput {
		t.Errorf("Command Code: output %d, want %d", got, CommandCodeMaxOutput)
	}
	if got := entryFor(Provider{ID: "deepseek"}, m, settings.Settings{}).Output; got != 384_000 {
		t.Errorf("deepseek: output %d, want 384000", got)
	}
	m.Output = 64_000
	if got := entryFor(Provider{ID: CommandCodePlanID}, m, settings.Settings{}).Output; got != 64_000 {
		t.Errorf("Command Code, within: output %d, want 64000", got)
	}
}

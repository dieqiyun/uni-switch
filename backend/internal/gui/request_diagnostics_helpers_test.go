package gui

import (
	"encoding/json"
	"github.com/dieqiyun/uni-switch/backend/internal/gateway"
	"github.com/dieqiyun/uni-switch/backend/internal/usage"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func ctxRoute(id int64, agent, session string, base, chat, window, cached int) gateway.Route {
	return gateway.Route{
		ID: id, Time: time.Unix(1_790_000_000+id*60, 0), Agent: agent, Session: session, Model: agent + "-model", Done: true, Status: 200,
		Usage: []gateway.RouteUsage{{CacheRead: cached}},
		Prompt: &gateway.Prompt{Window: window, Tokens: base + chat, Counted: true, Parts: []gateway.PromptPart{
			{Kind: gateway.PartSystem, Tokens: base / 2},
			{Kind: gateway.PartTools, Tokens: base - base/2, Items: []gateway.PromptItem{{Name: "linear", Tag: "mcp", Tokens: base * 2 / 5}}},
			{Kind: gateway.PartChat, Tokens: chat},
		}},
	}
}

func getJSON(t *testing.T, mux *http.ServeMux, url string, v any) {
	t.Helper()
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest("GET", url, nil))
	if rec.Code != 200 {
		t.Fatalf("%s: %d %s", url, rec.Code, rec.Body)
	}
	if err := json.Unmarshal(rec.Body.Bytes(), v); err != nil {
		t.Fatal(err)
	}
}

func holdUsageClock(t *testing.T, at time.Time) time.Time {
	t.Helper()
	old := usage.Clock
	usage.Clock = func() time.Time { return at }
	t.Cleanup(func() { usage.Clock = old })
	return at
}

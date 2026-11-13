package usage

import (
	"slices"
	"testing"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/settings"
)

// #1958 (NorethSea): a bridge that starts Codex's app-server under a name
// of its own was counted as a client apart from Codex. Counted as Codex in
// the user's settings, its calls are Codex's in the Usage summary, the
// request filters and rows, and Sessions, the history with them; the rows
// still say the client's own name, and the pair taken out counts it on
// its own again.
func TestClientCountedAsAgent(t *testing.T) {
	resetUsageTest(t)
	at := Clock().Add(-time.Hour)
	bridge := "hermes-codex-thread-bridge/0.4 (Mac OS 26.2; arm64)"
	Append(Record{Time: at, Agent: bridge, Session: "thread-1", Provider: "p", Model: "m", Input: 10, Output: 2, Status: 200})
	Append(Record{Time: at.Add(time.Minute), Agent: bridge, Session: "thread-1", Provider: "p", Model: "m", Input: 5, Output: 1, Status: 200})
	Append(Record{Time: at.Add(2 * time.Minute), Agent: "codex", Session: "thread-2", Provider: "p", Model: "m", Input: 7, Output: 3, Status: 200})

	agentIDs := func(s Summary) []string {
		var out []string
		for _, g := range s.Agents {
			out = append(out, g.ID)
		}
		slices.Sort(out)
		return out
	}
	// before: two clients, as the ledger records them
	if got := agentIDs(Summarize(All)); !slices.Equal(got, []string{"codex", "hermes-codex-thread-bridge"}) {
		t.Fatalf("agents before counting: %v", got)
	}
	// warm every cache the pages read, so counting has to reach past them
	_ = Vias(time.Time{})
	_ = GatewaySessions(time.Time{}, nil)
	_ = QueryPage(All, Filter{}, 0, 50)

	s := settings.Load()
	s.ClientAgents = map[string]string{"Hermes-Codex-Thread-Bridge": "codex"}
	if err := settings.Save(s); err != nil {
		t.Fatal(err)
	}
	sum := Summarize(All)
	if got := agentIDs(sum); !slices.Equal(got, []string{"codex"}) {
		t.Fatalf("Usage's agents with the bridge counted as Codex: %v", got)
	}
	if sum.Agents[0].Calls != 3 || sum.Agents[0].Input != 22 {
		t.Fatalf("Codex's row doesn't hold the bridge's calls: %+v", sum.Agents[0])
	}
	if got := Vias(time.Time{}); len(got.Of("codex|thread-1")) != 1 || len(got.Of("hermes-codex-thread-bridge|thread-1")) != 0 {
		t.Fatalf("Routing's sessions: %v", got)
	}
	g, ok := GatewaySessionByID("codex", "thread-1", nil)
	if !ok || g.Agent != "codex" || g.Input != 15 {
		t.Fatalf("Sessions doesn't have the bridge's thread as Codex's: %+v %v", g, ok)
	}
	// a native Codex session file of the same thread is the one shown
	if got := GatewaySessions(time.Time{}, map[string]bool{"codex|thread-1": true}); len(got) != 1 || got[0].ID != "thread-2" {
		t.Fatalf("the native session doesn't win over the bridge's: %+v", got)
	}
	page := QueryPage(All, Filter{Agent: "codex"}, 0, 50)
	if page.Total != 3 || !slices.Equal(page.Agents, []string{"codex"}) {
		t.Fatalf("Requests filtered to Codex: total %d, agents %v", page.Total, page.Agents)
	}
	clients := map[string]int{}
	for _, r := range page.Rows {
		clients[r.Client]++
	}
	if clients["hermes-codex-thread-bridge"] != 2 || clients[""] != 1 {
		t.Fatalf("the rows' own client names: %v", clients)
	}
	if rows, _, _ := Ledger(All, Filter{Agent: "codex"}); len(rows) != 3 {
		t.Fatalf("the ledger filtered to Codex: %d rows", len(rows))
	}
	// what the gateway goes by is the client's own name
	if got := AgentOf(bridge); got != "hermes-codex-thread-bridge" {
		t.Fatalf("AgentOf = %q", got)
	}

	// taken out, the bridge is on its own again, the history with it
	s = settings.Load()
	s.ClientAgents = nil
	if err := settings.Save(s); err != nil {
		t.Fatal(err)
	}
	if got := agentIDs(Summarize(All)); !slices.Equal(got, []string{"codex", "hermes-codex-thread-bridge"}) {
		t.Fatalf("agents after the pair is taken out: %v", got)
	}
	if got := QueryPage(All, Filter{Agent: "codex"}, 0, 50); got.Total != 1 {
		t.Fatalf("Requests filtered to Codex after: %d", got.Total)
	}
	if _, ok := GatewaySessionByID("codex", "thread-1", nil); ok {
		t.Fatal("Sessions still has the bridge's thread as Codex's")
	}
}

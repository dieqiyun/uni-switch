package gateway

// GET /v1/uni-switch/claude-runs: the Claude Code processes the Claude
// subscription's bridge has running, for `uni-switch claude-code runs`.
//
// Each conversation on a Claude account is answered by a real Claude Code
// (`claude -p`), with uni-switch's MCP helper (`uni-switch claude-mcp-helper`)
// beside it for the client's tools, and a run is kept for the
// conversation's next turn (idleMost, idleLongest) or while it waits on
// the client's tool results (parkLongest, its 30-minute timer). Claude
// Code's parallel subagents are conversations of their own, so a session
// that starts many has many pairs running. In Activity Monitor or Task
// Manager they show as Claude Code's version ("2.1.311": its binary is
// named so) and "uni-switch", with nothing to say they are uni-switch's or why
// they stay (@Candydidi3 on X: "uni-switch里拉起很多子进程"). This lists each
// with its pid, model, account, what it waits for and since when.

import (
	"net/http"
	"slices"
	"time"
)

// ClaudeRunsPath is where the gateway lists its Claude Code runs.
const ClaudeRunsPath = "/v1/uni-switch/claude-runs"

// ClaudeRun is one Claude Code process a conversation runs on.
type ClaudeRun struct {
	PID     int    `json:"pid"`
	Model   string `json:"model"`
	Account string `json:"account,omitempty"`
	// State is "answering" (a turn is under way), "idle" (kept for the
	// conversation's next turn) or "tools" (its reply called the client's
	// tools, whose results it waits for); Since is when it began to.
	State   string    `json:"state"`
	Since   time.Time `json:"since"`
	Started time.Time `json:"started"`
	// Ends is when uni-switch lets it go if nothing comes for it (its timer).
	Ends time.Time `json:"ends,omitzero"`
	// RSS is Claude Code's resident memory in bytes, 0 when not read.
	RSS int64 `json:"rss,omitempty"`
}

// ClaudeRuns is what ClaudeRunsPath answers.
type ClaudeRuns struct {
	Object string      `json:"object"` // "claude-runs"
	Runs   []ClaudeRun `json:"runs"`
	// Live is every Claude Code uni-switch has running, the runs and its
	// one-off asks (a test, a warm-up, a /usage read) together.
	Live int `json:"live"`
	// Most is how many may run at once (0: no bound), MostWhy what says so.
	Most    int    `json:"most"`
	MostWhy string `json:"mostWhy,omitempty"`
	// IdleMost runs are kept for a next turn, IdleFor each at most.
	IdleMost int           `json:"idleMost"`
	IdleFor  time.Duration `json:"idleFor"`
}

// runTimeout is how long a run is kept with nothing coming for it while
// a turn is under way or its client's tools run: its timer (endIn).
const runTimeout = 30 * time.Minute

// claudeRunList is the bridge's runs, oldest first, and claudeRoom's count.
func (b *subscriptionBridge) claudeRunList() ClaudeRuns {
	type seen struct {
		run  *subscriptionRun
		idle time.Time
	}
	var all []seen
	b.mu.Lock()
	for _, r := range b.runs {
		s := seen{run: r}
		if r.idleKey != "" && b.idle[r.idleKey] == r {
			s.idle = r.idleAt
		}
		all = append(all, s)
	}
	b.mu.Unlock()
	out := ClaudeRuns{Object: "claude-runs", Runs: []ClaudeRun{}, IdleMost: idleMost, IdleFor: idleLongest}
	for _, s := range all {
		r := s.run
		r.mu.Lock()
		closed, parked, tree, active, ends := r.closed, r.parkedAt, r.tree, r.active, r.ends
		r.mu.Unlock()
		if closed {
			continue
		}
		user, _ := ownerAccount(r.owned())
		c := ClaudeRun{PID: r.pid(), Model: r.model, Account: user, Started: r.started, State: "answering", Since: active, Ends: ends}
		switch {
		case !s.idle.IsZero():
			c.State, c.Since = "idle", s.idle
		case !parked.IsZero():
			c.State, c.Since = "tools", parked
		}
		if c.Since.IsZero() {
			c.Since = r.started
		}
		if tree != nil {
			if rss, ok := claudeRunRSS(tree); ok {
				c.RSS = rss
			}
		}
		out.Runs = append(out.Runs, c)
	}
	slices.SortFunc(out.Runs, func(a, b ClaudeRun) int { return a.Started.Compare(b.Started) })
	claudeRoom.mu.Lock()
	out.Live = claudeRoom.live
	claudeRoom.mu.Unlock()
	out.Most, out.MostWhy = claudeRunsMost()
	return out
}

// claudeRuns answers ClaudeRunsPath, to a program on this computer only:
// it names the accounts and models in use.
func (s *Server) claudeRuns(w http.ResponseWriter, r *http.Request) {
	if why := profileRefused(r, "list of Claude Code processes", "uni-switch claude-code runs"); why != "" {
		writeJSON(w, http.StatusForbidden, map[string]any{"error": map[string]any{"message": why}})
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, s.subscription.claudeRunList())
}

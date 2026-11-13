package gateway

// A group's rules (provider.Rule) put one member first for the requests
// they match. A rule is looked at when a user's turn begins, and what it
// decided holds for the rest of the turn: the agent's tool results go back
// to the model that asked for them, however long the conversation grows
// meanwhile.

import (
	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// RuleHit is what the trace tells of a group's rules for a request.
type RuleHit struct {
	N   int    `json:"n"`             // the rule, from 1; 0 when none matched
	Use string `json:"use,omitempty"` // the member it puts first
	// More: the members the rule names after Use (provider.Rule.More), in
	// order: they go next, before Then
	More []string `json:"more,omitempty"`
	When []string `json:"when,omitempty"`
	Held bool     `json:"held,omitempty"` // decided when the turn began
	// Waits: the turn began before uni-switch saw it (or its rule was changed
	// since), so no rule moves it; the next turn is looked at afresh
	Waits bool `json:"waits,omitempty"`
	// Grown: within the turn the conversation grew past what the model it
	// was on can take, so the rules were looked at again — the one time a
	// turn moves
	Grown  bool   `json:"grown,omitempty"`
	Turn   int    `json:"turn"`             // the user's turns so far
	Tokens int    `json:"tokens"`           // how long the request was taken to be
	Images bool   `json:"images,omitempty"` // it carries an image
	Effort string `json:"effort,omitempty"` // the reasoning asked for: a level, or "on"
	// Folder: the working folder the agent said, looked at when a rule
	// is by folder; "" when it said none
	Folder string `json:"folder,omitempty"`
	// Unready: the member the rule names has nobody to take it now, and
	// the group routes the request as it would without the rule.
	Unready bool `json:"unready,omitempty"`
	// Then: the members the rules after it that match too send to, which
	// the request fails over to, in turn, before the group's others (#112)
	Then []string `json:"then,omitempty"`
	// Instead: the one of Then that went first, Use having nobody ready
	Instead string `json:"instead,omitempty"`
	// Classified: the group's classifier was asked which intent the
	// turn's first message is
	Classified *Classified `json:"classified,omitempty"`
	// Pick: the reasoning the group's decision model picked for the turn,
	// which its requests ask their model for (provider.EffortAuto)
	Pick string `json:"pick,omitempty"`
	// Wanted: the reasoning the decision model picked for the turn when
	// the conversation kept a higher one (Pick) for the vendor's cache of
	// it (#617) — sent instead where the change goes as an effort update
	// that keeps the cache (takesEffortUpdates)
	Wanted string `json:"wanted,omitempty"`
	// Compact: the request is the agent compacting its conversation, looked
	// at on its own; Small, the members of rules it matched that take less
	// than it is long, passed over
	Compact bool     `json:"compact,omitempty"`
	Small   []string `json:"small,omitempty"`
	// Bare: the group has no rules, only its effort picked
	Bare bool `json:"bare,omitempty"`
	// UseEffort: the reasoning the rule asks the member it names for
	// (provider.Rule.UseEffort, #2633), in place of the agent's
	UseEffort string `json:"useEffort,omitempty"`
	// members are the models of the member the rule names, as the group
	// whose rule it is has them (ruleOver): who UseEffort is for
	members []provider.Member
}

// Classified is what the classifier was asked as a turn began, and said.
type Classified struct {
	By      string   `json:"by"`               // the classifier model
	Intents []string `json:"intents"`          // what it chose among
	Intent  string   `json:"intent,omitempty"` // what it said the message is; "" for none
	// Said: the answer as it said it — the number a chat model answered
	// with (0 for none), or the choice a decision model named, its own
	// "none of these" included — kept when no intent came of it, so
	// "none" is told from an answer no rule could take (#4598)
	Said   string  `json:"said,omitempty"`
	After  string  `json:"after,omitempty"`  // what it said the turn before was, which it was told
	Effort string  `json:"effort,omitempty"` // the reasoning it said the turn wants
	Score  float64 `json:"score,omitempty"`  // how hard it took the turn to be, 0 (low) to 3 (xhigh)
	Sure   float64 `json:"sure,omitempty"`   // how confident it was of the intent, 0 to 1
	Cached bool    `json:"cached,omitempty"` // said before, for the same message
	Ms     int     `json:"ms,omitempty"`     // how long asking it took
	Error  string  `json:"error,omitempty"`  // why it couldn't say: no intent matches then, unless Fallback
	// Above: the reasoning a group above this one picked for the turn, so
	// this one's classifier wasn't asked for it (#4358): the turn reasons
	// at that, not as the agent asked
	Above string `json:"above,omitempty"`
	// Fallback: "previous" when it couldn't say and the turn keeps the
	// intent the turn before had (After), rather than none
	Fallback string `json:"fallback,omitempty"`

	// AfterEffort: the reasoning picked for the turn before, which it was told
	AfterEffort string `json:"afterEffort,omitempty"`
}

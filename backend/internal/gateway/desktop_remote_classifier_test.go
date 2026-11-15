package gateway

import (
	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// A Claude Desktop session on another uni-switch's Claude model keeps Claude
// Code's own background requests on the model they name, as served by that
// uni-switch: its auto mode's classifier asks for claude-sonnet-5 by Claude's
// id, which the Remote uni-switch lists as office/claude/claude-sonnet-5, and
// went to the session's office/claude/claude-opus-5-5 instead (#2187,
// SuInk: 9 sonnet calls answered by opus, 480K tokens of 1h cache written).
// Desktop's title still follows the session, and a model the remote
// doesn't serve still goes to the session's.

// the other computer's Claude model, under a provider "work": its
// subscription is "claude" there, which a test can't sign in to

// the models the other uni-switch was asked for

// its model list, as that uni-switch gives it

// here, only the other uni-switch serves Claude

// the session is on the other uni-switch's opus, by the id Desktop was
// shown it by

// Claude Code's safety and notification classifiers name their sonnet

// Desktop's title follows the session

// a Claude model the other uni-switch doesn't serve goes to its model of
// the same tier (cybercaius: a haiku call stays a haiku call on the
// session's provider), and one of a tier it has none of to the session's

func entryOf(id string) (provider.Entry, bool) {
	for _, e := range provider.Catalog() {
		if e.ID == id {
			return e, true
		}
	}
	return provider.Entry{}, false
}

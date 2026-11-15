package gateway

import (
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// A conversation affine keeps on an account by the account alone — the
// model that answered it has left the group, and the widest of affine's
// matches still holds it there — loses its stick when that reply breaks
// off with nothing resting it (too long for its model, a refusal):
// unanswered forgot a stick only when the model answered matched too, so
// the agent's retry went straight back to the account whose reply had
// just broken off, which is what #733 is about.
func TestUnansweredForgetsTheAccountWhenItsModelIsGone(t *testing.T) {
	fresh(t)
	const key = "group/g|s1"
	flaky := candidate{p: provider.Provider{ID: "flaky", Key: "broken"}, rest: "flaky"}
	answered(key, candidate{p: provider.Provider{ID: "flaky", Key: "broken"}, model: "m", rest: "flaky"}, 1, Usage{})
	answered(key+"-other", candidate{p: provider.Provider{ID: "steady", Key: "fine"}, model: "m", rest: "steady"}, 1, Usage{})
	unanswered(key, candidate{p: flaky.p, model: "m2", rest: flaky.rest}) // the model that answered is gone from the group; the account isn't
	sticks.Lock()
	_, kept := sticks.m[key]
	_, other := sticks.m[key+"-other"]
	sticks.Unlock()
	if kept {
		t.Fatal("the conversation is still kept on the account whose model has left the group")
	}
	if !other {
		t.Fatal("another conversation's stick was forgotten")
	}
}

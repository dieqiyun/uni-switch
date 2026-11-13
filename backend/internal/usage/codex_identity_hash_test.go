package usage

import (
	"crypto/sha256"
	"fmt"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/sessions"
)

// A request state's key changes with any thread's identity, and only with
// one: the same identities give the same key however the map was filled,
// and a field's end is not read as the next one's start.
func TestCodexIdentitiesHashed(t *testing.T) {
	sum := func(ids map[string]sessions.CodexIdentity) string {
		h := sha256.New()
		hashCodexIdentities(h, ids)
		return fmt.Sprintf("%x", h.Sum(nil))
	}
	many := func(n int, from int) map[string]sessions.CodexIdentity {
		ids := map[string]sessions.CodexIdentity{}
		for i := range n { // more than one write's worth
			k := fmt.Sprintf("019a%028d", (i+from)%n)
			ids[k] = sessions.CodexIdentity{ID: k, Role: "main", RootSession: k}
		}
		return ids
	}
	base := sum(many(5000, 0))
	if got := sum(many(5000, 1234)); got != base {
		t.Fatal("the same identities, filled in another order, hashed apart")
	}
	child := many(5000, 0)
	k := fmt.Sprintf("019a%028d", 4321)
	child[k] = sessions.CodexIdentity{ID: k, Role: "subagent", ParentSession: "p", RootSession: "p"}
	if sum(child) == base {
		t.Fatal("a thread that became a subagent left the key as it was")
	}
	a := map[string]sessions.CodexIdentity{"t": {ID: "ab", Role: "c"}}
	b := map[string]sessions.CodexIdentity{"t": {ID: "a", Role: "bc"}}
	if sum(a) == sum(b) {
		t.Fatal("fields run together: ab|c hashed as a|bc")
	}
	if sum(nil) == sum(a) {
		t.Fatal("no identities hashed as one")
	}
}

package gui

import (
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/filememo"
)

// The last-known state is uni-switch's own cache, and nothing a request reads
// is built from it. Its write dropped every file filememo held, so each
// poll of the Agents page that wrote it had each agent's model list and
// picker, and every provider's catalog, built anew on the next look.
func TestKeptStateLeavesFilememo(t *testing.T) {
	sandboxHome(t)
	forgetLastState()
	t.Cleanup(forgetLastState)
	gen := filememo.Gen()
	keepState(stateJSON{Agents: []agentJSON{{ID: "a1"}}})
	keepState(stateJSON{Agents: []agentJSON{{ID: "a2"}}})
	if filememo.Gen() != gen {
		t.Fatal("writing the last-known state made filememo read every file again")
	}
	forgetLastState()
	if st, ok := lastKnown(); !ok || len(st.Agents) != 1 || st.Agents[0].ID != "a2" {
		t.Fatalf("the state kept last is not the one read back: %+v %v", st.Agents, ok)
	}
}

package gateway

import (
	"fmt"

	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// Two requests of one conversation run at once (a turn and its title, a
// retry beside the original): one remembers a refusal while the other
// orders its members by what is remembered. Under -race the order reads
// what is remembered while it is written.
func TestOverflowMemoryReadWhileNoted(t *testing.T) {
	fresh(t)
	cs := []candidate{{p: provider.Provider{ID: "a"}, model: "m"}, {p: provider.Provider{ID: "b"}, model: "m"}}
	pl := planned{order: make([]Weighed, len(cs))}
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 2000; i++ {
			noteOverflow("conv", candidate{p: provider.Provider{ID: "a"}, model: fmt.Sprint(i % 7)}, 10)
			forgetOverflow("conv", candidate{p: provider.Provider{ID: "a"}, model: fmt.Sprint(i % 5)})
		}
	}()
	noteOverflow("conv", cs[0], 10)
	for i := 0; i < 2000; i++ {
		behindOverflow("conv", cs, pl, 20)
	}
	<-done
}

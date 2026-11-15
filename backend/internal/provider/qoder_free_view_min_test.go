package provider

import (
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/update"
)

// Qoder's plugin before 0.2.52 read a model list with every paid model off
// and the free ones on, what Qoder CN answers a job token it doesn't take
// for the account, as it came: a Qoder CN account showed the free Flash
// alone, and so did its agents. 0.2.52 renews the token once and reads the
// list again. The mover keeps every moved user on a version that does.
func TestQoderMoverHasTheFreeViewRenewal(t *testing.T) {
	for _, id := range []string{"qoder", QoderCNID} {
		if update.Newer("0.2.52", movers[id].min) {
			t.Fatalf("the %s mover installs opencode-qoder-auth %s, which lists only the free models on a job token Qoder no longer takes in full", id, movers[id].min)
		}
	}
}

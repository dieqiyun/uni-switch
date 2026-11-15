package agent

import (
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// Every cached list checks the provider stamp before reusing its contents.
// Go 1.26's Windows environment reads allocate per OS value. Measure that
// shared invalidation work independently, so cache budgets still detect
// rebuilding model/option lists instead of measuring the OS environment size.
func stampAllocs() float64 {
	return testing.AllocsPerRun(20, func() { provider.StampNow() })
}

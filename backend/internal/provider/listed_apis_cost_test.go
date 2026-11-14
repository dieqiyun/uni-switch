package provider

import (
	"fmt"
	"os"
	"runtime"
	"testing"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/catalog"
)

// A provider's model list is weighed model by model (Exposed's NoAPIFor
// since 644ad1ed7, /v1/models' native endpoints), and each look at a
// model's APIs copied and scanned the whole saved list: 2,500 models'
// /v1/models took 3.4s from v0.1.1234, where v0.1.1232 took 1.2s (K.Liu
// on Discord). Four times the models is four times the work, not
// sixteen.
func TestListedAPIsCostGrowsWithTheList(t *testing.T) {
	oneHome(t)
	cost := func(n int) uint64 {
		id := fmt.Sprintf("many-%d", n)
		if err := Save(Provider{ID: id, Name: id, Key: "sk-x", Chat: "http://127.0.0.1:1/v1"}); err != nil {
			t.Fatal(err)
		}
		ms := make([]catalog.Model, n)
		for i := range ms {
			ms[i] = catalog.Model{ID: fmt.Sprintf("model-%04d", i), Name: fmt.Sprintf("Model %d", i), APIs: []string{"chat"}}
		}
		if err := catalog.SaveLive(id, "http://127.0.0.1:1/v1", ms); err != nil {
			t.Fatal(err)
		}
		// saved a while ago, as a list is when an app asks for it, not
		// read again in full for its first seconds
		old := time.Now().Add(-time.Minute)
		if err := os.Chtimes(catalog.LivePath(id), old, old); err != nil {
			t.Fatal(err)
		}
		p, err := Find(id)
		if err != nil {
			t.Fatal(err)
		}
		if got := len(p.Exposed()); got != manyModels {
			t.Fatalf("%d models: exposed %d, want %d", n, got, manyModels)
		}
		var a, b runtime.MemStats
		runtime.ReadMemStats(&a)
		p.Exposed()
		runtime.ReadMemStats(&b)
		return b.TotalAlloc - a.TotalAlloc
	}
	small, large := cost(500), cost(2000)
	if large > 8*small {
		t.Fatalf("Exposed allocated %d bytes for 500 models and %d for 2000: %.1f times for 4 times the models", small, large, float64(large)/float64(small))
	}
}

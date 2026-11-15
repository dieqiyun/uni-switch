package cli

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// Bailian's decision model is added with its workspace or plan, and a
// System One API of another vendor with decide= (#647).
func TestProviderDecidePairs(t *testing.T) {
	if _, err := provider.FromPreset("bailian-decision"); err == nil {
		t.Fatal("retired preset available")
	}
	for _, pair := range []string{"workspace=ws-1", "plan=token-plan", "region=mars"} {
		p := provider.Provider{ID: "saved", Name: "Saved", Decide: "https://example.invalid/v1"}
		if err := applyPairs(&p, []string{pair}); err == nil {
			t.Errorf("invented endpoint for %s", pair)
		}
	}
	c := provider.Provider{Name: "My Decider"}
	if err := applyPairs(&c, []string{"decide=https://decide.example.com/v1", "models=my-decision-model", "key=sk-1"}); err != nil {
		t.Fatal(err)
	}
	if !c.DecideOnly() || c.Jev() != "my-decision-model" || !c.DecidesModel("my-decision-model") {
		t.Fatalf("custom: %+v asked as %s", c, c.Jev())
	}
}

func TestRemoteUniSwitchAddReportsModels(t *testing.T) {
	groupsHome(t)
	var asked atomic.Int32
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		asked.Add(1)
		w.Write([]byte(`{"data":[{"id":"lib/m1"}]}`))
	}))
	defer up.Close()
	_, err := stdoutOf(t, func() error {
		return providerCmd([]string{"provider", "add", "remote-uni-switch", "remote-key", "url=" + up.URL, "id=office"})
	})
	if err == nil {
		t.Fatal("retired remote provider added")
	}
	if _, err := provider.Find("office"); err == nil {
		t.Fatal("retired provider saved")
	}
	if asked.Load() != 0 {
		t.Fatal("remote model discovery was attempted")
	}
}
func TestTencentOldRegionAtCLI(t *testing.T) {
	p := provider.Provider{ID: "tencent-cloud", Name: "Saved Tencent", Preset: "tencent-cloud", Chat: "https://tokenhub.tencentmaas.com/plan/v3"}
	for _, region := range []string{"intl", "enterprise"} {
		if err := applyPairs(&p, []string{"region=" + region}); err == nil {
			t.Fatalf("retired region %s accepted", region)
		}
		if p.Chat != "https://tokenhub.tencentmaas.com/plan/v3" {
			t.Fatal("changed saved endpoint")
		}
	}
}

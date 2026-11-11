package agent

import (
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/gateway"
	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// Claude Desktop's subagent model field: offered uni-switch's models once
// Desktop is on uni-switch, a pick connecting it as any model pick does, an
// alias kept by its catalog id, one not served now kept, a bare name
// refused. Desktop's tiers and Codex's subagent model aren't Live, and a
// stopped WSL copy keeps the flag.
func TestDesktopSubagentModelField(t *testing.T) {
	home, _ := desktopSandbox(t)
	if err := provider.Save(provider.Provider{ID: "fake", Name: "Fake", Chat: "http://127.0.0.1:1/v1", Key: "k", Models: []string{"m1"}}); err != nil {
		t.Fatal(err)
	}
	a := claudeDesktop(home)
	f := a.Field("subagent_model")
	if f == nil || f.Label != "subagent model" || !f.Quiet || !f.Live {
		t.Fatalf("subagent field: %+v", f)
	}
	if f.Options(nil) != nil {
		t.Fatal("subagent options offered while unwired")
	}
	if err := a.Pick(f.Key, "fake/m1"); err != nil {
		t.Fatal(err)
	}
	if !a.Wired() || len(f.Options(nil)) == 0 {
		t.Fatal("subagent pick did not wire Desktop or offer its models")
	}
	if err := f.Set(gateway.DesktopID(provider.Entry{ID: "fake/m1"})); err != nil {
		t.Fatal(err)
	}
	if got := f.Get(); got != "fake/m1" {
		t.Fatalf("alias pick %q, want fake/m1", got)
	}
	if err := f.Set("gpt-6"); err == nil {
		t.Fatal("accepted a bare model")
	}
	if err := f.Set("missing/m"); err != nil || f.Get() != "missing/m" {
		t.Fatalf("restored model %q: %v", f.Get(), err)
	}
	if err := f.Set(""); err != nil || f.Get() != "" {
		t.Fatalf("clear %q: %v", f.Get(), err)
	}
	for _, tier := range gateway.DesktopTierNames {
		if a.Field(tier).Live {
			t.Fatalf("tier %s is Live", tier)
		}
	}
	if codex(home).Field("subagent_model").Live {
		t.Fatal("Codex's subagent model became Live")
	}
	stopped := asleep(a, wslKind{}, distro{Name: "Stopped"})
	if !stopped.Field("subagent_model").Live {
		t.Fatal("stopped WSL agent lost the Live flag")
	}
}

func TestDesktopSubagentEffortField(t *testing.T) {
	home, _ := desktopSandbox(t)
	if err := provider.Save(provider.Provider{ID: "fake", Name: "Fake", Chat: "http://127.0.0.1:1/v1", Key: "k", Models: []string{"m1", "m2"}}); err != nil {
		t.Fatal(err)
	}
	if err := provider.SetModelEfforts("fake/m1", []string{"low", "high"}); err != nil {
		t.Fatal(err)
	}
	a := claudeDesktop(home)
	f := a.Field("subagent_effort")
	if f == nil || !f.Live || !f.Quiet || f.Label != "subagent effort" {
		t.Fatalf("effort field: %+v", f)
	}
	if len(f.Options(nil)) != 0 || f.Set("high") != nil || a.Wired() {
		t.Fatal("saving an unwired effort offered choices or connected Desktop")
	}
	if err := a.Pick("provider", "uni-switch"); err != nil {
		t.Fatal(err)
	}
	if len(f.Options(nil)) != len(provider.MemberEfforts) {
		t.Fatal("inherited model lost effort choices")
	}
	if err := a.Pick("subagent_model", "fake/m1"); err != nil {
		t.Fatal(err)
	}
	opts := f.Options(nil)
	if len(opts) != 2 || opts[0].Value != "low" || opts[1].Value != "high" {
		t.Fatalf("model's effort choices: %+v", opts)
	}
	if err := a.Pick(f.Key, " HIGH "); err != nil || f.Get() != "high" {
		t.Fatalf("effort pick %q: %v", f.Get(), err)
	}
	if d := a.Drift(); d != nil {
		t.Fatalf("gateway effort caused config drift: %+v", d)
	}
	if !asleep(a, wslKind{}, distro{Name: "Stopped"}).Field(f.Key).Live {
		t.Fatal("stopped WSL field lost Live")
	}
	if err := provider.SetModelReasonless("fake/m1", true); err != nil {
		t.Fatal(err)
	}
	if len(f.Options(nil)) != 0 || f.Get() != "high" {
		t.Fatal("reasonless model offered levels or discarded the saved pick")
	}
	if err := f.Set("hard"); err == nil || f.Get() != "high" {
		t.Fatal("invalid effort overwrote the pick")
	}
	if err := f.Set(""); err != nil || f.Get() != "" {
		t.Fatalf("clear effort: %v", err)
	}
	// The effort that was there before connecting (a restored setting)
	// can be restored while disconnected, and the last live pick returns.
	if err := a.Pick(f.Key, "low"); err != nil {
		t.Fatal(err)
	}
	if err := a.Disconnect(); err != nil || a.Wired() || f.Get() != "high" {
		t.Fatalf("disconnect did not restore the saved preference: %q, %v", f.Get(), err)
	}
	if err := a.Connect(); err != nil || !a.Wired() || f.Get() != "low" {
		t.Fatalf("reconnect did not restore the live pick: %q, %v", f.Get(), err)
	}
}

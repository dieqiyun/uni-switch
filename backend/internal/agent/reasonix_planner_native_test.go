package agent

import (
	"slices"
	"testing"
)

func TestReasonixPlannerNativeSelections(t *testing.T) {
	const original = "default_model = \"deepseek-flash\"\n[agent]\nplanner_model = \"deepseek-pro\"\n"
	t.Run("current", func(t *testing.T) {
		a, _ := reasonixFixture(t, original)
		if err := a.Field("planner").Set("deepseek-pro"); err != nil {
			t.Fatalf("the current native planner cannot be reselected: %v", err)
		}
	})
	t.Run("executor option", func(t *testing.T) {
		a, _ := reasonixFixture(t, original)
		f := a.Field("planner")
		if !slices.ContainsFunc(f.Options(a.Values()), func(o Option) bool { return o.Value == "deepseek-flash" }) {
			t.Fatal("the native executor is missing from the shared model options")
		}
		if err := f.Set("deepseek-flash"); err != nil {
			t.Fatalf("the planner refuses an offered native model: %v", err)
		}
	})
	t.Run("return", func(t *testing.T) {
		a, _ := reasonixFixture(t, original)
		f := a.Field("planner")
		if err := f.Set("uni-switch/a/pro"); err != nil {
			t.Fatal(err)
		}
		if !slices.ContainsFunc(f.Options(a.Values()), func(o Option) bool { return o.Value == "deepseek-pro" }) {
			t.Fatal("the original native planner disappears from its model options")
		}
		if err := f.Set("deepseek-pro"); err != nil {
			t.Fatalf("the original native planner cannot be reselected: %v", err)
		}
		cfg, err := readReasonix(a.Path)
		if err != nil || cfg.Agent.Planner == nil || *cfg.Agent.Planner != "deepseek-pro" || cfg.uniSwitch().Name != "" {
			t.Fatalf("native planner restoration left a managed provider: %+v %v", cfg, err)
		}
	})
}

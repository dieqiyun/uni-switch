package agent

import (
	"github.com/dieqiyun/uni-switch/backend/internal/edit"
	"os"
	"testing"
)

func TestReasonixIndependentRolesAndRestore(t *testing.T) {
	a, env := reasonixFixture(t, reasonixNativeConfig+"\n[agent]\nplanner_model = \"native/other\"\n")
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(a.Field("planner").Set("uni-switch/a/pro"))
	if got := a.Field("model").Get(); got != "native/old" {
		t.Fatalf("Plan changed Executor: %s", got)
	}
	must(a.Field("model").Set("uni-switch/a/flash"))
	if got := a.Field("planner").Get(); got != "uni-switch/a/pro" {
		t.Fatalf("Executor changed Plan: %s", got)
	}
	must(a.Field("model").Set("native/other"))
	cfg, err := readReasonix(a.Path)
	must(err)
	if cfg.uniSwitch().Name == "" || a.Check() != "" {
		t.Fatal("Executor switch removed active Plan connection")
	}
	must(a.Field("planner").Set(""))
	must(a.Field("model").Set(""))
	cfg, err = readReasonix(a.Path)
	must(err)
	if cfg.uniSwitch().Name != "" || *cfg.DefaultModel != "native/other" || cfg.Agent.Planner == nil || *cfg.Agent.Planner != "native/other" {
		t.Fatalf("incorrect restore: %+v", cfg)
	}
	if _, exists := editEnvForReasonixTest(env); exists {
		t.Fatal("unused gateway credential survived")
	}
}

func editEnvForReasonixTest(path string) (string, bool) { return edit.GetEnvFile(path, reasonixKey) }

func TestReasonixPlanOffKeepsExecutorAndSyncDoesNotWrite(t *testing.T) {
	a, _ := reasonixFixture(t, reasonixNativeConfig)
	for _, pair := range [][2]string{{"model", "uni-switch/a/pro"}, {"planner", "uni-switch/a/flash"}, {"planner", "off"}} {
		if err := a.Field(pair[0]).Set(pair[1]); err != nil {
			t.Fatal(err)
		}
	}
	if a.Field("planner").Get() != "" || a.Field("model").Get() != "uni-switch/a/pro" || a.Check() != "" {
		t.Fatal("Plan off changed Executor connection")
	}
	before, _ := os.ReadFile(a.Path)
	st, _ := os.Stat(a.Path)
	for i := 0; i < 10; i++ {
		if err := a.Sync(); err != nil {
			t.Fatal(err)
		}
	}
	after, _ := os.ReadFile(a.Path)
	next, _ := os.Stat(a.Path)
	if string(before) != string(after) || !st.ModTime().Equal(next.ModTime()) {
		t.Fatal("unchanged Sync rewrote config")
	}
	if err := a.Field("model").Set(""); err != nil {
		t.Fatal(err)
	}
	if a.Field("model").Get() != "native/old" {
		t.Fatal("Executor restore failed")
	}
}

func TestReasonixPlannerConflictRollsBack(t *testing.T) {
	a, env := reasonixFixture(t, reasonixNativeConfig)
	if err := a.Field("model").Set("uni-switch/a/pro"); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(a.Path)
	if err := os.WriteFile(env, []byte(reasonixKey+"=user-change\n"+reasonixKey+"=duplicate\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := a.Field("planner").Set("uni-switch/a/flash"); err == nil {
		t.Fatal("credential conflict accepted")
	}
	after, _ := os.ReadFile(a.Path)
	if string(before) != string(after) {
		t.Fatal("failed Plan selection changed config")
	}
}

func BenchmarkReasonixRolesSyncUnchanged(b *testing.B) {
	a, _ := reasonixFixture(b, reasonixNativeConfig)
	if err := a.Field("model").Set("uni-switch/a/pro"); err != nil {
		b.Fatal(err)
	}
	if err := a.Field("planner").Set("uni-switch/a/flash"); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := a.Sync(); err != nil {
			b.Fatal(err)
		}
	}
}

func TestReasonixPlanDoesNotResetExecutorEffort(t *testing.T) {
	a, _ := reasonixFixture(t, reasonixNativeConfig)
	if err := a.Field("model").Set("uni-switch/a/pro"); err != nil {
		t.Fatal(err)
	}
	if err := a.Field("effort").Set("high"); err != nil {
		t.Fatal(err)
	}
	if err := a.Field("planner").Set("uni-switch/a/flash"); err != nil {
		t.Fatal(err)
	}
	if got := a.Field("effort").Get(); got != "high" {
		t.Fatalf("Plan changed Executor effort: %s", got)
	}
}

func TestReasonixRestoreWithInitiallyAbsentEnv(t *testing.T) {
	a, env := reasonixFixture(t, reasonixNativeConfig)
	if err := os.Remove(env); err != nil {
		t.Fatal(err)
	}
	if err := a.Field("planner").Set("uni-switch/a/pro"); err != nil {
		t.Fatal(err)
	}
	if err := a.Field("planner").Set(""); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(env); !os.IsNotExist(err) {
		t.Fatal("adapter-created empty env was not removed")
	}
}

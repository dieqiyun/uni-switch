package agent

import (
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// What the Codex app holds is read, Codex switched off in uni-switch or not,
// while it decides where Codex sends (sherlock_23333 on X): Codex on
// uni-switch's models beside its ChatGPT sign-in, or uni-switch its provider for
// the held account. Not for a Codex kept on ChatGPT, on uni-switch's API, or
// not on uni-switch at all.
func TestCodexHoldMatters(t *testing.T) {
	home, _ := codexHome(t, `{"tokens":{"access_token":"x","id_token":"x.e30.x"}}`, "")
	t.Cleanup(func() { codexUsedUp = func() bool { return false } })
	cx := codex(home)
	if cx.HoldMatters() {
		t.Fatal("not on uni-switch: HoldMatters")
	}
	if err := cx.Fields[0].Set("fake/m1"); err != nil {
		t.Fatal(err)
	}
	if !cx.HoldMatters() {
		t.Fatal("beside the sign-in: not HoldMatters")
	}
	codexUsedUp = func() bool { return true }
	if err := cx.Sync(); err != nil {
		t.Fatal(err)
	}
	if cx.AppOut() || !cx.HoldMatters() {
		t.Fatalf("held, uni-switch its provider: AppOut %v, HoldMatters %v", cx.AppOut(), cx.HoldMatters())
	}
	// the hook the provider package asks
	if provider.CodexHoldMatters == nil {
		t.Fatal("provider.CodexHoldMatters not set")
	}
	for _, v := range []string{"chatgpt", "api"} {
		if err := cx.Field("login").Set(v); err != nil {
			t.Fatal(err)
		}
		if cx.HoldMatters() {
			t.Fatalf("login %s: HoldMatters", v)
		}
	}
}

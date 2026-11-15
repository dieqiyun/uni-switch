package agent

import (
	"strings"
	"testing"
)

// isymbo (#2267): Codex joined beside its ChatGPT sign-in, a uni-switch model
// picked, then uni-switch API chosen. The config had uni-switch as Codex's
// provider, but the joined mark stayed, so the Agents row still said
// Codex's own models stay in /model beside uni-switch's: the ChatGPT state
// the user had just left, which is the one the Codex app blocks sending
// in while the account is out. Joined is false while uni-switch is Codex's
// provider, and true again once it is back beside the sign-in.
func TestCodexProviderIsNotJoined(t *testing.T) {
	home, read := codexHome(t, `{"tokens":{"access_token":"x","id_token":"x.e30.x"}}`, `model = "gpt-5.5"`+"\n")
	cx := codex(home)
	if ok, err := cx.Join(); err != nil || !ok || !cx.Joined() {
		t.Fatalf("join: %v %v, joined %v", ok, err, cx.Joined())
	}
	if err := cx.Fields[0].Set("fake/m1"); err != nil {
		t.Fatal(err)
	}
	if !cx.Joined() || !cx.Beside() {
		t.Fatalf("a uni-switch model beside the sign-in: joined %v, beside %v", cx.Joined(), cx.Beside())
	}
	if err := cx.Field("login").Set("api"); err != nil {
		t.Fatal(err)
	}
	if cfg := read(); !strings.Contains(cfg, `model_provider = "uni-switch"`) {
		t.Fatalf("api:\n%s", cfg)
	}
	if cx.Joined() || cx.Beside() {
		t.Fatalf("uni-switch API: joined %v, beside %v", cx.Joined(), cx.Beside())
	}
	if !cx.Wired() {
		t.Fatal("uni-switch API: not wired")
	}

	// the ChatGPT account held by the Codex app: uni-switch its provider too
	if err := cx.Field("login").Set(""); err != nil {
		t.Fatal(err)
	}
	if !cx.Joined() {
		t.Fatal("back beside the sign-in: not joined")
	}
	codexUsedUp = func() bool { return true }
	t.Cleanup(func() { codexUsedUp = func() bool { return false } })
	if err := cx.Sync(); err != nil {
		t.Fatal(err)
	}
	if cfg := read(); !strings.Contains(cfg, `model_provider = "uni-switch"`) || cx.Joined() {
		t.Fatalf("held: joined %v\n%s", cx.Joined(), cfg)
	}
	codexUsedUp = func() bool { return false }
	if err := cx.Sync(); err != nil {
		t.Fatal(err)
	}
	if cfg := read(); strings.Contains(cfg, "model_provider =") || !cx.Joined() {
		t.Fatalf("room again: joined %v\n%s", cx.Joined(), cfg)
	}
}

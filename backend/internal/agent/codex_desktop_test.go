package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// codexAppReads has the Codex app (and a CLI) read every config when app
// is true, the CLI alone when it is false; put back when the test ends.
func codexAppReads(t *testing.T, app *bool) {
	t.Helper()
	was := codexReaders
	t.Cleanup(func() { codexReaders = was })
	codexReaders = func(place) []CodexReader {
		out := []CodexReader{{Kind: "cli", Version: "0.163.0"}}
		if *app {
			out = append(out, CodexReader{Kind: "app", Version: "0.163.0"})
		}
		return out
	}
}

// deskListIs has CodexDesktopList answer list, or not know it while list
// is nil.
func deskListIs(t *testing.T, list *string) {
	t.Helper()
	was := CodexDesktopList
	t.Cleanup(func() { CodexDesktopList = was })
	CodexDesktopList = func() ([]byte, bool) {
		if list == nil || *list == "" {
			return nil, false
		}
		return []byte(*list), true
	}
}

// laddieyao on Discord: Codex joined beside its ChatGPT sign-in, the Codex
// app's picker showed GPT alone while the CLI listed uni-switch's models. The
// app shows only the models of Codex's list a flag of OpenAI's names,
// unless Codex reads its list from a file: where the app is installed,
// uni-switch writes the list it hands Codex there and names it; the CLI alone
// keeps the list the gateway hands it.
func TestCodexDesktopListBesideSignIn(t *testing.T) {
	home, read := codexHome(t, `{"tokens":{"access_token":"x","id_token":"x.e30.x"}}`, `model = "gpt-5.5"`+"\n")
	dir := filepath.Join(home, ".codex")
	desk := filepath.Join(dir, "uni-switch-desktop-models.json")
	app := true
	codexAppReads(t, &app)
	list := `{"models":[{"slug":"gpt-6-sol","base_instructions":"theirs"},{"slug":"fake/m1"}]}`
	deskListIs(t, &list)

	cx := codex(home)
	if ok, err := cx.Join(); err != nil || !ok {
		t.Fatalf("join: %v %v", ok, err)
	}
	cfg := read()
	if !strings.Contains(cfg, `model_catalog_json = "`) || !strings.Contains(cfg, "uni-switch-desktop-models.json") {
		t.Fatalf("joined with the Codex app here, no list named for it:\n%s", cfg)
	}
	if strings.Contains(cfg, "model_provider =") {
		t.Fatalf("Codex's provider changed:\n%s", cfg)
	}
	if b, err := os.ReadFile(desk); err != nil || string(b) != list {
		t.Fatalf("the app's list: %q %v", b, err)
	}
	// connected beside the sign-in, as before: not uni-switch as its provider
	if !cx.Joined() || cx.AppOut() {
		t.Fatalf("joined %v, app out %v\n%s", cx.Joined(), cx.AppOut(), cfg)
	}
	if msg := cx.Check(); msg != "" {
		t.Fatalf("check: %s", msg)
	}

	// uni-switch's models changed: written again at the next sync
	list = `{"models":[{"slug":"gpt-6-sol","base_instructions":"theirs"},{"slug":"fake/m2"}]}`
	if err := cx.Sync(); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(desk); string(b) != list {
		t.Fatalf("not written again: %s", b)
	}

	// a list not known now (the backend not answering) is unknown: the
	// file as it was stays, never an empty one
	was := list
	list = ""
	if err := cx.Sync(); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(desk); string(b) != was || !strings.Contains(read(), "uni-switch-desktop-models.json") {
		t.Fatalf("unknown list: %s\n%s", b, read())
	}
	list = was

	// a uni-switch model picked beside the sign-in: still Codex's provider,
	// the list still the app's
	if err := cx.Fields[0].Set("fake/m1"); err != nil {
		t.Fatal(err)
	}
	if cfg := read(); !cx.Beside() || !strings.Contains(cfg, "uni-switch-desktop-models.json") || strings.Contains(cfg, "model_provider =") {
		t.Fatalf("beside: %v\n%s", cx.Beside(), cfg)
	}

	// uni-switch API: uni-switch's own catalog, as uni-switch is Codex's provider, and
	// the app's list gone
	if err := cx.Field("login").Set("api"); err != nil {
		t.Fatal(err)
	}
	if cfg := read(); !strings.Contains(cfg, `model_provider = "uni-switch"`) || strings.Contains(cfg, "uni-switch-desktop-models.json") {
		t.Fatalf("api:\n%s", cfg)
	}
	if _, err := os.Stat(desk); !os.IsNotExist(err) {
		t.Fatalf("api: the app's list is still there: %v", err)
	}
	if err := cx.Field("login").Set(""); err != nil {
		t.Fatal(err)
	}
	if cfg := read(); !strings.Contains(cfg, "uni-switch-desktop-models.json") || strings.Contains(cfg, "model_provider =") {
		t.Fatalf("back beside the sign-in:\n%s", cfg)
	}

	// disconnected: the key and the file go
	if err := cx.Unwire(); err != nil {
		t.Fatal(err)
	}
	if cfg := read(); strings.Contains(cfg, "model_catalog_json") {
		t.Fatalf("unwired:\n%s", cfg)
	}
	if _, err := os.Stat(desk); !os.IsNotExist(err) {
		t.Fatalf("unwired: the app's list is still there: %v", err)
	}
}

// The CLI alone asks the gateway for its list, which follows each change
// as it is made: no file is written for it, and one written while the
// Codex app was installed goes once it isn't.
func TestCodexDesktopListCLIOnly(t *testing.T) {
	home, read := codexHome(t, `{"tokens":{"access_token":"x","id_token":"x.e30.x"}}`, `model = "gpt-5.5"`+"\n")
	desk := filepath.Join(home, ".codex", "uni-switch-desktop-models.json")
	app := false
	codexAppReads(t, &app)
	list := `{"models":[{"slug":"gpt-6-sol"}]}`
	deskListIs(t, &list)
	cx := codex(home)
	if ok, err := cx.Join(); err != nil || !ok {
		t.Fatalf("join: %v %v", ok, err)
	}
	if cfg := read(); strings.Contains(cfg, "model_catalog_json") {
		t.Fatalf("CLI alone:\n%s", cfg)
	}
	if _, err := os.Stat(desk); !os.IsNotExist(err) {
		t.Fatalf("CLI alone: a list written: %v", err)
	}
	app = true
	if err := cx.Sync(); err != nil {
		t.Fatal(err)
	}
	if cfg := read(); !strings.Contains(cfg, "uni-switch-desktop-models.json") {
		t.Fatalf("the app installed:\n%s", cfg)
	}
	app = false
	if err := cx.Sync(); err != nil {
		t.Fatal(err)
	}
	if cfg := read(); strings.Contains(cfg, "model_catalog_json") {
		t.Fatalf("the app gone:\n%s", cfg)
	}
	if _, err := os.Stat(desk); !os.IsNotExist(err) {
		t.Fatalf("the app gone: the list stays: %v", err)
	}
}

// A model_catalog_json the user set while joined is theirs: it stands, and
// no list of uni-switch's is written beside it. And a key naming uni-switch's list
// with the file gone (removed by hand) goes, as Codex loads no config then,
// while the list isn't known to write it again.
func TestCodexDesktopListKeepsTheirs(t *testing.T) {
	home, read := codexHome(t, `{"tokens":{"access_token":"x","id_token":"x.e30.x"}}`, `model = "gpt-5.5"`+"\n")
	dir := filepath.Join(home, ".codex")
	desk := filepath.Join(dir, "uni-switch-desktop-models.json")
	app := true
	codexAppReads(t, &app)
	list := `{"models":[{"slug":"gpt-6-sol"}]}`
	deskListIs(t, &list)
	cx := codex(home)
	if ok, err := cx.Join(); err != nil || !ok {
		t.Fatalf("join: %v %v", ok, err)
	}
	os.Remove(desk)
	list = ""
	if err := cx.Sync(); err != nil {
		t.Fatal(err)
	}
	if cfg := read(); strings.Contains(cfg, "model_catalog_json") {
		t.Fatalf("named with the file gone:\n%s", cfg)
	}
	list = `{"models":[{"slug":"gpt-6-sol"}]}`
	cfg := strings.Replace(read(), `model = "gpt-5.5"`, `model = "gpt-5.5"`+"\n"+`model_catalog_json = "mine.json"`, 1)
	os.WriteFile(filepath.Join(dir, "mine.json"), []byte(`{"models":[]}`), 0o644)
	os.WriteFile(filepath.Join(dir, "config.toml"), []byte(cfg), 0o644)
	if err := cx.Sync(); err != nil {
		t.Fatal(err)
	}
	if cfg := read(); !strings.Contains(cfg, `model_catalog_json = "mine.json"`) {
		t.Fatalf("theirs:\n%s", cfg)
	}
	if _, err := os.Stat(desk); !os.IsNotExist(err) {
		t.Fatalf("written beside theirs: %v", err)
	}
}

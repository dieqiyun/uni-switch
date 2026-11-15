package agent

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/gateway"
)

// liby233 on Discord: deepseek-acp (dsh in an editor over ACP, CodeG)
// reads $DSH_HOME/settings.yaml and nothing else, and loads the pi-ai plugin
// only when that file names llm-pi-ai; with uni-switch's route in the profiles
// alone an editor listed DeepSeek's own models. A pick puts uni-switch's route
// there too, beside the user's own routes and sections, and never a start
// of uni-switch's (dsh 0.1 kept the user's pick there); Disconnect takes the
// route out and leaves the rest.
func TestDshACPSettingsGetTheRoute(t *testing.T) {
	home, dir, web := dshRouteHome(t)
	os.WriteFile(web, []byte("# Your patch layer for this dsh profile.\n"), 0o644)
	settings := filepath.Join(dir, "settings.yaml")
	read := func() string { b, _ := os.ReadFile(settings); return string(b) }
	a := dsh(home)

	if err := a.Pick("model", "uni-switch/deepseek/pro"); err != nil {
		t.Fatal(err)
	}
	s := read()
	for _, want := range []string{"llm-pi-ai:\n  providers:\n    uni-switch:\n", "displayName: uni-switch", "apiKeyEnv: " + dshKeyRef, "api: openai-completions", "baseURL: " + gatewayV1(), "- id: deepseek/pro"} {
		if !strings.Contains(s, want) {
			t.Fatalf("settings.yaml lacks %q:\n%s", want, s)
		}
	}
	if strings.Contains(s, "agent-default-model") {
		t.Fatalf("a start of uni-switch's in settings.yaml:\n%s", s)
	}
	if st, _ := os.Stat(settings); runtime.GOOS != "windows" && st.Mode().Perm() != 0o600 {
		t.Fatalf("settings.yaml made %v", st.Mode().Perm())
	}

	// the user's own: a route of theirs, a section of another plugin's, the
	// protocol they picked for uni-switch's route in dsh's Models page
	own := "# mine\nllm-deepseek:\n  thinking: enabled\nllm-pi-ai:\n  providers:\n    mine:\n      displayName: Mine\n      apiKeyEnv: MINE_API_KEY\n      api: openai-completions\n      baseURL: https://mine.example/v1\n      models:\n        - id: m1\n    uni-switch:\n      api: openai-responses\n"
	os.Remove(settings)
	os.WriteFile(settings, []byte(own), 0o644)
	if err := a.Sync(); err != nil {
		t.Fatal(err)
	}
	s = read()
	for _, want := range []string{"# mine\n", "llm-deepseek:\n  thinking: enabled\n", "    mine:\n", "baseURL: https://mine.example/v1", "    uni-switch:\n", "api: openai-responses", "- id: deepseek/flash"} {
		if !strings.Contains(s, want) {
			t.Fatalf("after the sync settings.yaml lacks %q:\n%s", want, s)
		}
	}
	if st, _ := os.Stat(settings); runtime.GOOS != "windows" && st.Mode().Perm() != 0o644 {
		t.Fatalf("the user's settings.yaml went %v", st.Mode().Perm())
	}

	if err := a.Disconnect(); err != nil {
		t.Fatal(err)
	}
	s = read()
	if strings.Contains(s, "uni-switch") || strings.Contains(s, dshKeyRef) {
		t.Fatalf("uni-switch's route stayed:\n%s", s)
	}
	for _, want := range []string{"# mine\n", "llm-deepseek:\n  thinking: enabled\n", "    mine:\n", "baseURL: https://mine.example/v1"} {
		if !strings.Contains(s, want) {
			t.Fatalf("Disconnect took %q:\n%s", want, s)
		}
	}

	// only uni-switch's there: the section goes with it
	if err := a.Pick("model", "uni-switch/deepseek/pro"); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(settings, []byte("llm-deepseek:\n  thinking: enabled\n"), 0o644)
	if err := a.Sync(); err != nil {
		t.Fatal(err)
	}
	if err := a.Disconnect(); err != nil {
		t.Fatal(err)
	}
	if s := read(); s != "llm-deepseek:\n  thinking: enabled\n" {
		t.Fatalf("after Disconnect:\n%s", s)
	}
}

// dsh 0.2 eats settings.yaml at each start: it renames it to
// settings.yaml.imported, over the one before, and imports it into the
// profile. The round uni-switch does while it serves writes the route there
// again, and an import of the user's own from before uni-switch's first is
// kept aside rather than lost under uni-switch's.
func TestDshACPSettingsWrittenAgain(t *testing.T) {
	_, dir, web := dshRouteHome(t)
	t.Setenv("DSH_HOME", dir)
	os.WriteFile(web, []byte("# Your patch layer for this dsh profile.\n"), 0o644)
	settings := filepath.Join(dir, "settings.yaml")
	imported := settings + ".imported"
	mine := "llm-deepseek:\n  thinking: enabled\nagent-default-model:\n  model: deepseek-v4-pro\n"
	os.WriteFile(imported, []byte(mine), 0o644)
	if err := dsh(filepath.Dir(dir)).Field("model").Set("uni-switch/deepseek/pro"); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(imported + ".kept"); string(b) != mine {
		t.Fatalf("the user's import wasn't kept: %q", b)
	}
	// dsh starts
	os.Rename(settings, imported)
	if trouble := dshWiredOnce(); trouble != "" {
		t.Fatal(trouble)
	}
	if b, _ := os.ReadFile(settings); !strings.Contains(string(b), "    uni-switch:\n") {
		t.Fatalf("the round didn't write settings.yaml again:\n%s", b)
	}
	// uni-switch's own import isn't kept over the user's
	os.Rename(settings, imported)
	os.Remove(imported + ".kept")
	if trouble := dshWiredOnce(); trouble != "" {
		t.Fatal(trouble)
	}
	if _, err := os.Stat(imported + ".kept"); err == nil {
		t.Fatal("uni-switch's own import was kept")
	}
}

// deepseek-acp and dsh refuse a key store any other user can read ("run
// chmod 600"), and dsh won't start then: one there already at 0644, the
// user's or an older uni-switch's, is made owner-only when uni-switch puts its key
// in, and when the key was there already.
func TestDshStoreMadeOwnerOnly(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("no such bits; dsh doesn't ask on Windows")
	}
	home, dir, web := dshRouteHome(t)
	os.WriteFile(web, []byte("# Your patch layer for this dsh profile.\n"), 0o644)
	creds := filepath.Join(dir, ".credentials.yaml")
	os.WriteFile(creds, []byte("version: 1\nrefs:\n  DEEPSEEK_API_KEY: sk-mine\n"), 0o644)
	a := dsh(home)
	if err := a.Field("model").Set("uni-switch/deepseek/pro"); err != nil {
		t.Fatal(err)
	}
	if st, _ := os.Stat(creds); st.Mode().Perm() != 0o600 {
		t.Fatalf("store left %v", st.Mode().Perm())
	}
	if b, _ := os.ReadFile(creds); !strings.Contains(string(b), "DEEPSEEK_API_KEY: sk-mine") {
		t.Fatalf("the user's key went:\n%s", b)
	}
	os.Chmod(creds, 0o644)
	if err := a.Sync(); err != nil {
		t.Fatal(err)
	}
	if st, _ := os.Stat(creds); st.Mode().Perm() != 0o600 {
		t.Fatalf("store with the key already left %v", st.Mode().Perm())
	}
}

// In a WSL distro a file written through \\wsl.localhost is 0644 whatever
// mode uni-switch asks for (tried on xiaomi-gpu), so the key store is chmodded
// from inside the distro, at the path the distro has it; settings.yaml
// gets the route at the gateway as the distro reaches it.
func TestWSLDshStoreOwnerOnly(t *testing.T) {
	home := syncHome(t)
	moveHostDirs(t)
	root := t.TempDir()
	dir := filepath.Join(root, "home", "me", ".dsh")
	web := filepath.Join(dir, "profiles", "web", "cordis.patch.yml")
	os.MkdirAll(filepath.Dir(web), 0o755)
	os.WriteFile(web, []byte("# Your patch layer for this dsh profile.\n[]\n"), 0o644)
	var ran []string
	run := wslRun
	wslRun = func(_ time.Duration, args ...string) ([]byte, error) {
		ran = append(ran, strings.Join(args, " "))
		return nil, nil
	}
	t.Cleanup(func() { wslRun = run })
	d := distro{Name: "Ubuntu", Root: root, Home: "/home/me", Has: map[string]bool{"bin:dsh": true},
		Gateway: "172.20.0.1", Running: true}
	a := wslAgent(wslKindOf("dsh"), d)
	if err := a.Field("model").Set("uni-switch/relay/glm-4.6"); err != nil {
		t.Fatal(err)
	}
	want := "-d Ubuntu -e chmod go-rwx /home/me/.dsh/.credentials.yaml"
	if !strings.Contains(strings.Join(ran, "\n"), want) {
		t.Fatalf("wsl.exe ran %q, not %q", ran, want)
	}
	gw := "http://172.20.0.1:" + gateway.Port() + "/v1"
	if s := readFile(filepath.Join(dir, "settings.yaml")); !strings.Contains(s, "baseURL: "+gw) || !strings.Contains(s, "- id: relay/glm-4.6") {
		t.Fatalf("settings.yaml lacks the route at %s:\n%s", gw, s)
	}
	if es, _ := os.ReadDir(filepath.Join(home, ".dsh")); len(es) > 0 {
		t.Fatal("this machine's ~/.dsh was touched")
	}
}

package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// `uni-switch snow a/m` puts Snow CLI on a uni-switch profile and makes it the
// active one; `uni-switch snow default` puts back the profile the user was on,
// its files as they were.
func TestAgentSnowDefault(t *testing.T) {
	groupsHome(t)
	t.Setenv("SNOW_CONFIG_DIR", "")
	dir := filepath.Join(os.Getenv("HOME"), ".snow")
	os.MkdirAll(filepath.Join(dir, "profiles"), 0o755)
	own := `{
  "snowcfg": {
    "baseUrl": "https://idealab.example.com/v1",
    "apiKey": "team-key",
    "requestMethod": "chat",
    "advancedModel": "glm-5",
    "basicModel": "glm-5"
  }
}`
	active := "{\n  \"activeProfile\": \"default\"\n}"
	files := map[string]string{
		"profiles/default.json": own,
		"config.json":           own,
		"active-profile.json":   active,
	}
	for f, s := range files {
		if err := os.WriteFile(filepath.Join(dir, f), []byte(s), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	read := func(f string) string {
		b, _ := os.ReadFile(filepath.Join(dir, f))
		return string(b)
	}
	if err := run([]string{"snow", "a/m"}); err != nil {
		t.Fatal(err)
	}
	s := read("profiles/uni-switch.json")
	for _, want := range []string{`"advancedModel": "a/m"`, `"baseUrlMode": "base"`, `"requestMethod": "chat"`, `/v1"`} {
		if !strings.Contains(s, want) {
			t.Fatalf("not wired (%s missing):\n%s", want, s)
		}
	}
	if !strings.Contains(read("active-profile.json"), `"uni-switch"`) || read("config.json") != s {
		t.Fatalf("uni-switch's profile isn't the active one:\n%s\n%s", read("active-profile.json"), read("config.json"))
	}
	if err := run([]string{"snow", "default"}); err != nil {
		t.Fatal(err)
	}
	for f, want := range files {
		if got := read(f); got != want {
			t.Errorf("%s not put back:\n%s", f, got)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "profiles", "uni-switch.json")); !os.IsNotExist(err) {
		t.Error("uni-switch.json left")
	}
}

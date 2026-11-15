package agent

import (
	"reflect"
	"strings"
	"testing"

	"github.com/pelletier/go-toml/v2"
)

// #253: a config.toml with a top-level array over several lines. Picking a
// model through uni-switch put the new top-level keys inside the array, and the
// failed write left the provider table appended but no model.
const codexNotify = `notify = [
    "/usr/local/bin/notifier",
    "turn-ended",
]

[model_providers.example]
base_url = "http://127.0.0.1:1/v1"
`

func TestCodexMultilineArray(t *testing.T) {
	for _, tc := range []struct{ name, auth, config string }{
		{"signed in", `{"tokens":{"access_token":"x","id_token":"x.e30.x"}}`, codexNotify},
		{"signed in, on a provider of its own", `{"tokens":{"access_token":"x","id_token":"x.e30.x"}}`,
			"model = \"gpt-5.5\"\nmodel_provider = \"example\"\n" + codexNotify},
		{"as a provider", "", codexNotify},
	} {
		t.Run(tc.name, func(t *testing.T) {
			home, read := codexHome(t, tc.auth, tc.config)
			cx := codex(home)
			if err := cx.Fields[0].Set("fake/m1"); err != nil {
				t.Fatal(err)
			}
			var cfg struct {
				Model          string
				Notify         []string
				ModelProviders map[string]any `toml:"model_providers"`
			}
			if err := toml.Unmarshal([]byte(read()), &cfg); err != nil {
				t.Fatalf("%v\n%s", err, read())
			}
			if want := []string{"/usr/local/bin/notifier", "turn-ended"}; cfg.Model != "fake/m1" || !reflect.DeepEqual(cfg.Notify, want) ||
				cfg.ModelProviders["uni-switch"] == nil || cfg.ModelProviders["example"] == nil {
				t.Fatalf("after the pick:\n%s", read())
			}
			if !strings.Contains(read(), "notify = [\n    \"/usr/local/bin/notifier\",\n    \"turn-ended\",\n]\n") {
				t.Fatalf("the array was rewritten:\n%s", read())
			}
			// and back to one of Codex's own
			if err := cx.Fields[0].Set("gpt-5.4"); err != nil {
				t.Fatal(err)
			}
			if got := cx.Fields[0].Get(); got != "gpt-5.4" {
				t.Fatalf("model = %q\n%s", got, read())
			}
			if !strings.Contains(read(), "notify = [\n    \"/usr/local/bin/notifier\",\n    \"turn-ended\",\n]\n") {
				t.Fatalf("the array was rewritten:\n%s", read())
			}
		})
	}
}

// A pick whose last step can't be written leaves config.toml byte for byte
// as it was, rather than with uni-switch's provider table added and no model:
// here a table named openai_base_url makes the key of that name a
// conflicting definition, found only once the earlier steps are written.
func TestCodexFailedPickLeavesConfig(t *testing.T) {
	const config = "model = \"gpt-5.5\" # mine\nmodel_provider = \"example\"\n\n[openai_base_url]\nx = 1\n\n[model_providers.example]\nbase_url = \"http://127.0.0.1:1/v1\"\n"
	home, read := codexHome(t, `{"tokens":{"access_token":"x","id_token":"x.e30.x"}}`, config)
	err := codex(home).Fields[0].Set("fake/m1")
	if err == nil || !strings.Contains(err.Error(), "edited TOML is invalid") {
		t.Fatalf("err = %v\n%s", err, read())
	}
	if got := read(); got != config {
		t.Fatalf("config.toml left half-edited:\n%s", got)
	}
}

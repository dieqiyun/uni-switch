package agent

import (
	"encoding/json"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/catalog"
	"github.com/dieqiyun/uni-switch/backend/internal/edit"
	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// A model that thinks is written with a reasoning_effort, which is what
// makes Zed offer its thinking switch and levels; one that doesn't is
// written without, and a value the user set on a model is kept (#964).
func TestZedReasoningEffort(t *testing.T) {
	home := syncHome(t)
	credential := zedCredential
	t.Cleanup(func() { zedCredential = credential; catalog.Reset() })
	zedCredential = func(string) error { return nil }
	writeFile(t, catalog.CachePath(), `{"openai":{"models":{
		"gpt-5":{"id":"gpt-5","reasoning":true,"reasoning_options":[{"type":"effort","values":["low","medium","high","xhigh"]}]},
		"glm-5.3":{"id":"glm-5.3","reasoning":true,"reasoning_options":[{"type":"effort","values":["low","max"]}]},
		"tiny":{"id":"tiny","reasoning":true,"reasoning_options":[{"type":"effort","values":["minimal","low"]}]},
		"flash":{"id":"flash","reasoning":true,"reasoning_options":[{"type":"toggle"}]},
		"plain":{"id":"plain","name":"Plain"}}}}`)
	catalog.Reset()
	if err := provider.Save(provider.Provider{ID: "relay", Name: "Relay", Key: "k", Chat: "http://127.0.0.1:1/v1", Models: []string{"gpt-5", "glm-5.3", "tiny", "flash", "plain"}}); err != nil {
		t.Fatal(err)
	}
	a := zedAt(filepath.Join(home, "zed"))
	if err := a.Field("model").Set("uni-switch/relay/gpt-5"); err != nil {
		t.Fatal(err)
	}
	// efforts is each model's reasoning_effort by name, "-" for none
	// written, and where it is in the list
	efforts := func() (map[string]string, map[string]int) {
		t.Helper()
		raw, _ := edit.GetJSON(a.Path, zedProvider+".available_models")
		var list []struct {
			Name   string  `json:"name"`
			Effort *string `json:"reasoning_effort"`
		}
		if err := json.Unmarshal([]byte(raw), &list); err != nil {
			t.Fatal(err)
		}
		got, at := map[string]string{}, map[string]int{}
		for i, m := range list {
			got[m.Name], at[m.Name] = "-", i
			if m.Effort != nil {
				got[m.Name] = *m.Effort
			}
		}
		return got, at
	}
	check := func(want map[string]string) {
		t.Helper()
		got, _ := efforts()
		for n, w := range want {
			if got[n] != w {
				t.Errorf("%s: reasoning_effort = %q, want %q (all %v)", n, got[n], w, got)
			}
		}
	}
	check(map[string]string{
		"relay/gpt-5":   "high",
		"relay/glm-5.3": "high", // max is no level every Zed takes: the gateway fits high to it
		"relay/tiny":    "low",
		"relay/flash":   "high", // a switch alone: the gateway fits the level
		"relay/plain":   "-",    // doesn't think: no switch in Zed
	})

	// the user picks another level on one and turns another's thinking off
	_, at := efforts()
	for n, v := range map[string]string{"relay/gpt-5": "medium", "relay/flash": "none"} {
		if err := edit.SetJSON(a.Path, edit.KV{Path: zedProvider + ".available_models." + strconv.Itoa(at[n]) + ".reasoning_effort", Value: v}); err != nil {
			t.Fatal(err)
		}
	}
	// and a change in uni-switch has the models written again
	if err := provider.SetModelOutput("relay/gpt-5", 64000); err != nil {
		t.Fatal(err)
	}
	if err := a.Sync(); err != nil {
		t.Fatal(err)
	}
	if got, _ := edit.GetJSON(a.Path, zedProvider+".available_models."+strconv.Itoa(at["relay/gpt-5"])+".max_output_tokens"); got != "64000" {
		t.Fatalf("sync didn't write the models again: %s", readFile(a.Path))
	}
	want := map[string]string{"relay/gpt-5": "medium", "relay/flash": "none", "relay/glm-5.3": "high", "relay/plain": "-"}
	check(want)
	if err := a.Field("model").Set("uni-switch/relay/flash"); err != nil {
		t.Fatal(err)
	}
	check(want)
}

package catalog

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

// jiabubu on Discord: Kimi Code's own provider has its plan's models
// always think and load tools mid-conversation, from what
// api.kimi.com/coding/v1/models says of each (supports_thinking_type
// "only", supports_dynamic_tools, think_efforts' default_effort). The list
// is read as Kimi's own reply of 2026-10-31 sends it, and its nested forms
// (reasoning.type, tools.dynamic_load, reasoning.effort.default) alone say
// the same.
func TestFetchKimiCodeFacts(t *testing.T) {
	list, err := os.ReadFile("testdata/kimi-code-cn-models-2026-10-31.json")
	if err != nil {
		t.Fatal(err)
	}
	nested := `{"data":[{"id":"nested","reasoning":{"type":"only","effort":{"valid":["low","high"],"default":"low"}},"tools":{"dynamic_load":true}},
	  {"id":"both","supports_thinking_type":"both","supports_dynamic_tools":false,"reasoning":{"type":"both"},"tools":{"dynamic_load":false}},
	  {"id":"odd","supports_thinking_type":7,"supports_dynamic_tools":"yes","think_efforts":"max","reasoning":"only","tools":[1]},
	  {"id":"glm-5","context_length":200000}]}`
	for _, c := range []struct {
		body string
		want map[string]Model
	}{
		{string(list), map[string]Model{
			"kimi-for-coding":           {AlwaysThinks: true, DynamicTools: true, DefaultEffort: "max"},
			"kimi-for-coding-highspeed": {AlwaysThinks: true},
			"k3":                        {AlwaysThinks: true, DynamicTools: true, DefaultEffort: "high"},
			"k3-256k":                   {AlwaysThinks: true, DynamicTools: true, DefaultEffort: "high"},
			"k3.1-pro":                  {AlwaysThinks: true, DynamicTools: true, DefaultEffort: "high"},
			"k3.1-pro-highspeed":        {AlwaysThinks: true, DynamicTools: true, DefaultEffort: "high"},
		}},
		{nested, map[string]Model{
			"nested": {AlwaysThinks: true, DynamicTools: true, DefaultEffort: "low"},
			"both":   {},
			"odd":    {},
			"glm-5":  {},
		}},
	} {
		srv := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
			rw.Write([]byte(c.body))
		}))
		ms, _, err := FetchAt(context.Background(), srv.URL+"/coding/v1", "k", false, nil)
		srv.Close()
		if err != nil {
			t.Fatal(err)
		}
		if len(ms) != len(c.want) {
			t.Fatalf("%d models: %+v", len(ms), ms)
		}
		for _, m := range ms {
			w, ok := c.want[m.ID]
			if !ok {
				t.Fatalf("%s not listed", m.ID)
			}
			if m.AlwaysThinks != w.AlwaysThinks || m.DynamicTools != w.DynamicTools || m.DefaultEffort != w.DefaultEffort {
				t.Errorf("%s: always %v, dynamic tools %v, default %q; want %v %v %q", m.ID, m.AlwaysThinks, m.DynamicTools, m.DefaultEffort, w.AlwaysThinks, w.DynamicTools, w.DefaultEffort)
			}
		}
	}
}

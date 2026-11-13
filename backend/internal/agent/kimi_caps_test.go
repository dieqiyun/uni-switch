package agent

import (
	"testing"
)

// A Kimi Code config written by a uni-switch before this one has high on K2.8
// with no record that uni-switch wrote it: it becomes the vendor's max. One
// the user picked after a record was kept stays (TestKimiCodePlanCapabilities).
func TestKimiCodeOldHighBecomesTheVendorsDefault(t *testing.T) {
	kvs, def := kimiEfforts([]string{"none", "low", "high", "max"}, "max", "high", map[string]string{"display_name": "K2.8 Preview"})
	if def != "max" || kvs[len(kvs)-1].Value != "max" {
		t.Errorf("unrecorded high: %v, %q", kvs, def)
	}
	kvs, def = kimiEfforts([]string{"none", "low", "high", "max"}, "max", "high", map[string]string{"default_effort": "max"})
	if def != "" || kvs[len(kvs)-1].Value != "high" {
		t.Errorf("the user's high: %v, %q", kvs, def)
	}
	kvs, def = kimiEfforts([]string{"low", "high"}, "max", "", nil)
	if def != "high" || kvs[len(kvs)-1].Value != "high" {
		t.Errorf("a default the model hasn't: %v, %q", kvs, def)
	}
}

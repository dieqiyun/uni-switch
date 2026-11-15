package provider

import "testing"

func TestUniSwitchRemotePresetRemoved(t *testing.T) {
	for _, p := range Presets() {
		if p.Kind == "remote" {
			t.Fatalf("remote gateway remains addable: %s", p.ID)
		}
	}
	for _, id := range []string{"remote-uni-switch", "remote-magpie"} {
		if Preset(id) != nil {
			t.Errorf("retired remote gateway preset remains: %s", id)
		}
		if _, err := FromPreset(id); err == nil {
			t.Errorf("retired remote gateway import accepted: %s", id)
		}
	}
}

func TestUniSwitchLegacyRemoteProvidersStayDormant(t *testing.T) {
	detectHome(t)
	for _, preset := range []string{"remote-uni-switch", "remote-magpie"} {
		p := Provider{ID: "office", Name: "Office", Preset: preset, Chat: "http://192.168.1.20:3425/v1", Key: "synthetic-key"}
		own, picks := ownOf(file{Providers: []Provider{p}})
		if len(own) != 0 || len(picks) != 0 {
			t.Errorf("legacy remote provider activated: %s", preset)
		}
		if err := Save(p); err == nil {
			t.Errorf("retired remote provider can still be saved: %s", preset)
		}
	}
	// Explicit provider endpoints are still valid outbound connections.
	custom := Provider{ID: "custom", Name: "Custom", Chat: "http://192.168.1.20:8000/v1", Key: "synthetic-key"}
	if err := Save(custom); err != nil {
		t.Fatalf("custom endpoint rejected: %v", err)
	}
}

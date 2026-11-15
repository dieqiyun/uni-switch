package settingsapi

import (
	"encoding/json"
	"github.com/dieqiyun/uni-switch/backend/internal/settings"
	"reflect"
	"testing"
)

func TestAccountAutomationSurvivesGeneralSettingsSave(t *testing.T) {
	home(t)
	s := settings.Load()
	s.AccountWarmups = map[string]settings.AccountWarmup{"codex|a@example.com": {Reset: "week", Times: []string{"09:00", "15:00"}}, "claude|b@example.com": {}}
	s.AccountCheckins = map[string]bool{"workbuddy|a": true, "workbuddy|b": false}
	if err := settings.Save(s); err != nil {
		t.Fatal(err)
	}
	in, _, err := Apply([]byte("{\"theme\":\"dark\"}"), false)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(in.AccountWarmups, s.AccountWarmups) || !reflect.DeepEqual(in.AccountCheckins, s.AccountCheckins) {
		t.Fatal("general settings overwrote account automation")
	}
	raw, _ := json.Marshal(settings.Load())
	var fields map[string]any
	json.Unmarshal(raw, &fields)
	if _, ok := fields["classifyTimeout"]; ok || Known("classifyTimeout") {
		t.Fatal("retired timeout remains configurable")
	}
	s.AccountWarmups["claude|b@example.com"] = settings.AccountWarmup{Times: []string{"25:00"}}
	if settings.Save(s) == nil {
		t.Fatal("invalid time accepted")
	}
}

package provider

import (
	"github.com/dieqiyun/uni-switch/backend/internal/settings"
	"strings"
)

func accountWarmups(agent string) map[string]settings.AccountWarmup {
	out := map[string]settings.AccountWarmup{}
	for key, options := range settings.Load().AccountWarmups {
		if user, ok := strings.CutPrefix(key, agent+"|"); ok {
			out[user] = options
		}
	}
	return out
}

func dailyCheckinOn(s settings.Settings, by string) bool {
	switch by {
	case "", "workbuddy":
		return s.WorkBuddyCheckin
	case "trae":
		return s.TraeCheckin
	case "minimax":
		return s.MiniMaxCheckin
	case "qoder":
		return s.QoderCheckin
	}
	return false
}

func accountCheckinEnabled(label string) bool {
	prefixes := map[string][]string{
		"workbuddy": {"workbuddy|"}, "trae": {TraeCNID + "|"}, "minimax": {MiniMaxCodeID + "|", MiniMaxCodeGlobalID + "|"}, "qoder": {"qoder|", QoderCNID + "|"},
	}[label]
	for key, on := range settings.Load().AccountCheckins {
		if on {
			for _, prefix := range prefixes {
				if strings.HasPrefix(key, prefix) {
					return true
				}
			}
		}
	}
	return false
}

func (c codexWarmer) options() map[string]settings.AccountWarmup {
	if c.accountPrefs == nil {
		return nil
	}
	return c.accountPrefs()
}

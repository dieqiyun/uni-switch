package gateway

import (
	"time"
)

// factoryCore is the Droid Core models Factory's plugin gives its Core
// windows (packages/factory/index.mjs CORE, 0.1.38).
var factoryCore = []string{"glm-5.3", "glm-5.3-flash", "glm-5.2", "kimi-k3", "deepseek-v4.1-flash", "qwen3.8-max", "minimax-m3", "minimax-m2.7", "mistral-large-4", "mistral-medium-3.5", "nemotron-3-ultra"}

// standardOnlyWindows are an account's windows as Factory's plugin 0.1.38
// reports them with its Standard usage only option on (limitWindows(l,
// true), plugins#349): Standard's 5 hours and 7 days count every model,
// Droid Core's are aside (shown only), and so is the extra usage.
func standardOnlyWindows(five, week float64, fiveReset, weekReset time.Time, core float64) []map[string]any {
	return []map[string]any{
		{"name": "5 hours", "used": five, "span": 18000, "resetsAt": fiveReset.UTC().Format(time.RFC3339)},
		{"name": "7 days", "used": week, "span": 604800, "resetsAt": weekReset.UTC().Format(time.RFC3339)},
		{"name": "Droid Core · 5 hours", "used": core, "span": 18000, "resetsAt": fiveReset.UTC().Format(time.RFC3339), "aside": true, "models": factoryCore},
		{"name": "Droid Core · 7 days", "used": core, "span": 604800, "resetsAt": weekReset.UTC().Format(time.RFC3339), "aside": true, "models": factoryCore},
		{"name": "Extra usage", "used": 0, "display": "$12.50", "aside": true},
	}
}

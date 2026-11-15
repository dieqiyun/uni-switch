package provider

import (
	"cmp"
	"slices"
	"strings"

	"github.com/dieqiyun/uni-switch/backend/internal/catalog"
)

// peakBill is a vendor that bills DeepSeek's models by the hour a call is
// made at: its list price is the off-peak one, doubled in the peak hours
// (catalog.Peak). Each one's own pricing page says so, and so does its
// models.dev entry's cost multiplier, which models.dev's api.json leaves
// out. Others that resell DeepSeek's models keep one price at every hour:
// OpenCode Zen lists deepseek-v4.1-flash at $0.30/$1.20 flat
// (https://opencode.ai/docs/zen); a relay's price is its own.
type peakBill struct {
	// at is whether a base URL is the vendor's API
	at func(base string) bool
	// ids are its preset's id and its subscription's: how a provider with
	// no URL of its own (a plugin's, one removed since) is told
	ids  []string
	peak catalog.Peak
	// models are the ids it bills so, without a maker's prefix
	// ("deepseek/"); nil: every model it serves
	models []string
	// tagged: a listed model with a tag after a colon is the same model
	// (Ollama's dated deepseek-v4.1-flash:1017 beside deepseek-v4.1-flash)
	tagged bool
}

var peakBills = []peakBill{
	// DeepSeek's own API, every model: "Off-peak rates are half of the
	// peak rates" (https://api-docs.deepseek.com/quick_start/pricing)
	{at: func(b string) bool { return HostOf(b) == "api.deepseek.com" }, ids: []string{"deepseek"}, peak: catalog.PeakDeepSeek},
	// Command Code's DeepSeek V4 models, its Provider API's and its plan's
	// (https://commandcode.ai/docs/resources/pricing-limits): its
	// deepseek-v4-flash-fast is one price at every hour
	{at: func(b string) bool { return HostOf(b) == "api.commandcode.ai" }, ids: []string{"commandcode", CommandCodePlanID}, peak: catalog.PeakWeekdays,
		models: []string{"deepseek-v4.1-flash", "deepseek-v4.1-flash-fast", "deepseek-v4-flash", "deepseek-v4-flash-vision-exp", "deepseek-v4-pro"}},
	// OpenCode Go's (https://opencode.ai/docs/go): Off-Peak and Peak rows
	{at: func(b string) bool { return openCodeGateway(b) == "go" }, ids: []string{"opencode-go"}, peak: catalog.PeakWeekdays,
		models: []string{"deepseek-v4.1-flash", "deepseek-v4-pro"}},
	// Ollama Cloud's (https://ollama.com/pricing): the two models with an
	// Off-Peak row, at half the price 12:00 to 18:00 UTC on weekdays,
	// which models.dev lists as their price; every other model there is
	// one price at every hour
	{at: func(b string) bool { return HostOf(b) == "ollama.com" }, ids: []string{"ollama-cloud"}, peak: catalog.PeakOllama,
		models: []string{"deepseek-v4.1-flash", "deepseek-v4-pro"}, tagged: true},
}

// PeakOf is the hours a call to model at p is billed double in, by the
// vendor p sends it to: its base URLs say which, and only a provider with
// none is told by its preset or id. A plugin's base (plugin://) names no
// vendor, so the Command Code plan its plugin serves is told by its id
// (treeman on Discord: its DeepSeek models showed no peak hours). Empty
// for any other provider or model.
func (p Provider) PeakOf(model string) catalog.Peak {
	m := strings.ToLower(strings.TrimSpace(model))
	m = m[strings.LastIndexByte(m, '/')+1:]
	// the model first: the Providers page asks this of every model of
	// every list, and few are billed by the hour, while telling a vendor by
	// its URLs parses each of them (HostOf)
	var buf [3]string
	var urls []string
	looked := false
	for _, b := range peakBills {
		if b.models != nil && !slices.Contains(b.models, m) {
			if i := strings.IndexByte(m, ':'); !b.tagged || i < 0 || !slices.Contains(b.models, m[:i]) {
				continue
			}
		}
		if !looked {
			urls = buf[:0]
			for _, u := range [...]string{p.Chat, p.Responses, p.Anthropic} {
				if strings.TrimSpace(u) != "" && !strings.HasPrefix(u, pluginBase) {
					urls = append(urls, u)
				}
			}
			looked = true
		}
		var is bool
		if len(urls) > 0 {
			is = slices.ContainsFunc(urls, b.at)
		} else {
			is = slices.Contains(b.ids, cmp.Or(p.Preset, p.ID))
		}
		if is {
			return b.peak
		}
	}
	return ""
}

package gui

import (
	"strings"
	"testing"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// yi_time on Discord: uni-switch set to Chinese on Windows, and the tray icon's
// tooltip said "Today: 121K tokens (2.9K in · 180 out · 118K cached)" and
// "3 requests" in English. The tooltip is in uni-switch's language, as the
// tray menu is: the idle tip, uni-switch's totals and a subscription's
// windows, in every language uni-switch has. A window's name and the numbers
// are the card's own.
func TestTrayTooltipFollowsUniSwitchsLanguage(t *testing.T) {
	now := time.Now()
	asOf := now.Add(-time.Hour)
	card := provider.SubscriptionQuota{Provider: "claude", Name: "Claude", AsOf: &asOf,
		Windows: []provider.QuotaWindow{{Name: "5h", Used: 42, ResetSecs: 3*3600 + 10*60}}}
	_, tip := trayCardText("zh", card, now, false, false, nil)
	for _, w := range []string{"Claude\n", "5h 已用 42%", "3 小时 10 分 后重置", "截至 ", "暂时无法获取最新额度"} {
		if !strings.Contains(tip, w) {
			t.Errorf("zh card tooltip %q lacks %q", tip, w)
		}
	}
	if _, tip := trayCardText("zh", card, now, true, false, nil); !strings.Contains(tip, "5h 剩余 58%") {
		t.Errorf("zh card tooltip, what is left: %q", tip)
	}

	english := []string{"Today", "tokens", "K in", "0 in", " out", "cached", "request", " used", " left", "resets", "pace ", "as of", "Last 7", "no known price", "couldn't", "Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov", "Dec"}
	for _, lang := range []string{"zh", "zh-TW", "ja", "de"} {
		readAt := now.Add(-2 * time.Hour)
		paced := card
		paced.ReadAt = &readAt
		paced.Windows = []provider.QuotaWindow{{Name: "5 hours", Used: 42.5, ResetSecs: 3*3600 + 10*60}, {Name: "7 days", Used: 3, ResetSecs: 4*86400 + 3600}, {Name: "x", Used: 1, ResetSecs: 120}}
		var tips []string
		for _, left := range []bool{false, true} {
			_, tip := trayCardText(lang, paced, now, left, true, nil)
			if pace := trayText(lang, "pace %s", ""); !strings.Contains(tip, pace) {
				t.Errorf("%s: no %q in %q", lang, pace, tip)
			}
			tips = append(tips, tip)
		}
		_, _, view := trayCardsView(lang, []provider.SubscriptionQuota{paced}, now, false, true, nil)
		tips = append(tips, view)
		for _, tip := range tips {
			for _, w := range english {
				if strings.Contains(tip, w) {
					t.Errorf("%s tooltip says %q in English:\n%s", lang, w, tip)
				}
			}
		}
	}
}

package catalog

import "time"

// Peak names the hours a list price is the off-peak price of: a call made
// in them is billed at PeakFactor times it. DeepSeek bills its models at
// half price off-peak, and models.dev lists that half as the price, with a
// multiplier for the peak hours its api.json leaves out
// (providers/deepseek/models/deepseek-flash.toml: "Base prices below are
// off-peak; the multiplier doubles them at peak"). Empty: the price holds
// at every hour.
type Peak string

const (
	// PeakDeepSeek is DeepSeek's own: 01:00 to 04:00 and 06:00 to 10:00
	// UTC, Monday to Friday, never on a Chinese public holiday
	// (https://api-docs.deepseek.com/quick_start/pricing)
	PeakDeepSeek Peak = "deepseek"
	// PeakWeekdays is the same hours on every weekday, holidays too: what
	// Command Code (https://commandcode.ai/docs/resources/pricing-limits)
	// and OpenCode Go (https://opencode.ai/docs/go) bill DeepSeek's models
	// at, neither of which names a holiday
	PeakWeekdays Peak = "weekdays"
	// PeakOllama is Ollama Cloud's: 12:00 to 18:00 UTC, Monday to Friday
	// (https://ollama.com/pricing: off-peak outside 12:00 and 18:00 UTC on
	// weekdays, and all of Saturday and Sunday), where its
	// off-peak rows are half its peak ones and models.dev lists the
	// off-peak row as the price. It names no holiday.
	PeakOllama Peak = "ollama"
)

// PeakFactor is what a call in the peak hours costs, times the off-peak
// list price: DeepSeek's off-peak rates are half its peak rates, and so
// are Ollama Cloud's.
const PeakFactor = 2

// At is whether a call made at t is in the peak hours. A zero time, a
// call whose time isn't known, is not.
func (k Peak) At(t time.Time) bool {
	if k == "" || t.IsZero() {
		return false
	}
	u := t.UTC()
	if wd := u.Weekday(); wd == time.Saturday || wd == time.Sunday {
		return false
	}
	h := u.Hour()
	if k == PeakOllama {
		return h >= 12 && h < 18
	}
	if !(h >= 1 && h < 4 || h >= 6 && h < 10) {
		return false
	}
	// the peak hours are 09:00 to 18:00 in China, so a holiday's date is
	// the same in UTC
	return k != PeakDeepSeek || !cnHoliday(u)
}

// In is the price a call made at t is billed at: PeakFactor times this one
// in its peak hours, else this one. The price it gives holds at every hour.
func (p Price) In(t time.Time) Price {
	if !p.Peak.At(t) {
		p.Peak = ""
		return p
	}
	p = p.Times(PeakFactor)
	p.Peak = ""
	return p
}

// cnHolidays are the days off of China's public holidays, as the State
// Council's notice for each year lists them (a weekend day inside a break
// is a day off already, and a weekend day worked in exchange stays a
// weekend day for DeepSeek, whose peak is Monday to Friday). It needs a
// year added each November, when the notice for the next year comes out:
// TestCNHolidaysCoverThisYear fails in a year it doesn't have. A year it
// doesn't have counts no holiday, so its weekdays are all at peak hours'
// price, the most a call could have cost.
var cnHolidays = map[int][]string{
	// 国务院办公厅关于2026年部分节假日安排的通知, 2025-11-04
	// (https://www.news.cn/politics/20251104/88bcffd88ae249e58c699b8772548e3d/c.html)
	2026: {
		"01-01", "01-02", "01-03", // 元旦
		"02-15", "02-16", "02-17", "02-18", "02-19", "02-20", "02-21", "02-22", "02-23", // 春节
		"04-04", "04-05", "04-06", // 清明节
		"05-01", "05-02", "05-03", "05-04", "05-05", // 劳动节
		"06-19", "06-20", "06-21", // 端午节
		"09-25", "09-26", "09-27", // 中秋节
		"10-01", "10-02", "10-03", "10-04", "10-05", "10-06", "10-07", // 国庆节
	},
}

// cnHoliday is whether t's date, in China, is a day off of a public holiday.
func cnHoliday(t time.Time) bool {
	d := t.In(chinaTime).Format("01-02")
	for _, h := range cnHolidays[t.In(chinaTime).Year()] {
		if h == d {
			return true
		}
	}
	return false
}

// chinaTime is China's time, UTC+8 all year: a fixed zone, so it doesn't
// depend on the system's zone database (Windows has none Go can read).
var chinaTime = time.FixedZone("CST", 8*60*60)

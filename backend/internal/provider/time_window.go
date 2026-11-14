package provider

// A group's rules: which member a request goes to first, by what can be
// seen in the request itself — how long it is, whether it carries an
// image, how hard the agent asked the model to think, which agent sent it.
// Those are never guessed: the same request is always routed the same way,
// and the trace tells which rule did it. A rule may also hold only at some
// hours of the day (a vendor's peak-price hours, say), in the time zone
// they were set in (uni-switch's clock when they name none). The one exception is a rule with
// an intent (what the user asks for, in the user's words): as a turn
// begins, a model the group names (its Classifier) is asked which of the
// intents the user's message is, once, and the trace tells what it said.
//
// A rule decides when a user's turn begins; the agent's requests within
// that turn — tool results sent back — stay with what it decided, so a
// turn is never moved to another model halfway through (the gateway keeps
// that; see gateway/rules.go). The member a rule names goes first, then
// the others it names after it (Rule.More), in order; the group's other
// members stay behind them for failover.
//
// A pause rule (Rule.Pause) sends nothing first: while it holds, the
// member it names is out of the group altogether — not first, not a
// failover, not where a conversation was — as a member switched off is
// (Group.Off), but only at its hours, on its days, for its agents or in its
// folders (or outside them: #1606, an account's quota kept for the long
// task in one project). It is
// looked at on every request, not as a turn begins: a paused model is
// paid nothing in its hours, so a turn on it moves when they begin.

import (
	"fmt"
	"os"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Efforts are the reasoning levels a rule can ask for, lowest first.
var Efforts = []string{"low", "medium", "high", "xhigh", "max"}

// TimeWindow is hours of the day: From until To, "HH:MM" each, past
// midnight when To comes before From (22:00–08:00), the whole day when
// they are the same. Days, when set, are the days it holds on ("mon" …
// "sun"); a window past midnight is of the day it begins on, so Friday's
// 22:00–08:00 takes Saturday's early hours too.
type TimeWindow struct {
	From string   `json:"from"`
	To   string   `json:"to"`
	Days []string `json:"days,omitempty"`
	// Zone: the IANA time zone the hours and days were set in
	// ("Asia/Shanghai"): the browser's on the Routing page, the terminal's
	// for uni-switch group rule. uni-switch on a server in UTC keeps a Beijing
	// user's 09:00–18:00 at Beijing's 09:00 (gaubeebangeel on X). A window
	// with none — every one set before zones were kept — is on this
	// computer's clock, as it was.
	Zone string `json:"zone,omitempty"`
}

// zones are the zones windows were read in, each loaded once: Holds is
// asked on every request a pause rule may hold for.
var zones sync.Map // name → *time.Location

// zoneOf is the window's zone: time.Local when it names none; false when
// it names one this uni-switch doesn't know.
func zoneOf(name string) (*time.Location, bool) {
	if name == "" {
		return time.Local, true
	}
	if l, ok := zones.Load(name); ok {
		return l.(*time.Location), true
	}
	l, err := time.LoadLocation(name)
	if err != nil {
		return nil, false // not kept: a file's text can be anything
	}
	zones.Store(name, l)
	return l, true
}

// ValidZone reports whether name is a time zone a window can be in: an
// IANA name this uni-switch knows, or "" for this computer's clock.
func ValidZone(name string) bool {
	_, ok := zoneOf(name)
	return ok
}

// LocalZone is the IANA name of this computer's time zone, as a window
// set here records it: $TZ when it names one, else what /etc/localtime
// links to (macOS, Linux). "" when neither says (Windows, a TZ of "UTC+8"
// that names no zone): the window is then on the clock of whichever
// computer runs the gateway, as before.
func LocalZone() string {
	if tz, ok := os.LookupEnv("TZ"); ok {
		tz = strings.TrimPrefix(tz, ":")
		if tz != "" && tz != "Local" && ValidZone(tz) {
			return tz
		}
		return ""
	}
	if n := time.Local.String(); n != "Local" && n != "" && ValidZone(n) {
		return n
	}
	if dst, err := os.Readlink("/etc/localtime"); err == nil {
		if _, name, ok := strings.Cut(dst, "zoneinfo/"); ok && name != "" && ValidZone(name) {
			return name
		}
	}
	return ""
}

// Weekdays are the days a TimeWindow names, as time.Weekday counts them.
var Weekdays = []string{"sun", "mon", "tue", "wed", "thu", "fri", "sat"}

// dayOrder is the week as a window shows it, Monday first.
var dayOrder = []string{"mon", "tue", "wed", "thu", "fri", "sat", "sun"}

// clockMinutes reads "HH:MM" (or "H:MM") as minutes past midnight.
func clockMinutes(s string) (int, bool) {
	h, m, ok := strings.Cut(strings.TrimSpace(s), ":")
	if !ok || len(h) < 1 || len(h) > 2 || len(m) != 2 {
		return 0, false
	}
	hh, err1 := strconv.Atoi(h)
	mm, err2 := strconv.Atoi(m)
	if err1 != nil || err2 != nil || hh < 0 || hh > 23 || mm < 0 || mm > 59 || h[0] == '+' || m[0] == '+' {
		return 0, false
	}
	return hh*60 + mm, true
}

// ParseDay reads a day of the week, "mon" or "Monday", as Weekdays has it.
func ParseDay(s string) (string, bool) {
	s = strings.ToLower(strings.TrimSpace(s))
	if len(s) < 3 {
		return "", false
	}
	for i, d := range Weekdays {
		if strings.HasPrefix(strings.ToLower(time.Weekday(i).String()), s) {
			return d, true
		}
	}
	return "", false
}

// clean checks a window and puts it the one way: "09:00", days known,
// each once in the week's order, none when it is every day.
func (w *TimeWindow) clean() error {
	from, ok := clockMinutes(w.From)
	if !ok {
		return fmt.Errorf("the time %q is not HH:MM (09:00)", w.From)
	}
	to, ok := clockMinutes(w.To)
	if !ok {
		return fmt.Errorf("the time %q is not HH:MM (18:00)", w.To)
	}
	w.From, w.To = fmt.Sprintf("%02d:%02d", from/60, from%60), fmt.Sprintf("%02d:%02d", to/60, to%60)
	var days []string
	for _, d := range w.Days {
		v, ok := ParseDay(d)
		if !ok {
			return fmt.Errorf("%q is not a day of the week (mon … sun)", d)
		}
		days = append(days, v)
	}
	w.Days = nil
	for _, d := range dayOrder {
		if slices.Contains(days, d) {
			w.Days = append(w.Days, d)
		}
	}
	if len(w.Days) == len(dayOrder) {
		w.Days = nil
	}
	w.Zone = strings.TrimSpace(w.Zone)
	if !ValidZone(w.Zone) {
		return fmt.Errorf("the time zone %q is not one uni-switch knows: an IANA name such as Asia/Shanghai or America/New_York", w.Zone)
	}
	return nil
}

// Holds reports whether at is within the window, in its zone. A zone
// this uni-switch doesn't know (a file edited by hand; saving refuses one)
// is an unknown clock, and the window doesn't hold: the rule is as if it
// weren't there, rather than at hours that may be a day's half off.
func (w TimeWindow) Holds(at time.Time) bool {
	from, ok1 := clockMinutes(w.From)
	to, ok2 := clockMinutes(w.To)
	loc, ok3 := zoneOf(w.Zone)
	if at.IsZero() || !ok1 || !ok2 || !ok3 {
		return false
	}
	at = at.In(loc)
	m, day := at.Hour()*60+at.Minute(), at.Weekday()
	switch {
	case from == to: // the whole day
	case from < to:
		if m < from || m >= to {
			return false
		}
	default: // past midnight
		if m >= to && m < from {
			return false
		}
		if m < to {
			day = (day + 6) % 7 // the window began the day before
		}
	}
	return len(w.Days) == 0 || slices.Contains(w.Days, Weekdays[day])
}

// DaysText is the window's days for a list: "Mon–Fri", "Sat,Sun",
// "Mon,Wed–Fri"; "" for every day.
func (w TimeWindow) DaysText() string {
	name := func(d string) string { return strings.ToUpper(d[:1]) + d[1:] }
	var out []string
	for i := 0; i < len(dayOrder); {
		if !slices.Contains(w.Days, dayOrder[i]) {
			i++
			continue
		}
		j := i
		for j+1 < len(dayOrder) && slices.Contains(w.Days, dayOrder[j+1]) {
			j++
		}
		switch j - i {
		case 0:
			out = append(out, name(dayOrder[i]))
		case 1:
			out = append(out, name(dayOrder[i]), name(dayOrder[j]))
		default:
			out = append(out, name(dayOrder[i])+"–"+name(dayOrder[j]))
		}
		i = j + 1
	}
	return strings.Join(out, ",")
}

// Text is the window as a rule's conditions show it: "09:00–18:00 Mon–Fri",
// its zone after it when it has one: "09:00–18:00 (Asia/Shanghai)".
func (w TimeWindow) Text() string {
	s := w.From + "–" + w.To
	if w.From == w.To {
		s = "all day"
	}
	if d := w.DaysText(); d != "" {
		s += " " + d
	}
	if w.Zone != "" {
		s += " (" + w.Zone + ")"
	}
	return s
}

// MaxIntent is how long an intent may be, in characters.
const MaxIntent = 200

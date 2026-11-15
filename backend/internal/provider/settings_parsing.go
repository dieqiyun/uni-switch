package provider

// A rule as it is typed: use=<model>[,<model>…] use-effort=low tokens=200k images effort=high
// agents=a,b intent="…" compact time=09:00-18:00 days=mon-fri tz=Asia/Shanghai
// folders=~/work,"~/My Project" except-folders=~/work/old, or
// pause=<model> time=… days=… agents=… folders=… except-folders=… — by
// uni-switch group rule and the
// TUI's routing page.

import (
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"
)

// ParseTokens reads 200000, 200k, 1.5m.
func ParseTokens(v string) (int, error) {
	s := strings.ToLower(strings.TrimSpace(strings.ReplaceAll(v, "_", "")))
	mul := 1.0
	switch {
	case strings.HasSuffix(s, "k"):
		mul, s = 1e3, strings.TrimSuffix(s, "k")
	case strings.HasSuffix(s, "m"):
		mul, s = 1e6, strings.TrimSuffix(s, "m")
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil || f < 0 || math.IsNaN(f) || math.IsInf(f, 0) {
		return 0, fmt.Errorf("tokens %q is not a length (200000, 200k, 1m)", v)
	}
	n := f * mul
	if n >= float64(math.MaxInt) {
		return 0, fmt.Errorf("tokens %q is not a length (200000, 200k, 1m)", v)
	}
	return int(n), nil
}

// ParseDays reads days of the week: "mon-fri", "sat,sun", "mon,wed-fri";
// a range may run past Sunday (fri-mon).
func ParseDays(v string) ([]string, error) {
	var out []string
	for _, part := range strings.FieldsFunc(v, func(r rune) bool { return r == ',' || r == ' ' }) {
		a, b, isRange := strings.Cut(strings.ReplaceAll(part, "–", "-"), "-")
		from, ok := ParseDay(a)
		if !ok {
			return nil, fmt.Errorf("days: %q is not a day of the week (mon … sun)", a)
		}
		to := from
		if isRange {
			if to, ok = ParseDay(b); !ok {
				return nil, fmt.Errorf("days: %q is not a day of the week (mon … sun)", b)
			}
		}
		for i := slices.Index(Weekdays, from); ; i = (i + 1) % 7 {
			if !slices.Contains(out, Weekdays[i]) {
				out = append(out, Weekdays[i])
			}
			if Weekdays[i] == to {
				break
			}
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("days names the days of the week it holds on: days=mon-fri")
	}
	return out, nil
}

// ParseTimeWindow reads hours of the day: 09:00-18:00, 22:00-08:00, and
// the time zone they are in after an @: 09:00-18:00@Asia/Shanghai.
func ParseTimeWindow(v string) (*TimeWindow, error) {
	hours, zone, _ := strings.Cut(v, "@")
	from, to, ok := strings.Cut(strings.ReplaceAll(strings.TrimSpace(hours), "–", "-"), "-")
	if !ok {
		return nil, fmt.Errorf("time is hours of the day: time=09:00-18:00 (or 22:00-08:00, past midnight; 09:00-18:00@Asia/Shanghai in a time zone), not %q", v)
	}
	w := &TimeWindow{From: strings.TrimSpace(from), To: strings.TrimSpace(to), Zone: strings.TrimSpace(zone)}
	if err := w.clean(); err != nil {
		return nil, err
	}
	return w, nil
}

// zoneErr is what a time zone uni-switch doesn't know is answered with.
func zoneErr(v string) error {
	return fmt.Errorf("tz: %q is not a time zone uni-switch knows: an IANA name such as Asia/Shanghai, Europe/Berlin or America/New_York", v)
}

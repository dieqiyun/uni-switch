package provider

import (
	"testing"
	"time"
)

// #2008: a time of day a daily allowance comes back, on this computer's
// clock or at the vendor's offset, comes round next after now.
func TestNextResetAt(t *testing.T) {
	cst := time.FixedZone("CST", 8*3600)
	now := time.Date(2026, 10, 13, 16, 6, 0, 0, cst) // 08:06 UTC
	for s, want := range map[string]time.Time{
		"16:30":       time.Date(2026, 10, 13, 16, 30, 0, 0, cst),
		"8:00":        time.Date(2026, 10, 14, 8, 0, 0, 0, cst),
		"16:06":       time.Date(2026, 10, 14, 16, 6, 0, 0, cst), // now is gone by
		"00:00+08:00": time.Date(2026, 10, 14, 0, 0, 0, 0, cst),
		"09:00Z":      time.Date(2026, 10, 13, 9, 0, 0, 0, time.UTC),
		"08:00Z":      time.Date(2026, 10, 14, 8, 0, 0, 0, time.UTC),
		"23:30-07:00": time.Date(2026, 10, 13, 23, 30, 0, 0, time.FixedZone("", -7*3600)),
	} {
		got, ok := NextResetAt(s, now)
		if !ok || !got.Equal(want) {
			t.Errorf("%q: %v %v, want %v", s, got, ok, want)
		}
	}
	for _, s := range []string{"", "8", "24:00", "08:60", "8:5", "noon", "08:00+25:00", "08:00 CST"} {
		if _, ok := NextResetAt(s, now); ok {
			t.Errorf("%q read as a time of day", s)
		}
	}
	if _, err := ParseResetAt("noon"); err == nil {
		t.Error("noon taken")
	}
}

// Set, it is kept by providers.json and read back; one that isn't a time
// of day isn't saved, and the provider's own rest stands as it was.
func TestQuotaResetAtSaved(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	if err := Save(Provider{ID: "xk", Name: "xk", Chat: "https://example.invalid/v1", Key: "k", QuotaRest: 60}); err != nil {
		t.Fatal(err)
	}
	if err := SetQuotaResetAt("xk", "noon"); err == nil {
		t.Fatal("noon saved")
	}
	if err := SetQuotaResetAt("xk", "08:00+08:00"); err != nil {
		t.Fatal(err)
	}
	p, err := Find("xk")
	if err != nil || p.QuotaResetAt != "08:00+08:00" || p.QuotaRest != 0 {
		t.Fatalf("%+v %v", p, err)
	}
	now := time.Date(2026, 10, 13, 0, 27, 0, 0, time.FixedZone("", 8*3600))
	if d := p.OwnQuotaRest(now); d != 7*time.Hour+33*time.Minute {
		t.Errorf("rests %v from 00:27", d)
	}
	if err := SetQuotaResetAt("xk", ""); err != nil {
		t.Fatal(err)
	}
	if p, _ = Find("xk"); p.QuotaResetAt != "" || p.OwnQuotaRest(now) != 0 {
		t.Errorf("not let go: %+v", p)
	}
}

package cli

import (
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// #2008: `uni-switch provider quota-rest <id> until 00:00+08:00` sets the time
// of day the allowance comes back, a number of minutes after it takes its
// place, and default lets both go.
func TestQuotaRestUntil(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	if err := provider.Save(provider.Provider{ID: "xk", Name: "xk", Chat: "https://example.invalid/v1", Key: "k"}); err != nil {
		t.Fatal(err)
	}
	find := func() provider.Provider {
		p, err := provider.Find("xk")
		if err != nil {
			t.Fatal(err)
		}
		return *p
	}
	if err := quotaRestCmd([]string{"xk", "until", "00:00+08:00"}); err != nil {
		t.Fatal(err)
	}
	if p := find(); p.QuotaResetAt != "00:00+08:00" {
		t.Fatalf("until: %q", p.QuotaResetAt)
	}
	if err := quotaRestCmd([]string{"xk", "until", "25:00"}); err == nil {
		t.Fatal("25:00 taken")
	}
	if err := quotaRestCmd([]string{"xk", "6h"}); err != nil {
		t.Fatal(err)
	}
	if p := find(); p.QuotaResetAt != "" || p.QuotaRest != 360 {
		t.Fatalf("6h: %q %d", p.QuotaResetAt, p.QuotaRest)
	}
	if err := quotaRestCmd([]string{"xk", "at", "8:00"}); err != nil {
		t.Fatal(err)
	}
	if err := quotaRestCmd([]string{"xk", "default"}); err != nil {
		t.Fatal(err)
	}
	if p := find(); p.QuotaResetAt != "" || p.QuotaRest != 0 {
		t.Fatalf("default: %q %d", p.QuotaResetAt, p.QuotaRest)
	}
}

// #2008: `uni-switch provider rest-cap <id> 4h` sets where its keys' rests stop
// doubling, and default gives it back to the gateway's own.
func TestRestCapCmd(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	if err := provider.Save(provider.Provider{ID: "xk", Name: "xk", Chat: "https://example.invalid/v1", Key: "k"}); err != nil {
		t.Fatal(err)
	}
	cap := func() int {
		p, err := provider.Find("xk")
		if err != nil {
			t.Fatal(err)
		}
		return p.RestCap
	}
	if err := restCapCmd([]string{"xk", "4h"}); err != nil || cap() != 240 {
		t.Fatalf("4h: %v %d", err, cap())
	}
	if err := restCapCmd([]string{"xk", "9d"}); err == nil || cap() != 240 {
		t.Fatalf("9d taken: %d", cap())
	}
	if err := restCapCmd([]string{"xk", "default"}); err != nil || cap() != 0 {
		t.Fatalf("default: %v %d", err, cap())
	}
	// Kismet Alan on Discord: a relay whose unknown errors pass is set
	// never to rest after one, saved and read back so; a minute is the
	// shortest cap, and under it is turned away, not read as default
	if err := restCapCmd([]string{"xk", "off"}); err != nil || cap() != provider.RestCapOff {
		t.Fatalf("off: %v %d", err, cap())
	}
	if p, _ := provider.Find("xk"); !p.NoFailRest() {
		t.Fatal("off not read back as no rest")
	}
	if err := restCapCmd([]string{"xk", "1m"}); err != nil || cap() != 1 {
		t.Fatalf("1m: %v %d", err, cap())
	}
	if err := restCapCmd([]string{"xk", "30s"}); err == nil || cap() != 1 {
		t.Fatalf("30s taken: %v %d", err, cap())
	}
}

// `uni-switch quota reset --card <n>` picks which of the account's resets is
// spent (01huadalang on Discord); a card that isn't a place among them, or
// a flag it doesn't know, is turned away before any account is read, never
// taken for an account's name.
func TestQuotaResetCardArgs(t *testing.T) {
	for _, args := range [][]string{{"--card"}, {"--card", "0"}, {"--card", "two"}, {"me@example.com", "--card", "-1"}, {"--cards", "2"}} {
		if err := quotaResetCmd(args); err == nil {
			t.Fatalf("%q taken", args)
		}
	}
	for n, want := range map[int]string{1: "1st", 2: "2nd", 3: "3rd", 4: "4th", 11: "11th", 12: "12th", 13: "13th", 21: "21st", 22: "22nd", 101: "101st", 111: "111th"} {
		if got := ordinal(n); got != want {
			t.Fatalf("ordinal(%d) = %q, want %q", n, got, want)
		}
	}
}

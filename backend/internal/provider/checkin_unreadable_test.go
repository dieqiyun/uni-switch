package provider

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// A check-in file that is there but can't be read or parsed is unknown,
// never empty: neither the loop nor a press asks the vendor anything, the
// file is left byte for byte as it was, and each account says why. Read
// as empty, the next check-in asked again (a second check-in in a day) and
// wrote over the entries it couldn't read.
func TestCheckinUnreadableFileLeftAlone(t *testing.T) {
	bad := map[string][]byte{
		"cut short": []byte(`{"workbuddy|u1": {"day": "2026-10-21", "outc`),
		"zeroed":    make([]byte, 512),
		"empty":     {},
		"a list":    []byte(`[]`),
		"null":      []byte(`null`),
	}
	for name, b := range bad {
		t.Run(name, func(t *testing.T) {
			f := &fakeWBCheckin{active: true}
			srv := f.serve(t)
			now := time.Date(2026, 10, 21, 3, 0, 0, 0, time.UTC)
			c := wbCheckinFixture(t, srv, &now)
			if err := os.WriteFile(c.path, b, 0o600); err != nil {
				t.Fatal(err)
			}
			for _, soon := range []bool{false, true} {
				rs := c.checkinNow(context.Background(), soon)
				if len(rs) != 1 || rs[0].Outcome != CheckinFailed || rs[0].Asked || rs[0].User != "旅行者" ||
					!strings.Contains(rs[0].Msg, c.path) {
					t.Fatalf("soon %v: %+v", soon, rs)
				}
			}
			if s, cl := f.counts(); s != 0 || cl != 0 {
				t.Fatalf("asked WorkBuddy with its file unread: status %d, claims %d", s, cl)
			}
			if got, err := os.ReadFile(c.path); err != nil || !bytes.Equal(got, b) {
				t.Fatalf("the file became %q (%v), was %q", got, err, b)
			}
		})
	}

	// one that can't be read at all (a folder in its place)
	t.Run("unreadable", func(t *testing.T) {
		var asked atomic.Int32
		path := filepath.Join(t.TempDir(), "plugin-checkin.json")
		if err := os.Mkdir(path, 0o700); err != nil {
			t.Fatal(err)
		}
		c := checkiner{path: path, now: time.Now, mu: &pluginCheckinMu, by: "plugin:x", label: "x", recheck: true,
			accounts: func() []checkinAcct {
				return []checkinAcct{{User: "a", On: true, key: "x|a", do: func(context.Context) WorkBuddyCheckin {
					asked.Add(1)
					return WorkBuddyCheckin{Outcome: CheckinClaimed}
				}}}
			}}
		rs := c.checkinNow(context.Background(), true)
		if len(rs) != 1 || rs[0].Outcome != CheckinFailed || rs[0].By != "plugin:x" || asked.Load() != 0 {
			t.Fatalf("checked in %d times: %+v", asked.Load(), rs)
		}
		if fi, err := os.Stat(path); err != nil || !fi.IsDir() {
			t.Fatalf("its place was written over: %v", err)
		}
	})
}

// A file not there is still empty: the account is checked in.
func TestCheckinMissingFileIsEmpty(t *testing.T) {
	f := &fakeWBCheckin{active: true}
	srv := f.serve(t)
	now := time.Date(2026, 10, 21, 3, 0, 0, 0, time.UTC)
	c := wbCheckinFixture(t, srv, &now)
	rs := c.checkinNow(context.Background(), false)
	if len(rs) != 1 || rs[0].Outcome != CheckinClaimed || !rs[0].Asked {
		t.Fatalf("%+v", rs)
	}
}

// While a file doesn't parse, its cards keep what last read from it, and
// once it parses again the check-in goes on from what it holds.
func TestCheckinUnreadableKeepsLastGood(t *testing.T) {
	f := &fakeWBCheckin{active: true}
	srv := f.serve(t)
	now := time.Date(2026, 10, 21, 3, 0, 0, 0, time.UTC)
	c := wbCheckinFixture(t, srv, &now)
	ctx := context.Background()
	if rs := c.checkinNow(ctx, false); len(rs) != 1 || rs[0].Outcome != CheckinClaimed {
		t.Fatalf("first: %+v", rs)
	}
	good, err := os.ReadFile(c.path)
	if err != nil {
		t.Fatal(err)
	}
	os.WriteFile(c.path, good[:len(good)/2], 0o600)
	if st := shownCheckins(c.path); st["workbuddy|u1"].Outcome != CheckinClaimed {
		t.Fatalf("shown while unread: %+v", st)
	}
	if _, err := readCheckins(c.path); err == nil {
		t.Fatal("a file cut short read without an error")
	}
	// the file mended: today's claim read back, nothing asked again
	os.WriteFile(c.path, good, 0o600)
	if rs := c.checkinNow(ctx, false); len(rs) != 1 || rs[0].Outcome != CheckinClaimed || rs[0].Asked {
		t.Fatalf("mended: %+v", rs)
	}
	if s, cl := f.counts(); s != 1 || cl != 1 {
		t.Fatalf("status %d, claims %d", s, cl)
	}
}

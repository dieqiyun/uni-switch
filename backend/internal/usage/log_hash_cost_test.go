package usage

import (
	"encoding/json"
	"fmt"
	"os"
	"testing"
	"time"
)

// While the gateway writes, the log's change time is nearly always under a
// second old, so each read verifies the snapshot by a fingerprint (#1357).
// The parse itself produces that fingerprint: a rebuild re-reads the file only
// to verify the prefix it continues and to check nothing was rewritten during
// the read, not a third time before the read. Hashing doesn't allocate, so
// the allocation tests don't see this; this counts the bytes re-read.
func TestUsageAppendReadHashesNoPreReadScan(t *testing.T) {
	pageHome(t)
	holdClock(t, time.Now())
	historyLog(t, 20000)
	now := logIndexNow
	logIndexNow = func() time.Time { return time.Now().Add(2 * logStampSettle) }
	Summarize(Today) // settled first, as a log the gateway hasn't touched for a while
	logIndexNow = now
	t.Cleanup(func() { logIndexNow = now })
	hash := logRecordHash
	var hashed int64
	logRecordHash = func(path string, n int64) string {
		hashed += n
		return hash(path, n)
	}
	t.Cleanup(func() { logRecordHash = hash })
	const cycles = 5
	start := time.Now()
	for range cycles {
		Append(Record{Agent: "codex", Provider: "relay", Model: "m", Input: 1, Status: 200})
		Summarize(Today)
	}
	took := time.Since(start)
	info, err := os.Stat(Path())
	if err != nil {
		t.Fatal(err)
	}
	perCycle := hashed / cycles
	t.Logf("log %d bytes: %d bytes hashed per append + read (%.2f× the log), %v per cycle", info.Size(), perCycle, float64(perCycle)/float64(info.Size()), took/cycles)
	if s := Summarize(Today); s.Calls != 1+cycles {
		t.Fatalf("calls today: %d", s.Calls)
	}
	// An unsettled append verifies its prefix and re-hashes after the read:
	// two passes. A third, before the read, is the scan the parse replaces.
	if perCycle > 2*info.Size()+info.Size()/10 {
		t.Fatalf("an append + read hashed %d bytes over a %d-byte log, more than the prefix check and the after-read check", perCycle, info.Size())
	}
}

// Another writer can be mid-line when a read stats the log. The fingerprint
// covers only the complete lines parsed, so the line's completion continues
// the snapshot rather than counting the half line as read.
func TestUsagePartialLineThenCompletion(t *testing.T) {
	for _, settle := range []bool{false, true} {
		t.Run(fmt.Sprint("settled=", settle), func(t *testing.T) {
			pageHome(t)
			now := holdClock(t, time.Now())
			if settle {
				settleLogClock(t)
			}
			historyLog(t, logBlockRows+3)
			line, err := json.Marshal(Record{Time: now, Agent: "codex", Provider: "relay", Model: "m", Session: "late", Input: 41, Status: 200})
			if err != nil {
				t.Fatal(err)
			}
			line = append(line, '\n')
			half := len(line) / 2
			write := func(b []byte) {
				t.Helper()
				f, err := os.OpenFile(Path(), os.O_APPEND|os.O_WRONLY, 0600)
				if err != nil {
					t.Fatal(err)
				}
				if _, err := f.Write(b); err != nil {
					t.Fatal(err)
				}
				if err := f.Close(); err != nil {
					t.Fatal(err)
				}
			}
			write(line[:half])
			before := Summarize(All)
			if s := readLogSnapshot(); s.hash != "" && s.hash != recordHash(Path(), s.off) {
				t.Fatal("fingerprint does not cover the parsed bytes")
			}
			write(line[half:])
			got, want := Summarize(All), summarize(All, now, Load(time.Time{}))
			if got.Totals != want.Totals || got.Calls != before.Calls+1 {
				t.Fatalf("completed line: got %+v (%d calls, %d before) want %+v", got.Totals, got.Calls, before.Calls, want.Totals)
			}
			equalPage(t, QueryPage(All, Filter{}, 0, 100), pageFromLedger(All, Filter{}, 0, 100, LedgerOf(All, Filter{})))
		})
	}
}

// Under steady traffic the gateway appends a call less than a second before
// most reads. A change time with a fraction of a second settles in
// logFineSettle, so a read that long after an append continues the
// snapshot from the appends it noted, hashing nothing again; one in whole
// seconds still waits out logStampSettle (#3394).
//
// Each read is timed from the append's change stamp, not the wall clock: a
// whole-second stamp is up to a second behind the append, so 150 ms after
// an append late in its second is already the next second, past the
// stamp's tick, where nothing is hashed, rightly (#3511). The last case
// holds that: any later write has a later stamp by then.
func TestUsageReadAfterAppendHashesNothingOnAFineClock(t *testing.T) {
	for _, tc := range []struct {
		name   string
		fine   bool
		after  time.Duration // from the stamp to the read
		hashes bool
	}{
		{"fine stamp", true, 150 * time.Millisecond, false},
		{"whole-second stamp", false, 150 * time.Millisecond, true},
		{"whole-second stamp read the next second", false, time.Second + 150*time.Millisecond, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pageHome(t)
			holdClock(t, time.Now())
			historyLog(t, 20000)
			changeTime, now := logChangeTime, logIndexNow
			t.Cleanup(func() { logChangeTime, logIndexNow = changeTime, now })
			if !tc.fine {
				logChangeTime = func(info os.FileInfo) time.Time { return changeTime(info).Truncate(time.Second) }
			}
			// each read comes tc.after past the stamp of the append before it
			stamp := logChangeTime(mustStat(t))
			logIndexNow = func() time.Time { return stamp.Add(tc.after) }
			Summarize(Today)
			hash := logRecordHash
			var hashed int64
			logRecordHash = func(path string, n int64) string {
				hashed += n
				return hash(path, n)
			}
			t.Cleanup(func() { logRecordHash = hash })
			const cycles = 5
			for range cycles {
				Append(Record{Agent: "codex", Provider: "relay", Model: "m", Input: 1, Status: 200})
				stamp = logChangeTime(mustStat(t))
				if tc.fine && stamp.Nanosecond() == 0 {
					t.Skip("this file system keeps change times in whole seconds")
				}
				Summarize(Today)
				Summarize(Today)
			}
			if s := Summarize(Today); s.Calls != 1+cycles {
				t.Fatalf("calls today: %d", s.Calls)
			}
			t.Logf("%d bytes hashed over %d appends", hashed, cycles)
			if tc.hashes != (hashed > 0) {
				t.Fatalf("hashed %d bytes over %d appends and reads %v past each one's stamp", hashed, cycles, tc.after)
			}
		})
	}
}

func mustStat(t *testing.T) os.FileInfo {
	t.Helper()
	info, err := statLogFile(Path())
	if err != nil {
		t.Fatal(err)
	}
	return info
}

// Each append wakes several readers at once (the menu bar, the pages), all
// inside the change stamp's tick. Each of them hashed the whole log again,
// and so did the read that built the snapshot and the first read once it
// settled: four times the log for one request (#4368). The read once it
// settles is the one that checks it.
func TestUsageReadersOfOneAppendHashTheLogOnce(t *testing.T) {
	pageHome(t)
	holdClock(t, time.Now())
	historyLog(t, 20000)
	now := logIndexNow
	t.Cleanup(func() { logIndexNow = now })
	later := func() time.Time { return time.Now().Add(2 * logStampSettle) }
	logIndexNow = later
	Summarize(Today)
	hash := logRecordHash
	var hashed int64
	logRecordHash = func(path string, n int64) string {
		hashed += n
		return hash(path, n)
	}
	t.Cleanup(func() { logRecordHash = hash })
	const cycles = 5
	for range cycles {
		Append(Record{Agent: "codex", Provider: "relay", Model: "m", Input: 1, Status: 200})
		info, err := os.Stat(Path())
		if err != nil {
			t.Fatal(err)
		}
		// the readers come within the tick, whatever the machine's speed
		logIndexNow = func() time.Time { return logChangeTime(info) }
		for range 3 {
			Summarize(Today)
		}
		logIndexNow = later
		Summarize(Today)
	}
	info, err := os.Stat(Path())
	if err != nil {
		t.Fatal(err)
	}
	perCycle := hashed / cycles
	t.Logf("log %d bytes: %d bytes hashed per append and four reads (%.2f× the log)", info.Size(), perCycle, float64(perCycle)/float64(info.Size()))
	if perCycle > info.Size()+info.Size()/10 {
		t.Fatalf("an append and its readers hashed %d bytes over a %d-byte log, more than the one check once it settles", perCycle, info.Size())
	}
	if s := Summarize(Today); s.Calls != 1+cycles {
		t.Fatalf("calls today: %d", s.Calls)
	}
}

package usage

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

// Append keeps the log open between calls (#3725), and still writes to
// the file Path names after another program replaces or removes it.
func TestAppendHeldLogFollowsThePath(t *testing.T) {
	pageHome(t)
	holdClock(t, time.Date(2026, 9, 30, 12, 0, 0, 0, time.Local))
	t.Cleanup(func() {
		mu.Lock()
		defer mu.Unlock()
		if heldLog.f != nil {
			heldLog.f.Close()
			heldLog.f = nil
		}
	})
	Append(Record{Agent: "codex", Provider: "relay", Model: "m", Input: 2, Status: 200})
	first := heldLog.f
	Append(Record{Agent: "codex", Provider: "relay", Model: "m", Input: 3, Status: 200})
	if runtime.GOOS != "windows" && (first == nil || heldLog.f != first) {
		t.Fatalf("the log was opened again for the second call: %p then %p", first, heldLog.f)
	}
	if runtime.GOOS == "windows" && heldLog.f != nil {
		t.Fatal("Windows kept the log open")
	}

	f := filepath.Join(filepath.Dir(Path()), "replacement.jsonl")
	if err := os.WriteFile(f, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(f, Path()); err != nil {
		t.Fatal(err)
	}
	Append(Record{Agent: "codex", Provider: "relay", Model: "m", Input: 7, Status: 200})
	if s := Summarize(Today); s.Input != 7 || s.Calls != 1 {
		t.Fatalf("after a replacement: %+v", s.Totals)
	}

	if err := os.Remove(Path()); err != nil {
		t.Fatal(err)
	}
	Append(Record{Agent: "codex", Provider: "relay", Model: "m", Input: 11, Status: 200})
	if s := Summarize(Today); s.Input != 11 || s.Calls != 1 {
		t.Fatalf("after a removal: %+v", s.Totals)
	}

	pageHome(t) // another HOME: the log is another file
	Append(Record{Agent: "codex", Provider: "relay", Model: "m", Input: 13, Status: 200})
	if s := Summarize(Today); s.Input != 13 || s.Calls != 1 {
		t.Fatalf("in another HOME: %+v", s.Totals)
	}
}

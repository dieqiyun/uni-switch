package sessions

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/catalog"
	"github.com/dieqiyun/uni-switch/backend/internal/settings"
)

func listingsMade() int {
	mu.Lock()
	defer mu.Unlock()
	return keptList.made
}

func keptRow(t *testing.T, list []Session, key string) Session {
	t.Helper()
	for _, s := range list {
		if s.Agent+":"+s.ID == key {
			return s
		}
	}
	t.Fatalf("%s not listed in %d sessions", key, len(list))
	return Session{}
}

// TestListKeptBetweenPolls: a listing over files that haven't changed gives
// the one made last, not every session grouped and assembled again, and a
// file that changed, a price, Codex's name for a thread or the time it was
// kept makes it again. A caller writing a row writes its own copy.
func TestListKeptBetweenPolls(t *testing.T) {
	claude, codex := setup(t)
	const cc, cx = "claude:11111111-2222-3333-4444-555555555555", "codex:01a0bdc9-b5fd-7e63-9658-68bc8dce5ecd"
	first := List(All)
	made := listingsMade()
	again := List(All)
	if n := listingsMade() - made; n != 0 {
		t.Fatalf("unchanged files listed again %d times", n)
	}
	if !reflect.DeepEqual(again, first) {
		t.Fatalf("kept %+v\nlisted %+v", again, first)
	}
	again[0].Title = "written by a caller"
	if List(All)[0].Title == "written by a caller" {
		t.Fatal("a caller's write reached the kept listing")
	}

	out := keptRow(t, first, cc).Tokens.Output
	f, err := os.OpenFile(filepath.Join(claude, "projects", "-work-app", "11111111-2222-3333-4444-555555555555.jsonl"), os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.WriteString(`{"parentUuid":"a1","isSidechain":false,"message":{"model":"claude-opus-5-5","id":"msg_kept","type":"message","role":"assistant","content":[{"type":"text","text":"more"}],"usage":{"input_tokens":1,"output_tokens":7000}},"requestId":"req_kept","type":"assistant","uuid":"a-kept","timestamp":"2026-09-20T10:30:00.000Z","cwd":"/work/app","sessionId":"11111111-2222-3333-4444-555555555555"}` + "\n")
	f.Close()
	if err != nil {
		t.Fatal(err)
	}
	grown := List(All)
	if n := keptRow(t, grown, cc).Tokens.Output; n != out+7000 {
		t.Fatalf("output %d after the file grew, %d before", n, out)
	}

	cost := keptRow(t, grown, cc).Cost
	PriceOf = func(_ settings.Settings, m string) (catalog.Price, bool) {
		return catalog.Price{Input: 40, Output: 200, CacheRead: 2, CacheWrite: 50}, true
	}
	if c := keptRow(t, List(All), cc).Cost; c <= cost {
		t.Fatalf("cost %v after the prices went up, %v before", c, cost)
	}

	line := fmt.Sprintf("{\"id\":\"01a0bdc9-b5fd-7e63-9658-68bc8dce5ecd\",\"thread_name\":\"Named in Codex\",\"updated_at\":%q}\n", time.Now().UTC().Format(time.RFC3339Nano))
	if err := os.WriteFile(filepath.Join(codex, "session_index.jsonl"), []byte(line), 0600); err != nil {
		t.Fatal(err)
	}
	if got := keptRow(t, List(All), cx).Title; got != "Named in Codex" {
		t.Fatalf("title %q after Codex named the thread", got)
	}

	defer func(d time.Duration) { listKeptFor = d }(listKeptFor)
	listKeptFor = 0
	made = listingsMade()
	List(All)
	if n := listingsMade() - made; n != 1 {
		t.Fatalf("a listing kept past listKeptFor was made %d times, want 1", n)
	}
}

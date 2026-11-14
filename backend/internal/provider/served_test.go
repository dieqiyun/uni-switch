package provider

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Each quota is told when its account, plan or key last answered (#570):
// an account by its user, a key by the name or mask its card has, a card
// with no user by any of its provider's; the latest is marked last.
func TestQuotasLastServed(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	if err := Save(Provider{ID: "dk", Name: "DK", Key: "sk-first-key-0001", KeyName: "work", Chat: "http://127.0.0.1:1/v1",
		Keys: []KeyAccount{{Key: "sk-other-key-0002"}}}); err != nil {
		t.Fatal(err)
	}
	now := time.Now().Truncate(time.Second)
	NoteServed(Provider{ID: "codex", Account: &Account{Agent: "codex", User: "A@x.com"}}, now.Add(-time.Hour))
	NoteServed(Provider{ID: "dk", Key: "sk-other-key-0002"}, now.Add(-time.Minute))
	NoteServed(Provider{ID: "solo", Key: "sk-solo"}, now.Add(-2*time.Hour))
	FlushServed() // as the gateway ending writes it
	// the CLI is a process of its own: what it is told is read from the file
	servedAt.Lock()
	servedAt.m = map[string]time.Time{}
	servedAt.Unlock()
	qs := withServed([]Quota{
		{Provider: "codex", Kind: "subscription", User: "a@x.com"},
		{Provider: "codex", Kind: "subscription", User: "b@x.com"},
		{Provider: "dk", Kind: "plan", User: "work"},
		{Provider: "dk", Kind: "plan", User: Mask("sk-other-key-0002")},
		{Provider: "dk", Kind: "balance"},
		{Provider: "solo", Kind: "balance"},
		{Provider: "none", Kind: "balance"},
	}, LastServed())
	want := []*time.Time{ptr(now.Add(-time.Hour)), nil, nil, ptr(now.Add(-time.Minute)), ptr(now.Add(-time.Minute)), ptr(now.Add(-2 * time.Hour)), nil}
	for i, q := range qs {
		if (q.LastServedAt == nil) != (want[i] == nil) || q.LastServedAt != nil && !q.LastServedAt.Equal(*want[i]) {
			t.Errorf("%d %s %s %q: lastServedAt %v, want %v", i, q.Provider, q.Kind, q.User, q.LastServedAt, want[i])
		}
		if last := i == 3 || i == 4; q.Last != last {
			t.Errorf("%d %s %q: last %v", i, q.Provider, q.User, q.Last)
		}
	}
	b, _ := json.Marshal(qs[0])
	if !strings.Contains(string(b), `"lastServedAt":"`+now.Add(-time.Hour).UTC().Format(time.RFC3339)+`"`) || strings.Contains(string(b), `"last"`) {
		t.Errorf("json %s", b)
	}
	if b, _ := json.Marshal(qs[1]); strings.Contains(string(b), "lastServedAt") || strings.Contains(string(b), `"last"`) {
		t.Errorf("never served: %s", b)
	}
	if _, err := os.Stat(servedPath()); err != nil {
		t.Fatal("not kept on disk:", err)
	}
}

func ptr(t time.Time) *time.Time { return &t }

// The Usage page's cards are told it too (okingkee on X): the page and the
// tray panel show the account that answered last in full, and the menu
// bar's "account in use" follows it. A remote uni-switch's card is that one's,
// and the cache the cards are read from keeps none of it.
func TestCardsLastServed(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	now := time.Now().Truncate(time.Second)
	NoteServed(Provider{ID: "codex", Account: &Account{Agent: "codex", User: "B@x.com"}}, now.Add(-time.Minute))
	NoteServed(Provider{ID: "solo", Key: "sk-solo"}, now.Add(-2*time.Hour))
	subs := []SubscriptionQuota{
		{Provider: "codex", User: "a@x.com"},
		{Provider: "codex", User: "b@x.com"},
		{Provider: "codex", User: "b@x.com", From: "studio"},
	}
	balances := []SubscriptionQuota{{Provider: "solo"}}
	got := cardsServed(subs, nil, balances, LastServed())
	if len(got) != 4 {
		t.Fatalf("cards: %+v", got)
	}
	want := []*time.Time{nil, ptr(now.Add(-time.Minute)), nil, ptr(now.Add(-2 * time.Hour))}
	for i, q := range got {
		if (q.LastServedAt == nil) != (want[i] == nil) || q.LastServedAt != nil && !q.LastServedAt.Equal(*want[i]) {
			t.Errorf("%d %s %q from %q: lastServedAt %v, want %v", i, q.Provider, q.User, q.From, q.LastServedAt, want[i])
		}
	}
	if subs[1].LastServedAt != nil || balances[0].LastServedAt != nil {
		t.Error("the cards read from were written to")
	}
	if b, _ := json.Marshal(got[1]); !strings.Contains(string(b), `"lastServedAt":"`) {
		t.Errorf("json %s", b)
	}
}

// In a test binary served.json is written as an answer is noted: a write
// left waiting outlived the test that noted it, and wrote into its temp
// folder as the folder was being removed (BenchmarkGroupOverhead failed
// "TempDir RemoveAll cleanup: … directory not empty").
func TestNoteServedInATestWritesAtOnce(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	at := time.Now().Truncate(time.Second)
	NoteServed(Provider{ID: "dk", Key: "sk-key-0002"}, at)
	var m map[string]time.Time
	if b, err := os.ReadFile(servedPath()); err != nil || json.Unmarshal(b, &m) != nil || !m["dk#"+KeyID("sk-key-0002")].Equal(at) {
		t.Fatalf("not written as the answer was noted: %v %v", err, m)
	}
	servedFlush.Lock()
	pending := servedFlush.pending
	servedFlush.Unlock()
	if pending {
		t.Fatal("a write left waiting past the test")
	}
}

// A request answered isn't a read and a write of served.json: what is
// noted is written once, within servedFlushAfter, however many requests
// were answered meanwhile, or as the gateway ends (FlushServed); the file
// is as it was, merged with what another gateway wrote, and this process
// tells what it noted at once.
func TestNoteServedWritesBehind(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	now := time.Now().Truncate(time.Second)
	// another gateway's, already in the file
	if err := os.MkdirAll(filepath.Dir(servedPath()), 0o700); err != nil {
		t.Fatal(err)
	}
	other, _ := json.Marshal(map[string]time.Time{"other#k1": now.Add(-time.Hour)})
	if err := os.WriteFile(servedPath(), other, 0o600); err != nil {
		t.Fatal(err)
	}
	defer func(d time.Duration) { servedFlushAfter = d }(servedFlushAfter)
	servedFlushAfter = 2 * time.Second // as outside a test binary
	servedFlush.Lock()
	writes := servedFlush.writes
	servedFlush.Unlock()
	key := "dk#" + KeyID("sk-key-0001")
	for i := range 200 {
		NoteServed(Provider{ID: "dk", Key: "sk-key-0001"}, now.Add(time.Duration(i)*time.Millisecond))
	}
	if b, _ := os.ReadFile(servedPath()); string(b) != string(other) {
		t.Fatalf("written on a request answered: %s", b)
	}
	if at := LastServed()[key]; !at.Equal(now.Add(199 * time.Millisecond)) {
		t.Fatalf("this process isn't told at once: %v", LastServed())
	}
	FlushServed()
	FlushServed() // nothing new: no write
	servedFlush.Lock()
	n := servedFlush.writes - writes
	servedFlush.Unlock()
	if n != 1 {
		t.Fatalf("%d writes for 200 answers", n)
	}
	b, err := os.ReadFile(servedPath())
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]time.Time
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err, string(b))
	}
	if len(m) != 2 || !m["other#k1"].Equal(now.Add(-time.Hour)) || !m[key].Equal(now.Add(199*time.Millisecond)) {
		t.Fatalf("file %s", b)
	}
	// and by itself, a moment later
	servedFlushAfter = time.Millisecond
	NoteServed(Provider{ID: "dk", Key: "sk-key-0001"}, now.Add(time.Second))
	for deadline := time.Now().Add(10 * time.Second); ; {
		if b, _ := os.ReadFile(servedPath()); json.Unmarshal(b, &m) == nil && m[key].Equal(now.Add(time.Second)) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("never written")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// BenchmarkNoteServed is what noting a request answered costs the request.
func BenchmarkNoteServed(b *testing.B) {
	home := b.TempDir()
	b.Setenv("HOME", home)
	b.Setenv("USERPROFILE", home)
	b.Setenv("XDG_CONFIG_HOME", b.TempDir())
	p := Provider{ID: "dk", Key: "sk-key-0001"}
	for b.Loop() {
		NoteServed(p, time.Now())
	}
	FlushServed()
}

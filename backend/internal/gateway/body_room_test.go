package gateway

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

func roomOf(room int64) *bodyRoomT { return &bodyRoomT{room: room, why: "test", sized: true} }

// taking runs b.take in the background; its result comes on the channel.
func taking(b *bodyRoomT, ctx context.Context, n int64) chan error {
	out := make(chan error, 1)
	go func() { out <- b.take(ctx, n) }()
	return out
}

func waitQueued(t *testing.T, b *bodyRoomT, n int) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		b.mu.Lock()
		q := len(b.queue)
		b.mu.Unlock()
		if q == n {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("queue = %d, want %d", q, n)
		}
		time.Sleep(time.Millisecond)
	}
}

func roomHeld(b *bodyRoomT) int64 {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.held
}

// Bodies that fit are let in at once; the next waits until one of the
// requests holding room ends, then holds its own, and every byte is given
// back when the requests end (#4004).
func TestBodyRoomWaitsForARequestToEnd(t *testing.T) {
	b := roomOf(100 << 20)
	ctx1, end1 := context.WithCancel(context.Background())
	ctx2, end2 := context.WithCancel(context.Background())
	ctx3, end3 := context.WithCancel(context.Background())
	defer end2()
	defer end3()
	if err := b.take(ctx1, 45<<20); err != nil {
		t.Fatal(err)
	}
	if err := b.take(ctx2, 45<<20); err != nil {
		t.Fatal(err)
	}
	third := taking(b, ctx3, 45<<20)
	waitQueued(t, b, 1)
	select {
	case err := <-third:
		t.Fatalf("a third 45 MiB body was let into 100 MiB beside two: %v", err)
	default:
	}
	end1()
	if err := <-third; err != nil {
		t.Fatal(err)
	}
	if h := roomHeld(b); h != 90<<20 {
		t.Fatalf("held = %d MiB, want 90", h>>20)
	}
	end2()
	end3()
	deadline := time.Now().Add(10 * time.Second)
	for roomHeld(b) != 0 {
		if time.Now().After(deadline) {
			t.Fatalf("held = %d after every request ended", roomHeld(b))
		}
		time.Sleep(time.Millisecond)
	}
}

// A body bigger than the room runs alone: let in when nothing is held, and
// it waits, rather than being refused, while something is.
func TestBodyRoomBiggerThanTheRoomRunsAlone(t *testing.T) {
	b := roomOf(64 << 20)
	small, endSmall := context.WithCancel(context.Background())
	big, endBig := context.WithCancel(context.Background())
	defer endBig()
	if err := b.take(small, 2<<20); err != nil {
		t.Fatal(err)
	}
	got := taking(b, big, 200<<20)
	waitQueued(t, b, 1)
	endSmall()
	if err := <-got; err != nil {
		t.Fatal(err)
	}
	if h := roomHeld(b); h != 200<<20 {
		t.Fatalf("held = %d MiB, want 200", h>>20)
	}
}

// First come, first served: a small body that came after a waiting big one
// waits behind it, so a big body isn't passed by a stream of small ones.
func TestBodyRoomKeepsOrder(t *testing.T) {
	b := roomOf(64 << 20)
	first, endFirst := context.WithCancel(context.Background())
	big, endBig := context.WithCancel(context.Background())
	later, endLater := context.WithCancel(context.Background())
	defer endBig()
	defer endLater()
	if err := b.take(first, 40<<20); err != nil {
		t.Fatal(err)
	}
	bigGot := taking(b, big, 40<<20)
	waitQueued(t, b, 1)
	laterGot := taking(b, later, 2<<20)
	waitQueued(t, b, 2)
	select {
	case <-laterGot:
		t.Fatal("a 2 MiB body went ahead of the 40 MiB one waiting before it")
	default:
	}
	endFirst()
	if err := <-bigGot; err != nil {
		t.Fatal(err)
	}
	if err := <-laterGot; err != nil {
		t.Fatal(err)
	}
}

// A body that waits bodyRoomWait is refused with what is held and why, and
// leaves the queue, so the one behind it is let in when room comes.
func TestBodyRoomRefusesAfterTheWait(t *testing.T) {
	was := bodyRoomWait
	bodyRoomWait = 50 * time.Millisecond
	defer func() { bodyRoomWait = was }()
	b := roomOf(64 << 20)
	ctx1, end1 := context.WithCancel(context.Background())
	ctx2, end2 := context.WithCancel(context.Background())
	defer end1()
	defer end2()
	if err := b.take(ctx1, 60<<20); err != nil {
		t.Fatal(err)
	}
	err := b.take(ctx2, 30<<20)
	var full *bodyFull
	if !errors.As(err, &full) || !strings.Contains(err.Error(), "60 MiB") || !strings.Contains(err.Error(), "the provider was not asked") {
		t.Fatalf("err = %v", err)
	}
	b.mu.Lock()
	q := len(b.queue)
	b.mu.Unlock()
	if q != 0 || roomHeld(b) != 60<<20 {
		t.Fatalf("queue = %d, held = %d MiB after a refusal", q, roomHeld(b)>>20)
	}
}

// A waiter whose client went away leaves the queue and holds nothing.
func TestBodyRoomGoneWaiterLeaves(t *testing.T) {
	b := roomOf(64 << 20)
	ctx1, end1 := context.WithCancel(context.Background())
	ctx2, end2 := context.WithCancel(context.Background())
	defer end1()
	if err := b.take(ctx1, 60<<20); err != nil {
		t.Fatal(err)
	}
	got := taking(b, ctx2, 30<<20)
	waitQueued(t, b, 1)
	end2()
	if err := <-got; !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v", err)
	}
	if h := roomHeld(b); h != 60<<20 {
		t.Fatalf("held = %d MiB", h>>20)
	}
}

// Small bodies, a context never done, a request reading its body twice
// and a machine whose memory isn't known take no room.
func TestBodyRoomTakesNothingItCantGiveBack(t *testing.T) {
	b := roomOf(64 << 20)
	ctx, end := context.WithCancel(context.Background())
	defer end()
	if err := b.take(ctx, bodyRoomFree-1); err != nil || roomHeld(b) != 0 {
		t.Fatalf("a small body: %v, held %d", err, roomHeld(b))
	}
	if err := b.take(context.Background(), 100<<20); err != nil || roomHeld(b) != 0 {
		t.Fatalf("a context never done: %v, held %d", err, roomHeld(b))
	}
	if err := b.take(ctx, 40<<20); err != nil {
		t.Fatal(err)
	}
	if err := b.take(ctx, 40<<20); err != nil || roomHeld(b) != 40<<20 {
		t.Fatalf("one request's second read: %v, held %d MiB", err, roomHeld(b)>>20)
	}
	if err := b.take(ctx, 50<<20); err != nil || roomHeld(b) != 50<<20 {
		t.Fatalf("a body read bigger than its request said: %v, held %d MiB", err, roomHeld(b)>>20)
	}
	unknown := roomOf(0)
	if err := unknown.take(ctx, 400<<20); err != nil || roomHeld(unknown) != 0 {
		t.Fatalf("memory not known: %v, held %d", err, roomHeld(unknown))
	}
}

// The room is a thirty-second of the memory uni-switch may use, 64 MiB at
// least: jihuayu's 1,670 MiB server serves one ~45 MiB body at a time.
func TestBodyRoomSize(t *testing.T) {
	root := t.TempDir()
	writeMeminfo := func(kb string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Join(root, "proc"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, "proc/meminfo"), []byte("MemTotal:       "+kb+" kB\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	wasGOOS, wasRoot := memGOOS, memRoot
	memGOOS, memRoot = "linux", root
	defer func() { memGOOS, memRoot = wasGOOS, wasRoot }()
	writeMeminfo("1710080") // 1,670 MiB
	if n, why := bodyRoomSize(); n != bodyRoomFloor || !strings.Contains(why, "1670 MiB") {
		t.Fatalf("1,670 MiB: %d MiB (%s), want the floor", n>>20, why)
	}
	writeMeminfo("8388608") // 8 GiB
	if n, _ := bodyRoomSize(); n != 256<<20 {
		t.Fatalf("8 GiB: %d MiB, want 256", n>>20)
	}
}

// Through the gateway: a request whose body finds no room is answered 503
// in its protocol's envelope, and the provider is not asked.
func TestBodyRoomFullAnswers503(t *testing.T) {
	was, wasWait := bodyRoom, bodyRoomWait
	bodyRoom, bodyRoomWait = roomOf(64<<20), 50*time.Millisecond
	defer func() { bodyRoom, bodyRoomWait = was, wasWait }()
	hold, end := context.WithCancel(context.Background())
	defer end()
	if err := bodyRoom.take(hold, 60<<20); err != nil {
		t.Fatal(err)
	}
	s := &Server{}
	body := `{"model":"m","input":"` + strings.Repeat("x", 30<<20) + `"}`
	r := httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader([]byte(body)))
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	r = r.WithContext(ctx)
	w := httptest.NewRecorder()
	if got, ok := s.requestBody(w, r, provider.Responses); ok || got != nil {
		t.Fatal("a body with no room was read through")
	}
	if w.Code != http.StatusServiceUnavailable || !strings.Contains(w.Body.String(), "the provider was not asked") {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
}

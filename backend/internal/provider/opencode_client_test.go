package provider

import (
	"net/http"
	"strconv"
	"testing"
	"time"
)

// openCodeDecode reads an id as OpenCode's identifier.ts lays it out: the
// time in milliseconds (kept to 36 bits) and the count of ids made in it.
func openCodeDecode(t *testing.T, id string, descending bool) (ms, counter uint64) {
	t.Helper()
	v, err := strconv.ParseUint(id[4:16], 16, 64)
	if err != nil {
		t.Fatalf("%q: %v", id, err)
	}
	if descending {
		v = ^v & 0xffffffffffff
	}
	return v / 0x1000, v % 0x1000
}

// Zen's free tier answers 403 FreeTierError ("can only be used from within
// OpenCode") to a session id OpenCode couldn't have made (c9cu on Discord:
// muse-spark-1.3-contributor-free from Pi worked one day and not the
// next). Checked against opencode.ai with the same request:
// ses_e9283da055b15JwOAra76bwUx4, a sha256 cut to the shape that uni-switch
// sent, got 403; ses_ecc5d8f28ffe9Cy14V8Em7lI2u, from a real OpenCode
// 1.19.0 run, got 200. The session made is OpenCode's: descending, the
// time it was made, a small count.
func TestOpenCodeSessionIsOpenCodes(t *testing.T) {
	before := uint64(time.Now().UnixMilli()) % (1 << 36)
	id := openCodeSession("conversation-a")
	after := uint64(time.Now().UnixMilli()) % (1 << 36)
	if !openCodeIDRe.MatchString(id) || id[:4] != "ses_" || !openCodeRealSession(id) {
		t.Fatalf("session %q", id)
	}
	if ms, n := openCodeDecode(t, id, true); ms < before || ms > after || n < 1 || n >= 16 {
		t.Fatalf("session %q: time %d (want %d..%d), count %d", id, ms, before, after, n)
	}
	// one conversation keeps one session, another gets its own
	if again, other := openCodeSession("conversation-a"), openCodeSession("conversation-b"); again != id || other == id {
		t.Fatalf("sessions %q %q %q", id, again, other)
	}
	if a, b := openCodeSession(""), openCodeSession(""); a == b || !openCodeRealSession(a) {
		t.Fatalf("no conversation: %q %q", a, b)
	}
	// OpenCode's own goes as it is; a hash cut to its shape doesn't
	const real, hashed = "ses_ecc5d8f28ffe9Cy14V8Em7lI2u", "ses_e9283da055b15JwOAra76bwUx4"
	if got := openCodeSession(real); got != real {
		t.Fatalf("OpenCode's own session became %q", got)
	}
	if openCodeRealSession(hashed) {
		t.Fatal("a hash passes for OpenCode's session")
	}
	if got := openCodeSession(hashed); got == hashed || !openCodeRealSession(got) {
		t.Fatalf("a hashed session went on as %q", got)
	}
	h := http.Header{"X-Session-Affinity": {"from-the-agent"}}
	OpenCodeClient(h, "conversation-a", "")
	if h.Get("x-opencode-session") != id || h.Get("x-opencode-session-id") != id || h.Get("User-Agent") != OpenCodeUserAgent() {
		t.Fatalf("headers %v", h)
	}
	// OpenCode 2.2.5 sends the session in x-session-affinity and
	// x-session-id too, and no x-opencode-request (#3491)
	if h.Get("x-session-affinity") != id || h.Get("X-Session-Id") != id || h.Get("x-opencode-request") != "" {
		t.Fatalf("headers %v", h)
	}
}

// Ids made in one millisecond count up, as OpenCode's do; a request's is
// ascending.
func TestOpenCodeIDsCountUp(t *testing.T) {
	now := time.UnixMilli(1_790_000_000_000)
	first, second := openCodeNewID("msg", false, now), openCodeNewID("msg", false, now)
	if ms, n := openCodeDecode(t, first, false); ms != 1_790_000_000_000%(1<<36) || n != 1 {
		t.Fatalf("%q: time %d count %d", first, ms, n)
	}
	if _, n := openCodeDecode(t, second, false); n != 2 || second <= first {
		t.Fatalf("%q after %q: count %d", second, first, n)
	}
}

// Only the last openCodeKeep conversations' sessions are kept.
func TestOpenCodeSessionsKeptBounded(t *testing.T) {
	first := openCodeSession("bounded-0")
	for i := 1; i <= openCodeKeep; i++ {
		openCodeSession("bounded-" + strconv.Itoa(i))
	}
	openCodeIDs.Lock()
	n := openCodeIDs.order.Len()
	openCodeIDs.Unlock()
	if n > openCodeKeep {
		t.Fatalf("%d kept", n)
	}
	if openCodeSession("bounded-0") == first {
		t.Fatal("the oldest conversation's session was still kept")
	}
}

package gateway

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
	"github.com/dieqiyun/uni-switch/backend/internal/sessions"
	"github.com/dieqiyun/uni-switch/backend/internal/usage"
)

// WorkBuddy 5.8.2's bundled ModelProvider sends the session as
// X-Conversation-ID, not its per-turn request or per-message id.
func TestWorkBuddySessionHeader(t *testing.T) {
	const body = `{"messages":[{"role":"user","content":"hello"}]}`
	h := http.Header{}
	h.Set("X-Conversation-ID", "workbuddy-session")
	h.Set("X-Conversation-Request-ID", "turn-1")
	h.Set("X-Conversation-Message-ID", "message-1")
	for name, got := range map[string]string{
		"session": sessionOf(h), "native": nativeSessionOf(h),
		"conversation": conversationID(h, []byte(body)),
	} {
		if got != "workbuddy-session" {
			t.Errorf("%s = %q, want WorkBuddy's actual session", name, got)
		}
	}
	if got := convOf(h, []byte(body)); got != "" {
		t.Errorf("an explicit WorkBuddy session still has an inferred conversation %q", got)
	}
	h.Set(SessionHeader, "override")
	if sessionOf(h) != "override" || nativeSessionOf(h) != "workbuddy-session" || conversationID(h, []byte(body)) != "workbuddy-session" {
		t.Fatal("the explicit override lost WorkBuddy's native session")
	}
	for _, header := range []string{"x-opencode-session", "x-session-affinity", "x-session-id", "session_id", "session-id", "x-claude-code-session-id"} {
		t.Run(header, func(t *testing.T) {
			h := http.Header{}
			h.Set(header, "existing-session")
			h.Set("X-Conversation-ID", "workbuddy-session")
			if sessionOf(h) != "existing-session" || nativeSessionOf(h) != "existing-session" || conversationID(h, []byte(body)) != "existing-session" {
				t.Fatal("WorkBuddy's fallback header overrode an existing session header")
			}
		})
	}
	h = http.Header{}
	h.Set("X-Conversation-Request-ID", "turn-only")
	h.Set("X-Conversation-Message-ID", "message-only")
	h.Set("X-Parent-Conversation-ID", "parent-only")
	if sessionOf(h) != "" || nativeSessionOf(h) != "" || conversationID(h, []byte(body)) != conversationID(http.Header{}, []byte(body)) {
		t.Fatal("a turn, message or parent id became the current session")
	}
	h.Set("x-conversation-id", "  ")
	if sessionOf(h) != "" || nativeSessionOf(h) != "" {
		t.Fatal("an empty conversation header became a session")
	}
	h.Set("x-conversation-id", "  "+strings.Repeat("s", 140)+"  ")
	if sessionOf(h) != strings.Repeat("s", 128) || nativeSessionOf(h) != strings.Repeat("s", 128) {
		t.Fatal("the new header bypassed the existing session bounds")
	}
}

func TestWorkBuddySessionThroughGateway(t *testing.T) {
	fresh(t)
	var mu sync.Mutex
	var seen []http.Header
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.Copy(io.Discard, r.Body)
		mu.Lock()
		seen = append(seen, r.Header.Clone())
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"id":"chatcmpl-test","model":"m","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":3000,"completion_tokens":1,"prompt_tokens_details":{"cached_tokens":2500}}}`)
	}))
	t.Cleanup(up.Close)
	for _, id := range []string{"first", "second"} {
		if err := provider.Save(provider.Provider{ID: id, Name: id, Key: "test-key", Keys: []provider.KeyAccount{{Key: "other-test-key"}}, Models: []string{"m"}, Chat: up.URL + "/v1", Affinity: provider.AffinitySession}); err != nil {
			t.Fatal(err)
		}
	}
	if err := sessions.SetGatewayRecording(true, false); err != nil {
		t.Fatal(err)
	}
	s := New()
	handler := localGuard(s.Handler())
	send := func(model, session, turn, message string) Route {
		t.Helper()
		req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(`{"model":"`+model+`","messages":[{"role":"user","content":"hello"}]}`))
		req.RemoteAddr = "127.0.0.1:5000"
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("User-Agent", "CLI/2.161.0 WorkBuddy/2.161.0")
		req.Header.Set("Authorization", "Bearer "+TokenFor("workbuddy"))
		req.Header.Set("X-Conversation-ID", session)
		req.Header.Set("X-Conversation-Request-ID", turn)
		req.Header.Set("X-Conversation-Message-ID", message)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK || rec.Header().Get(SessionHeader) != session {
			t.Fatalf("reply status=%d session=%q body=%s", rec.Code, rec.Header().Get(SessionHeader), rec.Body)
		}
		rows := s.Trace(context.Background(), 0, 0).Routes
		last := rows[len(rows)-1]
		if last.Agent != "workbuddy" || last.Session != session || last.Conv != "" {
			t.Fatalf("request was not attributed to its real WorkBuddy session: agent=%q session=%q conv=%q", last.Agent, last.Session, last.Conv)
		}
		return last
	}
	first := send("first/m", "chat-a", "turn-1", "message-1")
	again := send("first/m", "chat-a", "turn-1", "message-2")
	other := send("first/m", "chat-b", "turn-1", "message-3")
	send("second/m", "chat-a", "turn-2", "message-4")
	if first.Affinity == nil || first.Affinity.Why != "first" || again.Affinity == nil || !again.Affinity.Kept || other.Affinity == nil || other.Affinity.Why != "first" {
		t.Fatalf("sessions with the same first message shared affinity: first=%+v again=%+v other=%+v", first.Affinity, again.Affinity, other.Affinity)
	}
	counts := map[string]int{}
	for _, record := range usage.Load(time.Time{}) {
		if record.Agent == "workbuddy" {
			if record.NativeSession != record.Session {
				t.Errorf("usage lost the native session: %+v", record)
			}
			counts[record.Session]++
		}
	}
	if counts["chat-a"] != 3 || counts["chat-b"] != 1 {
		t.Fatalf("usage grouped requests incorrectly: %v", counts)
	}
	for id, want := range map[string]int{"chat-a": 3, "chat-b": 1} {
		transcript, err := sessions.GatewayTranscript("workbuddy", id)
		if err != nil || transcript.Captured != want {
			t.Errorf("session %s captured %d turns, want %d: %v", id, transcript.Captured, want, err)
		}
	}
	get := httptest.NewRequest("GET", "/v1/uni-switch/route", nil)
	get.RemoteAddr = "127.0.0.1:5000"
	get.Header.Set("X-Conversation-ID", "chat-a")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, get)
	var latest struct {
		Session string        `json:"session"`
		Route   *SessionRoute `json:"route"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &latest); err != nil || rec.Code != http.StatusOK || latest.Session != "chat-a" || latest.Route == nil || latest.Route.Asked != "second/m" {
		t.Fatalf("session lookup missed WorkBuddy's latest model: %d %s (%v)", rec.Code, rec.Body, err)
	}
	mu.Lock()
	defer mu.Unlock()
	for _, headers := range seen {
		for _, name := range []string{SessionHeader, "X-Conversation-ID", "X-Conversation-Request-ID", "X-Conversation-Message-ID"} {
			if headers.Get(name) != "" {
				t.Errorf("private client identity leaked to the vendor in %s", name)
			}
		}
	}
}

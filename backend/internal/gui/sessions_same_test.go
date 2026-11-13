package gui

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/sessions"
)

// The Sessions tab asks for its list and stats every 15 s, and over a long
// history the list is ~8 MB the page parsed and compared to find it the
// same. An ask that says the tag of the answer it has gets no body while
// the answer is the same, and the whole answer once it isn't.
func TestSessionsSentOnceWhileTheSame(t *testing.T) {
	h := sandboxHome(t)
	proj := filepath.Join(h, ".claude", "projects", "-work")
	if err := os.MkdirAll(proj, 0o755); err != nil {
		t.Fatal(err)
	}
	write := func(id, at string) {
		b := `{"parentUuid":null,"isSidechain":false,"type":"user","message":{"role":"user","content":"hi"},"uuid":"u","timestamp":"` + at + `","cwd":"/work","sessionId":"` + id + `"}` + "\n" +
			`{"parentUuid":"u","isSidechain":false,"message":{"model":"claude-sonnet-5","id":"msg_` + id + `","type":"message","role":"assistant","content":[{"type":"text","text":"done"}],"usage":{"input_tokens":10,"output_tokens":2}},"requestId":"req_1","type":"assistant","uuid":"a","timestamp":"` + at + `","cwd":"/work","sessionId":"` + id + `"}` + "\n"
		if err := os.WriteFile(filepath.Join(proj, id+".jsonl"), []byte(b), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("00000001-1111-2222-3333-444444444444", "2026-09-20T10:00:00.000Z")
	t.Setenv("CLAUDE_CONFIG_DIR", filepath.Join(h, ".claude"))
	sessions.Reset()
	t.Cleanup(func() { sessions.Reset() })

	mux := http.NewServeMux()
	sessionRoutes(mux, folderOnly{})
	get := func(path, had string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("GET", path, nil)
		if had != "" {
			r.Header.Set("X-Uni-Switch-Had", had)
		}
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		if w.Code != 200 {
			t.Fatalf("%s: %d %s", path, w.Code, w.Body)
		}
		return w
	}
	for _, path := range []string{"/api/sessions"} {
		first := get(path, "")
		tag := first.Header().Get("X-Uni-Switch-Tag")
		if tag == "" || first.Body.Len() == 0 || first.Header().Get("X-Uni-Switch-Same") != "" {
			t.Fatalf("%s: the first answer has tag %q and %d bytes, same %q", path, tag, first.Body.Len(), first.Header().Get("X-Uni-Switch-Same"))
		}
		again := get(path, tag)
		if again.Header().Get("X-Uni-Switch-Same") != "1" || again.Body.Len() != 0 || again.Header().Get("X-Uni-Switch-Tag") != tag {
			t.Errorf("%s: asked with the answer's own tag, got same %q, %d bytes, tag %q", path, again.Header().Get("X-Uni-Switch-Same"), again.Body.Len(), again.Header().Get("X-Uni-Switch-Tag"))
		}
		if other := get(path, "0123"); other.Header().Get("X-Uni-Switch-Same") != "" || other.Body.String() != first.Body.String() {
			t.Errorf("%s: asked with another tag, got same %q and %d bytes, want the whole answer", path, other.Header().Get("X-Uni-Switch-Same"), other.Body.Len())
		}
	}

	// a session more: the tag the page has is of an answer gone
	tags := map[string]string{}
	bodies := map[string]string{}
	for _, path := range []string{"/api/sessions"} {
		w := get(path, "")
		tags[path], bodies[path] = w.Header().Get("X-Uni-Switch-Tag"), w.Body.String()
	}
	write("00000002-1111-2222-3333-444444444444", "2026-09-21T10:00:00.000Z")
	sessions.Reset()
	for path, had := range tags {
		w := get(path, had)
		if w.Header().Get("X-Uni-Switch-Same") != "" || w.Body.Len() == 0 || w.Header().Get("X-Uni-Switch-Tag") == had {
			t.Errorf("%s: after a new session, asked with the old tag, got same %q, %d bytes, tag %q", path, w.Header().Get("X-Uni-Switch-Same"), w.Body.Len(), w.Header().Get("X-Uni-Switch-Tag"))
		}
		whole := get(path, "")
		if path != "/api/sessions" {
			if w.Body.String() != whole.Body.String() {
				t.Errorf("%s: asked with the old tag, got %s, want the whole answer", path, w.Body)
			}
			continue
		}
		// the list sends the new session's row, and the old one as a run
		if w.Header().Get("X-Uni-Switch-Delta") != "sessions" {
			t.Fatalf("the list after a new session: delta %q, want sessions", w.Header().Get("X-Uni-Switch-Delta"))
		}
		var delta, old, want struct {
			Sessions []json.RawMessage `json:"sessions"`
			Terminal bool              `json:"terminal"`
			Dirs     []string          `json:"dirs"`
		}
		for _, b := range []struct {
			s  string
			to any
		}{{w.Body.String(), &delta}, {bodies[path], &old}, {whole.Body.String(), &want}} {
			if err := json.Unmarshal([]byte(b.s), b.to); err != nil {
				t.Fatal(err)
			}
		}
		if len(delta.Sessions) != 2 || string(delta.Sessions[1]) != "[0,1]" || !strings.Contains(string(delta.Sessions[0]), "00000002") {
			t.Fatalf("the delta's list is %s, want the new row and [0,1]", w.Body)
		}
		var back []json.RawMessage
		for _, r := range delta.Sessions {
			var run []int
			if json.Unmarshal(r, &run) == nil {
				back = append(back, old.Sessions[run[0]:run[0]+run[1]]...)
				continue
			}
			back = append(back, r)
		}
		delta.Sessions = back
		got, _ := json.Marshal(delta)
		if wantB, _ := json.Marshal(want); string(got) != string(wantB) {
			t.Errorf("the delta read on the old list is %s, want %s", got, wantB)
		}
	}
}

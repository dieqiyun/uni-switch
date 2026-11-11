package gateway

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// listClaudeRuns asks srv for its Claude Code runs as `uni-switch claude-code
// runs` does, over loopback.
func listClaudeRuns(t *testing.T, srv *Server) ClaudeRuns {
	t.Helper()
	r := httptest.NewRequest("GET", ClaudeRunsPath, nil)
	r.RemoteAddr, r.Host = "127.0.0.1:50123", "127.0.0.1:3425"
	r.Header.Set("User-Agent", "Go-http-client/1.1")
	w := httptest.NewRecorder()
	localGuard(srv.Handler()).ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("%s: %d %s", ClaudeRunsPath, w.Code, w.Body.String())
	}
	var out ClaudeRuns
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil || out.Object != "claude-runs" {
		t.Fatalf("%s answered %s (%v)", ClaudeRunsPath, w.Body.String(), err)
	}
	return out
}

// runsUntil asks for the runs until ok holds of them, 10s at most.
func runsUntil(t *testing.T, srv *Server, what string, ok func(ClaudeRuns) bool) ClaudeRuns {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		l := listClaudeRuns(t, srv)
		if ok(l) {
			return l
		}
		if time.Now().After(deadline) {
			t.Fatalf("never %s: %+v", what, l)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// A Claude account's conversations, a subagent's each, run on Claude Code
// processes of their own that uni-switch keeps for the next turn or while the
// client's tools run, and a task manager shows them as Claude Code's
// version and "uni-switch" (@Candydidi3 on X: "uni-switch里拉起很多子进程").
// ClaudeRunsPath names each: its pid, model and account, whether it
// answers, waits for its next turn or on tools, since when and until
// when uni-switch keeps it, and drops it once it ends.
func TestClaudeRunsNamesEachProcess(t *testing.T) {
	fresh(t)
	t.Setenv("UNI_SWITCH_ADDR", "")
	t.Setenv("UNI_SWITCH_PUBLIC_URL", "")

	t.Run("kept for its next turn", func(t *testing.T) {
		fakeClaude(t)
		s := New()
		t.Cleanup(s.subscription.abortAll)
		p := provider.Provider{ID: "claude", Account: &provider.Account{Agent: "claude", User: "me@example.com"}}
		body := `{"model":"claude-opus-4-1","max_tokens":100,"tools":[{"name":"read","input_schema":{"type":"object"}}],"messages":[{"role":"user","content":"a subagent's task"}]}`
		rec := httptest.NewRecorder()
		var u Usage
		before := time.Now()
		req := httptest.NewRequest("POST", "/v1/messages", strings.NewReader(body))
		if code, msg := s.serveClaudeSubscription(rec, req, provider.Anthropic, p, "claude-opus-4-1", []byte(body), &u); code != 200 {
			t.Fatalf("%d %s", code, msg)
		}
		m := regexp.MustCompile(`pid (\d+) turn 1`).FindStringSubmatch(rec.Body.String())
		if m == nil {
			t.Fatalf("the reply names no pid: %s", rec.Body.String())
		}
		pid, _ := strconv.Atoi(m[1])
		l := runsUntil(t, s, "idle", func(l ClaudeRuns) bool { return len(l.Runs) == 1 && l.Runs[0].State == "idle" })
		r := l.Runs[0]
		if r.PID != pid || !alive(pid) || r.Model != "claude-opus-4-1" || r.Account != "me@example.com" {
			t.Errorf("the run is listed as %+v; Claude Code is pid %d", r, pid)
		}
		if r.Started.Before(before) || r.Since.Before(r.Started) || time.Since(r.Since) > time.Minute {
			t.Errorf("started %v, idle since %v (asked at %v)", r.Started, r.Since, before)
		}
		if left := time.Until(r.Ends); left < idleLongest-time.Minute || left > idleLongest {
			t.Errorf("it ends in %s, not the %s an idle run is kept", left, idleLongest)
		}
		if l.Live < 1 || l.IdleMost != idleMost || l.IdleFor != idleLongest {
			t.Errorf("live %d, idleMost %d, idleFor %s", l.Live, l.IdleMost, l.IdleFor)
		}
		s.subscription.abortAll()
		runsUntil(t, s, "dropped once it ended", func(l ClaudeRuns) bool { return len(l.Runs) == 0 })
	})

	t.Run("waiting on tools, and answering", func(t *testing.T) {
		dir := fakeClaudeCalling(t)
		s := New()
		t.Cleanup(s.subscription.abortAll)
		pid := askForCall(t, s, context.Background(), "subagent: read the file")
		if pid == 0 {
			t.Fatal("no tool call")
		}
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan struct{})
		go func() {
			defer close(done)
			p := provider.Provider{ID: "claude", Account: &provider.Account{Agent: "claude", User: "u"}}
			body := `{"model":"claude-sonnet-5","max_tokens":100,"tools":[{"name":"read","input_schema":{"type":"object"}}],"messages":[{"role":"user","content":"hang on"}]}`
			var u Usage
			req := httptest.NewRequest("POST", "/v1/messages", strings.NewReader(body)).WithContext(ctx)
			s.serveClaudeSubscription(httptest.NewRecorder(), req, provider.Anthropic, p, "claude-sonnet-5", []byte(body), &u)
		}()
		t.Cleanup(func() { cancel(); <-done })
		var hung int
		l := runsUntil(t, s, "two runs, one answering", func(l ClaudeRuns) bool {
			b, err := os.ReadFile(filepath.Join(dir, "hang.pid"))
			hung, _ = strconv.Atoi(strings.TrimSpace(string(b)))
			if err != nil || hung <= 0 || len(l.Runs) != 2 {
				return false
			}
			// the hung run's pid is listed once launch has set its tree,
			// a moment after Claude Code wrote hang.pid
			for _, r := range l.Runs {
				if r.PID == hung {
					return true
				}
			}
			return false
		})
		by := map[int]ClaudeRun{}
		for _, r := range l.Runs {
			by[r.PID] = r
		}
		if r := by[pid]; r.State != "tools" || r.Model != "claude-sonnet-5" || r.Account != "u" || !r.Ends.After(time.Now()) || time.Until(r.Ends) > runTimeout {
			t.Errorf("the run waiting on tools, pid %d, is listed as %+v", pid, r)
		}
		if r := by[hung]; r.State != "answering" || r.Since.IsZero() {
			t.Errorf("the run answering, pid %d, is listed as %+v", hung, r)
		}
		cancel()
		<-done
		runsUntil(t, s, "the given-up turn's run dropped", func(l ClaudeRuns) bool {
			return len(l.Runs) == 1 && l.Runs[0].PID == pid
		})
	})

	t.Run("to this computer's programs only", func(t *testing.T) {
		srv := New()
		for _, c := range profileCallersRefused("uni-switch-no-such-key") {
			r := httptest.NewRequest("GET", ClaudeRunsPath, nil)
			r.RemoteAddr, r.Host = c.remote, c.host
			for i := 0; i+1 < len(c.hdr); i += 2 {
				r.Header.Set(c.hdr[i], c.hdr[i+1])
			}
			w := httptest.NewRecorder()
			localGuard(srv.Handler()).ServeHTTP(w, r)
			if w.Code != http.StatusForbidden && w.Code != http.StatusUnauthorized {
				t.Errorf("%s: %d %s", c.name, w.Code, w.Body.String())
			}
		}
	})
}

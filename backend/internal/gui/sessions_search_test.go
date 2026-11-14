package gui

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/sessions"
	"github.com/dieqiyun/uni-switch/backend/internal/usage"
)

// YOU on Discord: a session the gateway recorded is listed as ses_…, and the
// Sessions page's filter matched only titles, folders, ids and models, so
// one couldn't be found by what was said in it. It is titled by what it was
// first asked, and /api/sessions/search finds it by any words of it.
func TestSessionsSearchFindsGatewaySessionsByWhatWasSaid(t *testing.T) {
	sandboxHome(t)
	sessions.Reset()
	t.Cleanup(sessions.Reset)
	mux := http.NewServeMux()
	sessionRoutes(mux, folderOnly{})
	sessionManageRoutes(mux, folderOnly{})
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest("POST", "/api/sessions/recording", strings.NewReader(`{"on":true}`)))
	if w.Code != 200 {
		t.Fatalf("consent: %d %s", w.Code, w.Body)
	}
	now := time.Now()
	say := func(id, asked, answer string, at time.Time) {
		t.Helper()
		usage.Append(usage.Record{Time: at, Agent: "pi", Session: id, Provider: "fake", Model: "m", Status: 200})
		if err := sessions.SaveGatewayTurn(sessions.GatewayTurn{Agent: "pi", Session: id, Time: at,
			Input: []sessions.Part{
				{Role: "system", Kind: "context", Text: "You are Pi. Never mention the word zebra."},
				{Role: "user", Kind: "text", Text: asked},
			},
			Output: []sessions.Part{{Role: "assistant", Kind: "text", Text: answer}}}); err != nil {
			t.Fatal(err)
		}
	}
	say("ses_old", "rename foo to bar in the parser", "Renamed it in parse.go.", now.Add(-2*time.Minute))
	say("ses_new", "why does the Parser panic on empty input?", "An index out of range in lex.go.", now.Add(-time.Minute))
	say("ses_none", "write a haiku", "Autumn moonlight", now)

	// the list says what each was asked, not its id
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest("GET", "/api/sessions/manage?agent=pi", nil))
	var manage struct {
		Sessions []struct{ ID, Title string }
	}
	if err := json.Unmarshal(w.Body.Bytes(), &manage); err != nil {
		t.Fatal(err)
	}
	titles := map[string]string{}
	for _, s := range manage.Sessions {
		titles[s.ID] = s.Title
	}
	if titles["ses_old"] != "rename foo to bar in the parser" || titles["ses_none"] != "write a haiku" {
		t.Fatalf("titles: %v", titles)
	}

	search := func(q string) searchJSON {
		t.Helper()
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest("GET", "/api/sessions/search?agent=pi&q="+q, nil))
		var out searchJSON
		if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &out) != nil {
			t.Fatalf("search %q: %d %s", q, w.Code, w.Body)
		}
		return out
	}
	got := search("PARSER")
	if len(got.Hits) != 2 || got.Hits[0].ID != "ses_new" || got.Hits[1].ID != "ses_old" || got.Total != 3 || got.Searched != 3 || got.Cut {
		t.Fatalf("parser: %+v", got)
	}
	if h := got.Hits[0]; h.Match != "Parser" || h.Before != "why does the " || h.Role != "user" {
		t.Fatalf("snippet: %+v", h)
	}
	if got := search("lex.go"); len(got.Hits) != 1 || got.Hits[0].ID != "ses_new" || got.Hits[0].Role != "assistant" {
		t.Fatalf("an answer's words: %+v", got)
	}
	// the system prompt every turn carries is not what was said
	if got := search("zebra"); len(got.Hits) != 0 {
		t.Fatalf("system prompt matched: %+v", got)
	}
	// one letter is no search
	if got := search("a"); len(got.Hits) != 0 || got.Total != 0 {
		t.Fatalf("one letter: %+v", got)
	}
	if got := search("haiku"); len(got.Hits) != 1 || got.Hits[0].ID != "ses_none" {
		t.Fatalf("haiku: %+v", got)
	}
}

// A search that runs out of time, or whose page asked another, stops and
// says it was cut short rather than reading on.
func TestSessionsSearchStopsWhenItsRequestEnds(t *testing.T) {
	sandboxHome(t)
	sessions.Reset()
	t.Cleanup(sessions.Reset)
	mux := http.NewServeMux()
	sessionManageRoutes(mux, folderOnly{})
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest("POST", "/api/sessions/recording", strings.NewReader(`{"on":true}`)))
	for i, id := range []string{"ses_a", "ses_b", "ses_c"} {
		at := time.Now().Add(time.Duration(i) * time.Second)
		usage.Append(usage.Record{Time: at, Agent: "pi", Session: id, Provider: "fake", Model: "m", Status: 200})
		if err := sessions.SaveGatewayTurn(sessions.GatewayTurn{Agent: "pi", Session: id, Time: at,
			Input: []sessions.Part{{Role: "user", Kind: "text", Text: "hello there"}}}); err != nil {
			t.Fatal(err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	done := make(chan searchJSON, 1)
	go func() { done <- searchSessions(ctx, "pi", "hello", time.Minute) }()
	select {
	case got := <-done:
		if !got.Cut || len(got.Hits) != 0 || got.Total != 3 {
			t.Fatalf("a cancelled search: %+v", got)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("a cancelled search didn't return")
	}
	if got := searchSessions(context.Background(), "pi", "hello", time.Minute); len(got.Hits) != 3 || got.Cut {
		t.Fatalf("the same search, not cancelled: %+v", got)
	}
}

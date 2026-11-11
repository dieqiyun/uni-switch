package gateway

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/usage"
)

// usageRows waits for n usage rows of op, and answers those there are.
func usageRows(t *testing.T, op string, n int) []usage.Record {
	t.Helper()
	var got []usage.Record
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(10 * time.Millisecond) {
		got = got[:0]
		for _, r := range usage.Load(time.Time{}) {
			if r.Operation == op {
				got = append(got, r)
			}
		}
		if len(got) >= n {
			break
		}
	}
	return got
}

// oneSideRoute is the one route s has, done, which the one usage row of op
// names: what Routing lists of a request Usage lists.
func oneSideRoute(t *testing.T, s *Server, op string) (Route, usage.Record) {
	t.Helper()
	rows := usageRows(t, op, 1)
	if len(rows) != 1 {
		t.Fatalf("%d usage rows of %s, want 1: %+v", len(rows), op, rows)
	}
	routes := s.Trace(context.Background(), 0, 0).Routes
	if len(routes) != 1 {
		t.Fatalf("Routing lists %d routes, want the request's one: %+v", len(routes), routes)
	}
	tr := routes[0]
	if !tr.Done || tr.ID == 0 || tr.ID != rows[0].RouteID {
		t.Fatalf("route %d (done %v) isn't the one the usage row names (%d)", tr.ID, tr.Done, rows[0].RouteID)
	}
	if tr.Kind != "" {
		t.Fatalf("route kind %q: the page would call it an unrecognized purpose", tr.Kind)
	}
	if tr.Agent == "" || tr.Agent != rows[0].Agent || tr.Session != rows[0].Session || tr.Status != rows[0].Status {
		t.Fatalf("route %+v doesn't say what its usage row does: %+v", tr, rows[0])
	}
	// each try has its seat in the order, as the page finds it
	for _, try := range tr.Tries {
		found := false
		for _, w := range tr.Order {
			found = found || w.ID == try.ID && w.Model == try.Model
		}
		if !found || !try.Done {
			t.Fatalf("try %+v (seated %v) in order %+v", try, found, tr.Order)
		}
	}
	return tr, rows[0]
}

// zzzkl3994 on Discord: a video the uni-switch-image MCP made was in Usage's
// requests and not on Routing's. Every request uni-switch sends is listed on
// Routing: an image, a video's start (once, however often it is polled
// and fetched) and an embedding, as a chat is, with its one usage row.
func TestImageIsOnRouting(t *testing.T) {
	s, _ := easeled(t)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/v1/images/generations", strings.NewReader(`{"model":"art/gpt-image-1","prompt":"a uni-switch"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "claude-code/2.1.0 "+DrawAgent)
	req.Header.Set("X-Uni-Switch-Session", "draw-session")
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	tr, row := oneSideRoute(t, s, "generate_content")
	if tr.Provider != "art" || tr.Model != "art/gpt-image-1" || tr.Status != 200 || tr.Session != "draw-session" || tr.Error != "" {
		t.Fatalf("route %+v", tr)
	}
	if len(tr.Tries) != 1 || tr.Tries[0].ID != "art" || tr.Tries[0].Model != "gpt-image-1" || tr.Tries[0].Status != 200 {
		t.Fatalf("tries %+v", tr.Tries)
	}
	if tr.Tokens != row.Input+row.Output || len(tr.Usage) == 0 {
		t.Fatalf("route tokens %d usage %+v, row %+v", tr.Tokens, tr.Usage, row)
	}
}

func TestEmbeddingAndRerankAreOnRouting(t *testing.T) {
	for _, tc := range []struct{ path, op, body, model string }{
		{"/v1/embeddings", "embeddings", `{"model":"lib/embed-1","input":["hello"]}`, "embed-1"},
		{"/v1/rerank", "rerank", `{"model":"lib/rerank-1","query":"q","documents":["a","b"]}`, "rerank-1"},
	} {
		t.Run(tc.op, func(t *testing.T) {
			s, _ := shelved(t)
			if code, raw := postRetrieval(t, s, tc.path, tc.body); code != 200 {
				t.Fatalf("%d %s", code, raw)
			}
			tr, row := oneSideRoute(t, s, tc.op)
			var asked struct{ Model string }
			json.Unmarshal([]byte(tc.body), &asked)
			if tr.Provider != "lib" || tr.Model != asked.Model || tr.Status != 200 || len(tr.Tries) != 1 || tr.Tries[0].ID != "lib" || tr.Tries[0].Model != tc.model {
				t.Fatalf("route %+v", tr)
			}
			if tr.Tokens == 0 || tr.Tokens != row.Input+row.Output {
				t.Fatalf("route tokens %d, row %+v", tr.Tokens, row)
			}
		})
	}
}

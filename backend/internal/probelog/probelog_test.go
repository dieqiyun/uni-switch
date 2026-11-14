package probelog

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func catch(t *testing.T) *[]Row {
	t.Helper()
	var rows []Row
	old := Sink
	Sink = func(r Row) { rows = append(rows, r) }
	t.Cleanup(func() { Sink = old })
	return &rows
}

// A row keeps a URL's host and path, never its query (Gemini's ?key=) or
// its user:password; a GET says so; a provider not yet saved goes by its
// host; a context with no mark writes nothing.
func TestNoteKeepsNoSecret(t *testing.T) {
	rows := catch(t)
	ctx := WithModel(With(context.Background(), KindList, "", ""), "m")
	Note(ctx, "https://user:pw@api.example/v1beta/models?key=sk-1#x", http.MethodGet, time.Now(), 200, nil)
	Note(context.Background(), "https://api.example/v1", http.MethodPost, time.Now(), 200, nil)
	Note(With(context.Background(), KindTest, "relay", "me"), "https://api.example", http.MethodPost, time.Now(), 0, errors.New("dial: refused"))
	if len(*rows) != 2 {
		t.Fatalf("%+v", *rows)
	}
	a, b := (*rows)[0], (*rows)[1]
	if a.Host != "api.example" || a.Endpoint != "GET /v1beta/models" || a.Provider != "api.example" || a.Kind != KindList || a.Model != "m" {
		t.Errorf("%+v", a)
	}
	if b.Endpoint != "/" || b.Provider != "relay" || b.Account != "me" || b.Error != "dial: refused" || b.Model != "" {
		t.Errorf("%+v", b)
	}
}

// The transport writes a marked request down once, and none that a caller
// took or that has no mark; a kind set inside another replaces it and
// keeps the model.
func TestTransportOnce(t *testing.T) {
	rows := catch(t)
	srv := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) { rw.WriteHeader(http.StatusTeapot) }))
	defer srv.Close()
	c := &http.Client{Transport: Transport(http.DefaultTransport)}
	marked := WithModel(With(context.Background(), KindTest, "p", ""), "m")
	inner := With(marked, KindList, "p", "")
	for _, ctx := range []context.Context{marked, Taken(marked), context.Background(), inner} {
		req, _ := http.NewRequestWithContext(ctx, http.MethodPost, srv.URL+"/x", nil)
		res, err := c.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
	}
	if len(*rows) != 2 || (*rows)[0].Status != http.StatusTeapot || (*rows)[0].Kind != KindTest || (*rows)[1].Kind != KindList || (*rows)[1].Model != "m" {
		t.Fatalf("%+v", *rows)
	}
	if Pending(Taken(marked)) || !Pending(inner) || Kind(inner) != KindList {
		t.Fatal("marks")
	}
}

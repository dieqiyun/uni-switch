package gateway

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/catalog"
	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// unknownEyed is eyed with the text model's list saying nothing of images,
// as most vendors' /models lists don't: uni-switch counts it text-only (agents
// were told so before Vision told them every model takes images).
func unknownEyed(t *testing.T) (*Server, *eyeUp) {
	t.Helper()
	s, u := eyed(t)
	p, err := provider.Find("probe")
	if err != nil {
		t.Fatal(err)
	}
	if err := catalog.SaveLive("probe", p.Chat, []catalog.Model{
		{ID: "text"},
		{ID: "eye", Images: true, ImageInput: imageInputBool(true)},
	}); err != nil {
		t.Fatal(err)
	}
	return s, u
}

func TestModelNotKnownToSeeIsGivenTheDescription(t *testing.T) {
	s, u := unknownEyed(t)
	cases := []struct{ path, body string }{
		{"/v1/responses", `{"model":"probe/text","input":[{"role":"user","content":[{"type":"input_text","text":"read"},{"type":"input_image","image_url":"data:image/png;base64,aGVsbG8="}]}]}`},
		{CodexPath + "/responses", `{"model":"probe/text","stream":true,"input":[{"role":"user","content":[{"type":"input_text","text":"read"},{"type":"input_image","image_url":"https://example.com/a.png"}]}]}`},
		{"/v1/chat/completions", `{"model":"probe/text","messages":[{"role":"user","content":[{"type":"text","text":"read"},{"type":"image_url","image_url":{"url":"data:image/png;base64,aGVsbG8="}}]}]}`},
	}
	for _, tc := range cases {
		rec := httptest.NewRecorder()
		s.Handler().ServeHTTP(rec, httptest.NewRequest("POST", tc.path, strings.NewReader(tc.body)))
		if rec.Code != 200 {
			t.Errorf("%s: %d %s", tc.path, rec.Code, rec.Body.String())
		}
	}
	got := u.sent("text")
	if len(got) != len(cases) {
		t.Fatalf("text model sent %d requests, want %d", len(got), len(cases))
	}
	for i, b := range got {
		if !strings.Contains(b, "HELLO") || strings.Contains(b, "aGVsbG8=") || strings.Contains(b, "example.com/a.png") {
			t.Errorf("%s: text model was sent %s", cases[i].path, b)
		}
	}
	if n := len(u.sent("eye")); n != 2 {
		t.Fatalf("the describer was asked %d times, want 2 (two images)", n)
	}
}

// The description is in the Routing view as a call of its own, on the
// model that described, for the request and in its session.
func TestDescriptionIsInRouting(t *testing.T) {
	s, _ := eyed(t)
	req := httptest.NewRequest("POST", CodexPath+"/responses", strings.NewReader(`{"model":"probe/text","stream":true,"input":[{"role":"user","content":[{"type":"input_text","text":"read"},{"type":"input_image","image_url":"data:image/png;base64,aGVsbG8="}]}]}`))
	req.Header.Set("session_id", "s-1")
	req.Header.Set("User-Agent", "codex_cli_rs/0.50.0")
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("%d %s", rec.Code, rec.Body.String())
	}
	var vision, turn *Route
	for _, r := range s.trace.routes {
		switch r.Model {
		case "probe/eye":
			vision = r
		case "probe/text":
			turn = r
		}
	}
	if vision == nil || turn == nil {
		t.Fatalf("routes: vision %v, turn %v", vision, turn)
	}
	if vision.Kind != "vision" || vision.For == nil || vision.For.Model != "probe/text" || vision.For.Agent != turn.Agent || vision.Session != turn.Session || turn.Session == "" {
		t.Fatalf("vision route: kind %q for %+v session %q; turn agent %q session %q", vision.Kind, vision.For, vision.Session, turn.Agent, turn.Session)
	}
}

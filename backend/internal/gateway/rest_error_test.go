package gateway

import (
	"encoding/json"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// A rest after a failure that names no kind ("failed") carries what the
// vendor said, kept with the rest and sent to the app, which shows it
// beside the row (GanHei37443 on X: three WorkBuddy accounts each
// "failed · back in 16s", and nothing on the page said why).
func TestRestCarriesTheVendorsWords(t *testing.T) {
	clineRestsCleared(t)
	s := &Server{}
	p := provider.Provider{ID: "custom", Chat: "https://relay.example/v1", Key: "k"}
	c := candidate{p: p, model: "deepseek-v4.1-flash", rest: p.ID}

	r := s.restAfter(c, 500, nil, []byte(`{"error":{"message":"model deepseek-v4.1-flash is not available for your plan","type":"server_error"}}`))
	if r.Why != failOther || r.Status != 500 {
		t.Fatalf("rests %s at %d, want a plain failure at 500", r.Why, r.Status)
	}
	if !strings.Contains(r.Error, "not available for your plan") {
		t.Fatalf("the rest says %q, not the vendor's words", r.Error)
	}
	kept, ok := restOf(p.ID)
	if !ok || kept.Error != r.Error {
		t.Fatalf("the kept rest says %q (resting %v), want %q", kept.Error, ok, r.Error)
	}
	b, _ := json.Marshal(r)
	if !strings.Contains(string(b), `"error":"`) {
		t.Fatalf("the app isn't sent the words: %s", b)
	}

	// a body with no words of its own says its status; a long one is cut
	// short on a rune, the request's details keeping it whole
	if r := s.restAfter(c, 502, nil, nil); !strings.HasPrefix(r.Error, "Bad Gateway") {
		t.Errorf("an empty 502 rests saying %q, want its status text", r.Error)
	}
	long := strings.Repeat("服务", 400)
	r = s.restAfter(c, 500, nil, []byte(`{"error":{"message":"`+long+`"}}`))
	if n := utf8.RuneCountInString(r.Error); n > restErrorRunes+1 || n < restErrorRunes || !utf8.ValidString(r.Error) {
		t.Errorf("a long error is kept as %d runes (valid %v), want %d", n, utf8.ValidString(r.Error), restErrorRunes)
	}
}

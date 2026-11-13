package provider

import (
	"context"
	"net/http"
	"slices"
	"testing"
)

// A provider's anthropic-beta adds to the request's, in any case, after it
// and without repeats; any other header of the user's replaces the request's.
func TestSignMergesListHeaders(t *testing.T) {
	p := Provider{ID: "relay", Key: "k", Anthropic: "https://relay.example/anthropic",
		Headers: map[string]string{"anthropic-beta": "b, ,a,c", "X-Thing": "mine"}}
	req, _ := http.NewRequest("POST", "https://relay.example/anthropic/v1/messages", nil)
	req.Header.Set("Anthropic-Beta", "a,b")
	req.Header.Set("X-Thing", "theirs")
	if err := p.Sign(context.Background(), req, Anthropic, nil); err != nil {
		t.Fatal(err)
	}
	if got := req.Header["anthropic-beta"]; !slices.Equal(got, []string{"a,b,c"}) {
		t.Fatalf("anthropic-beta = %q, want a,b,c", got)
	}
	if _, ok := req.Header["Anthropic-Beta"]; ok {
		t.Fatalf("the request's own line was kept beside the merged one: %v", req.Header)
	}
	if got := req.Header["X-Thing"]; !slices.Equal(got, []string{"mine"}) {
		t.Fatalf("X-Thing = %q, want the user's", got)
	}
}

package provider

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/catalog"
)

// What Aloha's service answered on Discord when sent both key headers.
const bothRefusedZH = `{"error":{"type":"authentication_error","message":"鉴权 header， x-api-key 和 Authorization 不可以同时存在"}}`

// oneHeaderOnly is a vendor that turns away a request carrying the key in
// both x-api-key and Authorization with the body it is given, and takes
// either alone; asked records the headers of each request.
func oneHeaderOnly(t *testing.T, refusal string) (*httptest.Server, func() [][2]string) {
	t.Helper()
	var mu sync.Mutex
	var asked [][2]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		x, a := r.Header.Get("x-api-key"), r.Header.Get("Authorization")
		mu.Lock()
		asked = append(asked, [2]string{x, a})
		mu.Unlock()
		switch {
		case x != "" && a != "":
			w.WriteHeader(http.StatusUnauthorized)
			w.Write([]byte(refusal))
		case x != "k" && a != "Bearer k":
			w.WriteHeader(http.StatusUnauthorized)
			w.Write([]byte(`{"error":{"message":"invalid key"}}`))
		case strings.HasSuffix(r.URL.Path, "/models"):
			w.Write([]byte(`{"data":[{"id":"claude-haiku-4-5"}]}`))
		default:
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"id":"msg_1","type":"message","role":"assistant","content":[{"type":"text","text":"hi"}]}`))
		}
	}))
	t.Cleanup(srv.Close)
	return srv, func() [][2]string { mu.Lock(); defer mu.Unlock(); return append([][2]string(nil), asked...) }
}

// A third-party Anthropic API is sent both headers unless the provider says
// one; Anthropic's own and Bedrock's get x-api-key alone, whatever it says.
func TestAuthHeaderChoice(t *testing.T) {
	relay := Provider{ID: "relay", Anthropic: "https://relay.example.com", Key: "k"}
	if h := AuthHeaders(relay, Anthropic); len(h) != 2 || h["x-api-key"] != "k" || h["Authorization"] != "Bearer k" {
		t.Errorf("default: %v", h)
	}
	relay.AuthHeader = AuthXAPIKey
	if h := AuthHeaders(relay, Anthropic); len(h) != 1 || h["x-api-key"] != "k" {
		t.Errorf("x-api-key: %v", h)
	}
	relay.AuthHeader = AuthBearer
	if h := AuthHeaders(relay, Anthropic); len(h) != 1 || h["Authorization"] != "Bearer k" {
		t.Errorf("bearer: %v", h)
	}
	// Chat is Bearer as ever
	relay.Chat = "https://relay.example.com/v1"
	if h := AuthHeaders(relay, Chat); len(h) != 1 || h["Authorization"] != "Bearer k" {
		t.Errorf("chat: %v", h)
	}
	for _, choice := range AuthChoices {
		official := Provider{ID: "anthropic", Preset: "anthropic", Anthropic: "https://api.anthropic.com", Key: "k", AuthHeader: choice}
		if h := AuthHeaders(official, Anthropic); len(h) != 1 || h["x-api-key"] != "k" {
			t.Errorf("official with %q: %v", choice, h)
		}
		if official.BothAuth() {
			t.Errorf("official counted as sending both")
		}
		bedrock, _ := legacyVendor("bedrock")
		bedrock.Key, bedrock.AuthHeader = "k", choice
		if h := AuthHeaders(bedrock, Anthropic); len(h) != 1 || h["x-api-key"] != "k" {
			t.Errorf("bedrock with %q: %v", choice, h)
		}
	}
	if !(Provider{Anthropic: "https://relay.example.com", Key: "k", AuthHeader: AuthBearer}).BothAuth() {
		t.Error("a relay with a pick still counts as one that sends both by default")
	}
	if p := normalize(Provider{ID: "x", Anthropic: "https://relay.example.com", AuthHeader: "both-please"}); p.AuthHeader != AuthBoth {
		t.Errorf("unknown pick kept: %q", p.AuthHeader)
	}
}

// The refusal is told by its words, in Chinese and English, and only on a
// 401 naming both headers.
func TestBothAuthRefused(t *testing.T) {
	for _, b := range []string{
		bothRefusedZH,
		`{"error":"x-api-key 和 Authorization 不能同时存在"}`,
		`{"error":{"message":"x-api-key and Authorization cannot both be present"}}`,
		`{"message":"Only one of x-api-key or Authorization header may be provided"}`,
		`{"message":"Headers x-api-key and Authorization are mutually exclusive"}`,
	} {
		if !catalog.BothAuthRefused(401, []byte(b)) {
			t.Errorf("not told: %s", b)
		}
	}
	for _, c := range []struct {
		status int
		body   string
	}{
		{401, `{"error":{"message":"invalid x-api-key"}}`},
		{401, `{"error":{"message":"Authorization failed"}}`},
		{400, bothRefusedZH},
		{403, bothRefusedZH},
	} {
		if catalog.BothAuthRefused(c.status, []byte(c.body)) {
			t.Errorf("told: %d %s", c.status, c.body)
		}
	}
}

// Aloha's case: a test of a relay that refuses both headers together asks
// again with x-api-key alone, passes, and the provider keeps x-api-key;
// its next requests go so from the first.
func TestRetryOneAuthLearns(t *testing.T) {
	azureHome(t)
	srv, asked := oneHeaderOnly(t, bothRefusedZH)
	if err := Save(Provider{ID: "relay", Name: "Relay", Anthropic: srv.URL, Key: "k", Models: []string{"claude-haiku-4-5"}}); err != nil {
		t.Fatal(err)
	}
	p, _ := Find("relay")
	rs := p.TestModels(context.Background(), []string{"claude-haiku-4-5"})
	if len(rs) != 1 || !rs[0].OK {
		t.Fatalf("test: %+v", rs)
	}
	got := asked()
	if len(got) != 2 || got[0] != [2]string{"k", "Bearer k"} || got[1] != [2]string{"k", ""} {
		t.Errorf("asked %v", got)
	}
	q, _ := Find("relay")
	if q.AuthHeader != AuthXAPIKey {
		t.Fatalf("kept %q", q.AuthHeader)
	}
	// the model list goes with it too
	if _, err := q.Fetch(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := asked()[2:]; len(got) == 0 || got[0] != [2]string{"k", ""} {
		t.Errorf("list asked %v", got)
	}
}

// One taking Bearer alone: x-api-key alone is tried first, then Bearer,
// which is kept. One the user picked is never changed.
func TestRetryOneAuthBearer(t *testing.T) {
	azureHome(t)
	var mu sync.Mutex
	n := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		n++
		mu.Unlock()
		x, a := r.Header.Get("x-api-key"), r.Header.Get("Authorization")
		if x != "" && a != "" {
			w.WriteHeader(401)
			w.Write([]byte(`{"error":{"message":"x-api-key and Authorization headers cannot both be sent"}}`))
			return
		}
		if a != "Bearer k" {
			w.WriteHeader(401)
			w.Write([]byte(`{"error":{"message":"missing bearer"}}`))
			return
		}
		w.Write([]byte(`{"id":"msg_1","type":"message","content":[{"type":"text","text":"hi"}]}`))
	}))
	defer srv.Close()
	if err := Save(Provider{ID: "relay", Name: "Relay", Anthropic: srv.URL, Key: "k", Models: []string{"claude-haiku-4-5"}}); err != nil {
		t.Fatal(err)
	}
	p, _ := Find("relay")
	if rs := p.TestModels(context.Background(), []string{"claude-haiku-4-5"}); !rs[0].OK {
		t.Fatalf("test: %+v", rs)
	}
	if n != 3 {
		t.Errorf("asked %d times", n)
	}
	if q, _ := Find("relay"); q.AuthHeader != AuthBearer {
		t.Errorf("kept %q", q.AuthHeader)
	}

	// LearnAuthHeader leaves a pick of the user's alone
	q, _ := Find("relay")
	q.AuthHeader = AuthXAPIKey
	if err := Save(*q); err != nil {
		t.Fatal(err)
	}
	LearnAuthHeader("relay", AuthBearer)
	if q, _ := Find("relay"); q.AuthHeader != AuthXAPIKey {
		t.Errorf("user's pick changed to %q", q.AuthHeader)
	}
}

// The model list is the first thing asked of a new provider: refused for
// the two headers, it is asked again with x-api-key alone, and that is kept.
func TestListLearnsOneAuth(t *testing.T) {
	azureHome(t)
	srv, asked := oneHeaderOnly(t, bothRefusedZH)
	if err := Save(Provider{ID: "relay", Name: "Relay", Anthropic: srv.URL, Key: "k"}); err != nil {
		t.Fatal(err)
	}
	p, _ := Find("relay")
	ms, err := p.Fetch(context.Background())
	if err != nil || len(ms) == 0 {
		t.Fatalf("list: %v %v", ms, err)
	}
	if got := asked(); len(got) < 2 || got[0][1] == "" || got[1] != [2]string{"k", ""} {
		t.Errorf("asked %v", got)
	}
	if q, _ := Find("relay"); q.AuthHeader != AuthXAPIKey {
		t.Errorf("kept %q", q.AuthHeader)
	}
}

// Another 401 is handed on as it came, asked once; so is a 401 to a request
// whose Authorization is the user's own header.
func TestRetryOneAuthOnlyOnThatRefusal(t *testing.T) {
	azureHome(t)
	srv, asked := oneHeaderOnly(t, `{"error":{"message":"invalid api key"}}`)
	if err := Save(Provider{ID: "relay", Name: "Relay", Anthropic: srv.URL, Key: "k", Models: []string{"claude-haiku-4-5"}}); err != nil {
		t.Fatal(err)
	}
	p, _ := Find("relay")
	rs := p.TestModels(context.Background(), []string{"claude-haiku-4-5"})
	if rs[0].OK || !strings.Contains(rs[0].Error, "invalid api key") {
		t.Errorf("test: %+v", rs)
	}
	if got := asked(); len(got) != 1 {
		t.Errorf("asked %v", got)
	}
	if q, _ := Find("relay"); q.AuthHeader != AuthBoth {
		t.Errorf("kept %q", q.AuthHeader)
	}

	srv2, asked2 := oneHeaderOnly(t, bothRefusedZH)
	own := Provider{ID: "own", Name: "Own", Anthropic: srv2.URL, Key: "k", Headers: map[string]string{"Authorization": "Bearer theirs"}, Models: []string{"claude-haiku-4-5"}}
	if err := Save(own); err != nil {
		t.Fatal(err)
	}
	q, _ := Find("own")
	q.TestModels(context.Background(), []string{"claude-haiku-4-5"})
	if got := asked2(); len(got) != 1 {
		t.Errorf("own header asked %v", got)
	}
}

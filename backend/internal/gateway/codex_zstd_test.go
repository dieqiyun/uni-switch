package gateway

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/klauspost/compress/zstd"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// zstdTurns sends turns to the ChatGPT backend compressed, as the gateway
// does outside tests (TestMain leaves them plain for the other fakes).
func zstdTurns(t *testing.T) {
	t.Helper()
	t.Cleanup(provider.CodexZstdForTests(true))
}

// sentTurn is one request as the stand-in ChatGPT backend got it.
type sentTurn struct {
	path, encoding, account string
	wire                    int   // bytes on the wire
	length                  int64 // Content-Length said
	body                    []byte
}

// turnsBackend is a ChatGPT backend that does what ylorn's did (X,
// v0.1.1202): a plain body of 1 MiB or more is answered Envoy's 503, the
// same body zstd-compressed is served. drop more answers that 503 to a
// body whose wire bytes are 1 MiB or more, compressed or not, that many
// times. Every request is decoded and kept.
func turnsBackend(t *testing.T, drop int) (*[]sentTurn, *sync.Mutex) {
	t.Helper()
	var mu sync.Mutex
	var got []sentTurn
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		wire, _ := io.ReadAll(r.Body)
		s := sentTurn{path: r.URL.Path, encoding: r.Header.Get("Content-Encoding"), account: r.Header.Get("chatgpt-account-id"),
			wire: len(wire), length: r.ContentLength, body: wire}
		if s.encoding == "zstd" {
			d, _ := zstd.NewReader(nil)
			b, err := d.DecodeAll(wire, nil)
			d.Close()
			if err != nil {
				t.Errorf("a zstd body that doesn't decode: %v", err)
			}
			s.body = b
		}
		mu.Lock()
		got = append(got, s)
		dropping := len(wire) >= 1<<20 && (s.encoding == "" || drop > 0)
		if dropping && s.encoding != "" {
			drop--
		}
		mu.Unlock()
		if !json.Valid(s.body) {
			t.Errorf("not JSON as the backend reads it (%s): %.80q", s.encoding, s.body)
		}
		if dropping {
			w.Header().Set("Content-Type", "text/plain")
			w.Header().Set("Server", "cloudflare")
			w.WriteHeader(http.StatusServiceUnavailable)
			io.WriteString(w, edgeDropped)
			return
		}
		io.WriteString(w, sse(
			`data: {"type":"response.created","response":{"id":"r1","model":"gpt-6-luna"}}`,
			`data: {"type":"response.output_text.delta","delta":"pong"}`,
			`data: {"type":"response.completed","response":{"id":"r1","usage":{"input_tokens":7,"output_tokens":1}}}`))
	}))
	t.Cleanup(up.Close)
	was := provider.CodexBase
	provider.CodexBase = up.URL + "/backend-api/codex"
	t.Cleanup(func() { provider.CodexBase = was })
	return &got, &mu
}

// textTurn is ylorn's turn: text alone, about n bytes of it, no image.
func textTurn(n int) string {
	line := "The quick brown fox jumps over the lazy dog while uni-switch relays this text-only request. "
	b, _ := json.Marshal(map[string]any{"model": "gpt-6-luna", "stream": true, "input": []any{
		map[string]any{"type": "message", "role": "user", "content": []any{
			map[string]any{"type": "input_text", "text": strings.Repeat(line, n/len(line)+1)[:n]}}}}})
	return string(b)
}

// codexPostZstd posts body to the Codex backend path compressed, as Codex
// sends it.
func codexPostZstd(t *testing.T, path, body string) (int, string) {
	t.Helper()
	e, _ := zstd.NewWriter(nil)
	z := e.EncodeAll([]byte(body), nil)
	e.Close()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", CodexPath+path, bytes.NewReader(z))
	req.Header.Set("Content-Encoding", "zstd")
	req.Header.Set("Authorization", "Bearer chatgpt-token")
	req.Header.Set("chatgpt-account-id", "acct-1")
	New().Handler().ServeHTTP(rec, req)
	return rec.Code, rec.Body.String()
}

// checkZstd says what is wrong with a turn that should have gone as Codex
// sends one: zstd, its Content-Length the compressed bytes, and the JSON it
// holds asking for model.
func checkZstd(t *testing.T, s sentTurn, model string) {
	t.Helper()
	if s.encoding != "zstd" {
		t.Errorf("%s went %q, want zstd as Codex sends it (%d bytes on the wire)", s.path, s.encoding, s.wire)
		return
	}
	if s.length != int64(s.wire) {
		t.Errorf("Content-Length %d, %d bytes sent", s.length, s.wire)
	}
	var q struct {
		Model string `json:"model"`
	}
	if json.Unmarshal(s.body, &q); q.Model != model {
		t.Errorf("model %q in the decoded body, want %q", q.Model, model)
	}
}

// ylorn (X, v0.1.1202): a text-only gpt-6-luna turn of 1.1 MB through a
// ChatGPT account was answered 503 when sent plain, and 200 sent
// zstd-compressed as Codex's own client sends it. Through uni-switch's
// accounts it now goes compressed and is answered; on the old code every
// account was asked plain and the agent got the 503.
func TestCodexAccountTextTurnGoesZstd(t *testing.T) {
	zstdTurns(t)
	codexSignedIn(t, "spare@example.com")
	got, mu := turnsBackend(t, 0)

	code, body := codexPost(t, textTurn(1100224))
	if code != 200 || !strings.Contains(body, "pong") {
		t.Fatalf("status %d, want 200 pong: %.300s", code, body)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(*got) != 1 {
		t.Fatalf("asked %d times, want once", len(*got))
	}
	checkZstd(t, (*got)[0], "gpt-6-luna")
	if (*got)[0].wire >= 64<<10 {
		t.Errorf("%d bytes sent for 1.1 MB of repeated text", (*got)[0].wire)
	}
}

// With Codex's own sign-in alone the turn is relayed, and goes compressed
// again whether Codex sent it zstd (it does, which uni-switch reads) or plain.
func TestCodexRelayedTextTurnGoesZstd(t *testing.T) {
	zstdTurns(t)
	codexSignedIn(t)
	got, mu := turnsBackend(t, 0)

	for _, send := range []func(*testing.T, string, string) (int, string){
		codexPostZstd,
		func(t *testing.T, _ string, body string) (int, string) { return codexPost(t, body) },
	} {
		mu.Lock()
		*got = nil
		mu.Unlock()
		code, body := send(t, "/responses", textTurn(1100224))
		if code != 200 || !strings.Contains(body, "pong") {
			t.Fatalf("status %d, want 200 pong: %.300s", code, body)
		}
		mu.Lock()
		if len(*got) != 1 {
			t.Fatalf("asked %d times, want once", len(*got))
		}
		checkZstd(t, (*got)[0], "gpt-6-luna")
		mu.Unlock()
	}
}

// A turn asked again goes again whole: each try is the same JSON,
// compressed, with its own Content-Length. A screenshot's random bytes
// don't compress, so the turn is still past 1 MiB on the wire and the
// backend's reset of it is asked again (#1628), by the accounts and on
// the relay.
func TestCodexTurnAskedAgainGoesZstdAgain(t *testing.T) {
	zstdTurns(t)
	for _, spares := range [][]string{{"spare@example.com"}, nil} {
		codexSignedIn(t, spares...)
		got, mu := turnsBackend(t, 1)
		turn := screenshotTurn(t, 1600<<10)
		code, body := codexPost(t, turn)
		if code != 200 || !strings.Contains(body, "pong") {
			t.Fatalf("accounts %v: status %d, want 200 pong: %.300s", spares, code, body)
		}
		mu.Lock()
		if len(*got) != 2 {
			t.Fatalf("accounts %v: asked %d times, want 2", spares, len(*got))
		}
		for _, s := range *got {
			checkZstd(t, s, "gpt-5.5")
			if !bytes.Equal(s.body, (*got)[0].body) {
				t.Errorf("accounts %v: the try again sent another body", spares)
			}
		}
		mu.Unlock()
	}
}

// Codex sends only its turns compressed: /responses/compact goes plain, as
// does a Codex signed in with an API key, which goes to OpenAI's API.
func TestCodexCompactAndAPIKeyStayPlain(t *testing.T) {
	zstdTurns(t)
	codexSignedIn(t)
	got, mu := turnsBackend(t, 0)
	codexPostZstd(t, "/responses/compact", `{"model":"gpt-6-luna","input":"hi"}`)
	mu.Lock()
	if len(*got) != 1 || (*got)[0].path != "/backend-api/codex/responses/compact" || (*got)[0].encoding != "" {
		t.Errorf("compact went %+v, want plain", *got)
	}
	mu.Unlock()

	api := &fake{t: t}
	up := httptest.NewServer(api)
	t.Cleanup(up.Close)
	was := codexAPIBase
	codexAPIBase = up.URL + "/v1"
	t.Cleanup(func() { codexAPIBase = was })
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", CodexPath+"/responses", strings.NewReader(`{"model":"gpt-6-luna","stream":true,"input":"hi"}`))
	req.Header.Set("Authorization", "Bearer sk-test")
	New().Handler().ServeHTTP(rec, req)
	if api.path != "/v1/responses" || api.head.Get("Content-Encoding") != "" || !json.Valid(api.got) {
		t.Errorf("an API key's turn went %s %q: %.80q", api.path, api.head.Get("Content-Encoding"), api.got)
	}
}

// Another provider's Responses requests go plain, even one at the very
// address a ChatGPT account's turns go to: only a ChatGPT account's are
// compressed.
func TestKeyProviderTurnStaysPlain(t *testing.T) {
	zstdTurns(t)
	f := &fake{t: t, reply: sse(
		`data: {"type":"response.created","response":{"id":"r1","model":"m1"}}`,
		`data: {"type":"response.completed","response":{"id":"r1","usage":{"input_tokens":7,"output_tokens":1}}}`)}
	up := setup(t, provider.Responses, f)
	was := provider.CodexBase
	provider.CodexBase = up.URL + "/v1"
	t.Cleanup(func() { provider.CodexBase = was })

	code, body := post(t, "/v1/responses", `{"model":"fake/m1","stream":true,"input":"ping"}`)
	if code != 200 {
		t.Fatalf("status %d: %s", code, body)
	}
	if f.path != "/v1/responses" || f.head.Get("Content-Encoding") != "" || !json.Valid(f.got) {
		t.Errorf("a key's turn went %s %q: %.80q", f.path, f.head.Get("Content-Encoding"), f.got)
	}
}

// Whether a reset is a large request's (#1628) is judged by what went on
// the wire: a 1.1 MB text turn goes as a few KB, so the backend closing on
// it is its trouble as for any small request, relayed as the 503 it is
// after one try and not told as the request's size.
func TestCodexResetJudgedByTheBytesSent(t *testing.T) {
	zstdTurns(t)
	codexSignedIn(t)
	tries := 0
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tries++
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusServiceUnavailable)
		io.WriteString(w, edgeDropped)
	}))
	t.Cleanup(up.Close)
	was := provider.CodexBase
	provider.CodexBase = up.URL + "/backend-api/codex"
	t.Cleanup(func() { provider.CodexBase = was })

	code, body := codexPost(t, textTurn(1100224))
	if code != http.StatusServiceUnavailable || strings.Contains(body, "MiB") || tries != 1 {
		t.Fatalf("status %d after %d tries: %s", code, tries, body)
	}
}

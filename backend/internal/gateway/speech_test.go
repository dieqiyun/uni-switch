package gateway

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
	"github.com/dieqiyun/uni-switch/backend/internal/usage"
)

// mp3 is audio as a vendor sends it: bytes no text has.
var mp3 = append([]byte("ID3\x04\x00\x00\x00\x00\x00\x00\xff\xfb\x90\x64"), bytes.Repeat([]byte{0, 0xff, 0xfb, '{', '}', '\r', '\n'}, 2048)...)

// speaker is a vendor with OpenAI's text-to-speech API. What it was sent
// is kept; an SSE answer sends its first event, then waits for more to be
// let through, so a test can see the first reach the client before the
// rest is written.
type speaker struct {
	mu      sync.Mutex
	sent    [][]byte
	accepts []string
	more    chan struct{}
}

func (sp *speaker) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	sp.mu.Lock()
	sp.sent = append(sp.sent, body)
	sp.accepts = append(sp.accepts, r.Header.Get("Accept"))
	sp.mu.Unlock()
	if r.Header.Get("Authorization") != "Bearer key" {
		w.WriteHeader(401)
		return
	}
	if r.URL.Path != "/v1/audio/speech" {
		w.WriteHeader(404)
		return
	}
	var req map[string]any
	json.Unmarshal(body, &req)
	switch {
	case req["model"] == "refused":
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(400)
		io.WriteString(w, `{"error":{"message":"voice nova isn't one of this model's"}}`)
	case req["stream_format"] == "sse":
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, "data: {\"type\":\"speech.audio.delta\",\"audio\":\"SUQz\"}\n\n")
		w.(http.Flusher).Flush()
		select {
		case <-sp.more:
		case <-time.After(30 * time.Second):
		}
		io.WriteString(w, "data: {\"type\":\"speech.audio.delta\",\"audio\":\"//uQ\"}\n\n")
		io.WriteString(w, "data: {\"type\":\"speech.audio.done\",\"usage\":{\"input_tokens\":12,\"output_tokens\":340,\"total_tokens\":352}}\n\n")
	case req["voice_id"] != nil:
		// Mistral's: the audio as base64 in JSON
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"audio_data":"UklGRg=="}`)
	default:
		w.Header().Set("Content-Type", "audio/mpeg")
		w.Write(mp3)
	}
}

func (sp *speaker) got() ([][]byte, []string) {
	sp.mu.Lock()
	defer sp.mu.Unlock()
	return append([][]byte(nil), sp.sent...), append([]string(nil), sp.accepts...)
}

func speakerUp(t *testing.T) (*Server, *speaker) {
	t.Helper()
	fresh(t)
	sp := &speaker{more: make(chan struct{})}
	up := httptest.NewServer(sp)
	t.Cleanup(up.Close)
	if err := provider.Save(provider.Provider{ID: "voice", Name: "Voice", Chat: up.URL + "/v1", Key: "key", Models: []string{"gpt-4o-mini-tts", "refused"}}); err != nil {
		t.Fatal(err)
	}
	return New(), sp
}

func postSpeech(t *testing.T, s *Server, path, body string) (int, http.Header, []byte) {
	t.Helper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	s.Handler().ServeHTTP(rec, req)
	return rec.Code, rec.Header(), rec.Body.Bytes()
}

// A speech request goes to the model's provider with only its model
// rewritten, every other field as sent, and the vendor's audio comes back
// byte for byte in its own type; it is in the usage ledger. The bare
// /audio/speech path an SDK given uni-switch's root posts to works too.
func TestSpeechGoesToTheModelsProvider(t *testing.T) {
	s, sp := speakerUp(t)
	body := `{"model":"voice/gpt-4o-mini-tts","input":"Hello from the uni-switch.","voice":"coral","response_format":"mp3","speed":1.25,"instructions":"Speak cheerfully."}`
	code, h, got := postSpeech(t, s, "/v1/audio/speech", body)
	if code != 200 || !bytes.Equal(got, mp3) || h.Get("Content-Type") != "audio/mpeg" {
		t.Fatalf("%d %q, %d bytes back of %d", code, h.Get("Content-Type"), len(got), len(mp3))
	}
	sent, accepts := sp.got()
	if len(sent) != 1 || accepts[0] == "application/json" {
		t.Fatalf("vendor was sent %d bodies, Accept %v", len(sent), accepts)
	}
	var vendor, asked map[string]any
	json.Unmarshal(sent[0], &vendor)
	json.Unmarshal([]byte(body), &asked)
	asked["model"] = "gpt-4o-mini-tts"
	a, _ := json.Marshal(asked)
	v, _ := json.Marshal(vendor)
	if !bytes.Equal(a, v) {
		t.Fatalf("vendor was sent %s", sent[0])
	}
	var recs []usage.Record
	for i := 0; i < 50; i++ {
		if recs = usage.Load(time.Time{}); len(recs) >= 1 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if len(recs) != 1 || recs[0].Operation != "speech" || recs[0].Provider != "voice" || recs[0].Model != "gpt-4o-mini-tts" || recs[0].Requested != "voice/gpt-4o-mini-tts" || recs[0].Status != 200 {
		t.Fatalf("usage %+v", recs)
	}
	// the bare path, and a field only one vendor has (Mistral's voice_id)
	// with its JSON answer passed on as JSON
	code, h, got = postSpeech(t, s, "/audio/speech", `{"model":"voice/gpt-4o-mini-tts","input":"hi","voice_id":"gb_jane_neutral","response_format":"wav"}`)
	if code != 200 || string(got) != `{"audio_data":"UklGRg=="}` || h.Get("Content-Type") != "application/json" {
		t.Fatalf("Mistral's shape: %d %q %s", code, h.Get("Content-Type"), got)
	}
	if sent, _ = sp.got(); len(sent) != 2 || !bytes.Contains(sent[1], []byte(`"voice_id":"gb_jane_neutral"`)) || !bytes.Contains(sent[1], []byte(`"model":"gpt-4o-mini-tts"`)) {
		t.Fatalf("vendor was sent %s", sent[len(sent)-1])
	}
}

// stream_format sse: each event reaches the client as the vendor sends
// it, before the vendor has sent the rest, and the done event's usage is
// counted.
func TestSpeechStreamsAsItComes(t *testing.T) {
	s, sp := speakerUp(t)
	gw := httptest.NewServer(s.Handler())
	t.Cleanup(gw.Close)
	res, err := http.Post(gw.URL+"/v1/audio/speech", "application/json", strings.NewReader(`{"model":"voice/gpt-4o-mini-tts","input":"hi","voice":"coral","stream_format":"sse"}`))
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != 200 || res.Header.Get("Content-Type") != "text/event-stream" {
		t.Fatalf("%d %q", res.StatusCode, res.Header.Get("Content-Type"))
	}
	rd := bufio.NewReader(res.Body)
	first := make(chan string, 1)
	go func() {
		l, _ := rd.ReadString('\n')
		first <- l
	}()
	select {
	case l := <-first:
		if !strings.Contains(l, `"audio":"SUQz"`) {
			t.Fatalf("first line %q", l)
		}
	case <-time.After(20 * time.Second):
		close(sp.more)
		t.Fatal("the first event didn't reach the client while the vendor held the rest: the answer was held whole")
	}
	close(sp.more)
	rest, _ := io.ReadAll(rd)
	if !bytes.Contains(rest, []byte(`"audio":"//uQ"`)) || !bytes.Contains(rest, []byte("speech.audio.done")) {
		t.Fatalf("rest %q", rest)
	}
	var recs []usage.Record
	for i := 0; i < 100; i++ {
		if recs = usage.Load(time.Time{}); len(recs) >= 1 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if len(recs) != 1 || recs[0].Operation != "speech" || recs[0].Input != 12 || recs[0].Output != 340 {
		t.Fatalf("usage %+v", recs)
	}
}

// A vendor's refusal comes back as an error with its status and message.
func TestSpeechVendorRefusalPassedOn(t *testing.T) {
	s, _ := speakerUp(t)
	code, _, got := postSpeech(t, s, "/v1/audio/speech", `{"model":"voice/refused","input":"hi","voice":"nova"}`)
	if code != 400 || !strings.Contains(string(got), "voice nova isn't one of this model's") {
		t.Fatalf("%d %s", code, got)
	}
}

// A request uni-switch can't route is told why, and nothing reaches a vendor.
func TestSpeechRefusals(t *testing.T) {
	s, sp := speakerUp(t)
	if code, _, got := postSpeech(t, s, "/v1/audio/speech", `not json`); code != 400 || !strings.Contains(string(got), "JSON object") {
		t.Fatalf("not json: %d %s", code, got)
	}
	if code, _, got := postSpeech(t, s, "/v1/audio/speech", `{"input":"hi"}`); code != 400 || !strings.Contains(string(got), "name the model") {
		t.Fatalf("no model: %d %s", code, got)
	}
	if code, _, got := postSpeech(t, s, "/v1/audio/speech", `{"model":"nobody/tts-1","input":"hi"}`); code != 404 || !strings.Contains(string(got), "nobody/tts-1") {
		t.Fatalf("unknown: %d %s", code, got)
	}
	if sent, _ := sp.got(); len(sent) != 0 {
		t.Fatalf("vendor was sent %d", len(sent))
	}
}

// Speech text and instructions reach the provider exactly as supplied.
func TestSpeechInputPreserved(t *testing.T) {
	s, sp := speakerUp(t)
	secret := "sk-ant-api03-" + strings.Repeat("Ab3x", 20)
	other := "sk-ant-api03-" + strings.Repeat("Zq9w", 20)
	body, _ := json.Marshal(map[string]any{"model": "voice/gpt-4o-mini-tts", "input": "Read this: " + secret, "instructions": "never say " + other, "voice": "coral"})
	if code, _, got := postSpeech(t, s, "/v1/audio/speech", string(body)); code != 200 || !bytes.Equal(got, mp3) {
		t.Fatalf("%d, %d bytes", code, len(got))
	}
	sent, _ := sp.got()
	if len(sent) != 1 || !bytes.Contains(sent[0], []byte(secret)) || !bytes.Contains(sent[0], []byte(other)) {
		t.Fatalf("vendor did not receive the original text: %s", sent)
	}
	var vendor map[string]string
	json.Unmarshal(sent[0], &vendor)
	if !strings.HasPrefix(vendor["input"], "Read this: ") || !strings.HasPrefix(vendor["instructions"], "never say ") || vendor["voice"] != "coral" || vendor["model"] != "gpt-4o-mini-tts" {
		t.Fatalf("vendor was sent %v", vendor)
	}
}

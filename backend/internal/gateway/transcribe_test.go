package gateway

import (
	"bytes"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
	"github.com/dieqiyun/uni-switch/backend/internal/usage"
)

// scribe is a vendor with OpenAI's speech-to-text APIs; what each path was
// sent, and with which Content-Type, is kept.
type scribe struct {
	mu    sync.Mutex
	sent  map[string][][]byte
	types map[string][]string
}

func (sc *scribe) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	sc.mu.Lock()
	if sc.sent == nil {
		sc.sent, sc.types = map[string][][]byte{}, map[string][]string{}
	}
	sc.sent[r.URL.Path] = append(sc.sent[r.URL.Path], body)
	sc.types[r.URL.Path] = append(sc.types[r.URL.Path], r.Header.Get("Content-Type"))
	sc.mu.Unlock()
	if r.Header.Get("Authorization") != "Bearer key" {
		w.WriteHeader(401)
		return
	}
	switch r.URL.Path {
	case "/v1/audio/transcriptions", "/v1/audio/translations":
	default:
		w.WriteHeader(404)
		return
	}
	r.Body = io.NopCloser(bytes.NewReader(body)) // read once above
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		w.WriteHeader(400)
		io.WriteString(w, `{"error":{"message":"`+err.Error()+`"}}`)
		return
	}
	// what OpenAI answers: JSON by default, the bare text when asked
	if r.FormValue("response_format") == "text" {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		io.WriteString(w, "Hello from the uni-switch.\n")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	io.WriteString(w, `{"text":"Hello from the uni-switch.","usage":{"type":"tokens","input_tokens":14,"input_token_details":{"text_tokens":0,"audio_tokens":14},"output_tokens":7,"total_tokens":21}}`)
}

func (sc *scribe) got(path string) ([][]byte, []string) {
	sc.mu.Lock()
	defer sc.mu.Unlock()
	return append([][]byte(nil), sc.sent[path]...), append([]string(nil), sc.types[path]...)
}

func scribed(t *testing.T) (*Server, *scribe) {
	t.Helper()
	fresh(t)
	sc := &scribe{}
	up := httptest.NewServer(sc)
	t.Cleanup(up.Close)
	if err := provider.Save(provider.Provider{ID: "ears", Name: "Ears", Chat: up.URL + "/v1", Key: "key", Models: []string{"whisper-1"}}); err != nil {
		t.Fatal(err)
	}
	return New(), sc
}

// voice is audio as a recorder writes it: bytes no text field has, a
// stray CRLF and dashes among them.
var voice = append([]byte("\x1aE\xdf\xa3\x9fB\x86\x81\x01webm\x00\x00\r\n--\xff\xfe"), bytes.Repeat([]byte{0, 0x81, 0x7f, '\r', '\n', '-'}, 512)...)

// audioForm builds the form as curl -F sends it.
func audioFormBody(t *testing.T, fields ...string) ([]byte, string) {
	t.Helper()
	var b bytes.Buffer
	mw := multipart.NewWriter(&b)
	if err := mw.SetBoundary("------------------------d74496d66958873e"); err != nil {
		t.Fatal(err)
	}
	h := make(map[string][]string)
	h["Content-Disposition"] = []string{`form-data; name="file"; filename="voice.webm"`}
	h["Content-Type"] = []string{"audio/webm"}
	fw, err := mw.CreatePart(h)
	if err != nil {
		t.Fatal(err)
	}
	fw.Write(voice)
	for i := 0; i+1 < len(fields); i += 2 {
		mw.WriteField(fields[i], fields[i+1])
	}
	mw.Close()
	return b.Bytes(), mw.FormDataContentType()
}

func postAudio(t *testing.T, s *Server, path, ctype string, body []byte) (int, http.Header, string) {
	t.Helper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", path, bytes.NewReader(body))
	req.Header.Set("Content-Type", ctype)
	s.Handler().ServeHTTP(rec, req)
	return rec.Code, rec.Header(), rec.Body.String()
}

// formOf reads a form the vendor was sent.
func formOf(t *testing.T, ctype string, body []byte) (map[string]string, []byte) {
	t.Helper()
	_, params, err := mime.ParseMediaType(ctype)
	if err != nil {
		t.Fatal(err)
	}
	f, err := multipart.NewReader(bytes.NewReader(body), params["boundary"]).ReadForm(32 << 20)
	if err != nil {
		t.Fatal(err)
	}
	vals := map[string]string{}
	for k, v := range f.Value {
		vals[k] = v[0]
	}
	var audio []byte
	if fh := f.File["file"]; len(fh) == 1 {
		rd, _ := fh[0].Open()
		audio, _ = io.ReadAll(rd)
		vals["filename"] = fh[0].Filename
		vals["file type"] = fh[0].Header.Get("Content-Type")
	}
	return vals, audio
}

// #2053: a transcription goes to the model's provider as the client sent
// it, the model its own id and the audio byte for byte under the same
// boundary, and the vendor's answer comes back; it is in the usage ledger.
func TestTranscriptionGoesToTheModelsProvider(t *testing.T) {
	s, sc := scribed(t)
	body, ctype := audioFormBody(t, "model", "ears/whisper-1", "language", "zh", "prompt", "uni-switch", "response_format", "json", "temperature", "0")
	code, _, raw := postAudio(t, s, "/v1/audio/transcriptions", ctype, body)
	if code != 200 || !strings.Contains(raw, `"text":"Hello from the uni-switch."`) {
		t.Fatalf("%d %s", code, raw)
	}
	sent, types := sc.got("/v1/audio/transcriptions")
	if len(sent) != 1 || types[0] != ctype {
		t.Fatalf("vendor was sent %d bodies as %v, not %s", len(sent), types, ctype)
	}
	vals, audio := formOf(t, types[0], sent[0])
	if !bytes.Equal(audio, voice) || vals["filename"] != "voice.webm" || vals["file type"] != "audio/webm" {
		t.Fatalf("audio arrived as %d bytes %q (%s), not the %d sent", len(audio), vals["filename"], vals["file type"], len(voice))
	}
	if vals["model"] != "whisper-1" || vals["language"] != "zh" || vals["prompt"] != "uni-switch" || vals["response_format"] != "json" || vals["temperature"] != "0" {
		t.Fatalf("vendor's form %v", vals)
	}
	var recs []usage.Record
	for i := 0; i < 50; i++ {
		if recs = usage.Load(time.Time{}); len(recs) >= 1 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if len(recs) != 1 || recs[0].Operation != "transcriptions" || recs[0].Provider != "ears" || recs[0].Model != "whisper-1" || recs[0].Requested != "ears/whisper-1" || recs[0].Input != 14 || recs[0].Output != 7 || recs[0].Status != 200 {
		t.Fatalf("usage %+v", recs)
	}
}

// A form whose model is the vendor's own id goes as it came, every byte;
// the bare /audio/... path an SDK given uni-switch's root posts to as well,
// and translations alike.
func TestTranscriptionFormGoesAsSent(t *testing.T) {
	s, sc := scribed(t)
	body, ctype := audioFormBody(t, "model", "whisper-1")
	for _, path := range []string{"/v1/audio/transcriptions", "/audio/transcriptions", "/v1/audio/translations", "/audio/translations"} {
		if code, _, raw := postAudio(t, s, path, ctype, body); code != 200 {
			t.Fatalf("%s: %d %s", path, code, raw)
		}
	}
	for _, path := range []string{"/v1/audio/transcriptions", "/v1/audio/translations"} {
		sent, types := sc.got(path)
		if len(sent) != 2 {
			t.Fatalf("%s: vendor was sent %d bodies", path, len(sent))
		}
		for i := range sent {
			if !bytes.Equal(sent[i], body) || types[i] != ctype {
				t.Fatalf("%s: vendor was sent a body of its own (%s)", path, types[i])
			}
		}
	}
}

// response_format text, srt or vtt comes back as the vendor sent it, in
// its own type, not as JSON.
func TestTranscriptAsTextKeepsItsType(t *testing.T) {
	s, _ := scribed(t)
	body, ctype := audioFormBody(t, "model", "ears/whisper-1", "response_format", "text")
	code, h, raw := postAudio(t, s, "/v1/audio/transcriptions", ctype, body)
	if code != 200 || raw != "Hello from the uni-switch.\n" || h.Get("Content-Type") != "text/plain; charset=utf-8" {
		t.Fatalf("%d %q %q", code, h.Get("Content-Type"), raw)
	}
}

// A request uni-switch can't route is told why, and nothing reaches a vendor.
func TestTranscriptionRefusals(t *testing.T) {
	s, sc := scribed(t)
	if err := provider.Save(provider.Provider{ID: "anth", Name: "Anth", Anthropic: "http://127.0.0.1:9/", Key: "key", Models: []string{"m"}}); err != nil {
		t.Fatal(err)
	}
	body, _ := audioFormBody(t, "model", "ears/whisper-1")
	if code, _, raw := postAudio(t, s, "/v1/audio/transcriptions", "application/json", []byte(`{"model":"ears/whisper-1"}`)); code != 400 || !strings.Contains(raw, "multipart/form-data") {
		t.Fatalf("json: %d %s", code, raw)
	}
	nameless, nctype := audioFormBody(t, "language", "en")
	if code, _, raw := postAudio(t, s, "/v1/audio/transcriptions", nctype, nameless); code != 400 || !strings.Contains(raw, "name the model") {
		t.Fatalf("no model: %d %s", code, raw)
	}
	none, ntype := audioFormBody(t, "model", "nobody/whisper-1")
	if code, _, raw := postAudio(t, s, "/v1/audio/transcriptions", ntype, none); code != 404 || !strings.Contains(raw, "nobody/whisper-1") {
		t.Fatalf("unknown: %d %s", code, raw)
	}
	anth, atype := audioFormBody(t, "model", "anth/m")
	if code, _, raw := postAudio(t, s, "/v1/audio/transcriptions", atype, anth); code != 400 || !strings.Contains(raw, "has no OpenAI-style API") {
		t.Fatalf("Anthropic-only: %d %s", code, raw)
	}
	// a boundary that isn't the body's
	if code, _, raw := postAudio(t, s, "/v1/audio/transcriptions", "multipart/form-data; boundary=elsewhere", body); code != 400 {
		t.Fatalf("wrong boundary: %d %s", code, raw)
	}
	if sent, _ := sc.got("/v1/audio/transcriptions"); len(sent) != 0 {
		t.Fatalf("vendor was sent %d", len(sent))
	}
}

// Transcription preserves the original prompt and audio bytes.
func TestTranscriptionPromptPreserved(t *testing.T) {
	s, sc := scribed(t)
	secret := "sk-ant-api03-" + strings.Repeat("Ab3x", 20)
	body, ctype := audioFormBody(t, "model", "whisper-1", "prompt", "key "+secret)
	if code, _, raw := postAudio(t, s, "/v1/audio/transcriptions", ctype, body); code != 200 {
		t.Fatalf("%d %s", code, raw)
	}
	sent, types := sc.got("/v1/audio/transcriptions")
	if len(sent) != 1 || !bytes.Contains(sent[0], []byte(secret)) {
		t.Fatalf("vendor did not receive the original prompt")
	}
	vals, audio := formOf(t, types[0], sent[0])
	if !bytes.Equal(audio, voice) || vals["model"] != "whisper-1" || !strings.HasPrefix(vals["prompt"], "key ") {
		t.Fatalf("vendor's form %v, %d audio bytes", vals, len(audio))
	}
}

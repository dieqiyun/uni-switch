package gateway

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// speak serves OpenAI's text-to-speech API: POST /v1/audio/speech. The
// body is JSON ({model, input, voice, response_format, speed,
// instructions, stream_format…}); it goes to the model's provider at its
// OpenAI-style base + path with only the model rewritten to the
// provider's own id. The answer is audio (mp3, opus, aac, flac, wav, pcm), SSE when
// stream_format is sse, or a vendor's own JSON (Mistral's audio_data):
// it comes back as it comes, in the vendor's own type, never held whole.
// It is routed, limited and recorded as an embeddings call is
// (serveRetrieval).
func (s *Server) speak(w http.ResponseWriter, r *http.Request) {
	body, admitted := s.requestBody(w, r, provider.Chat)
	if !admitted {
		return
	}
	start := time.Now()
	var req map[string]json.RawMessage
	if json.Unmarshal(body, &req) != nil || req == nil {
		writeError(w, provider.Chat, 400, "the body isn't a JSON object: /v1/audio/speech takes {\"model\", \"input\", \"voice\"…} as OpenAI's does")
		return
	}
	var asked string
	json.Unmarshal(req["model"], &asked)
	if asked == "" {
		writeError(w, provider.Chat, 400, "name the model: \"model\" is a uni-switch model id, provider/model (openai/gpt-4o-mini-tts, groq/playai-tts)")
		return
	}
	s.serveRetrieval(w, r, "/audio/speech", "speech", start, asked, &speechRequest{req: req})
}

// speechRequest is a text-to-speech JSON body.
type speechRequest struct{ req map[string]json.RawMessage }

func (sp *speechRequest) forModel(model string) ([]byte, string, error) {
	sp.req["model"], _ = json.Marshal(model)
	out, err := json.Marshal(sp.req)
	return out, "application/json", err
}

// answerType is the vendor's; OpenAI's default format is mp3.
func (sp *speechRequest) answerType(got string) string {
	if got == "" {
		return "audio/mpeg"
	}
	return got
}

// streams: the answer is copied to the client as it comes.
func (sp *speechRequest) streams() bool { return true }

// streamedRetrieval is a retrieval whose answer goes to the client as it comes,
// not read whole first (serveRetrieval).
type streamedRetrieval interface{ streams() bool }

// speechTail is how much of an answer's end is kept to read its usage:
// gpt-4o-mini-tts's SSE ends with a speech.audio.done event that has it.
const speechTail = 16 << 10

// copyAnswer copies an answer to w as it comes, flushing each read, and
// gives the tokens its last event says it counted, and the error that cut
// it short, if any.
func copyAnswer(w http.ResponseWriter, body io.Reader) (in, out int, err error) {
	rc := http.NewResponseController(w)
	var tail []byte
	buf := make([]byte, 32<<10)
	for {
		n, rerr := body.Read(buf)
		if n > 0 {
			if _, werr := w.Write(buf[:n]); werr != nil {
				return 0, 0, werr
			}
			rc.Flush()
			tail = append(tail, buf[:n]...)
			if len(tail) > speechTail {
				tail = append(tail[:0], tail[len(tail)-speechTail:]...)
			}
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			return 0, 0, rerr
		}
	}
	in, out = speechUsage(tail)
	return in, out, nil
}

// speechUsage is the usage an answer's last JSON line or SSE data line
// says: {"type":"speech.audio.done","usage":{"input_tokens":…}}. Audio
// bytes say none.
func speechUsage(tail []byte) (int, int) {
	lines := bytes.Split(tail, []byte("\n"))
	for i := len(lines) - 1; i >= 0; i-- {
		l := bytes.TrimSpace(lines[i])
		l = bytes.TrimSpace(bytes.TrimPrefix(l, []byte("data:")))
		if !bytes.Contains(l, []byte(`"usage"`)) {
			continue
		}
		var a struct {
			Usage struct {
				Input  int `json:"input_tokens"`
				Output int `json:"output_tokens"`
			} `json:"usage"`
		}
		if json.Unmarshal(l, &a) == nil {
			return a.Usage.Input, a.Usage.Output
		}
	}
	return 0, 0
}

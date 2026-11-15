package gateway

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"strings"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// transcribe serves OpenAI's speech-to-text APIs (#2053):
// POST /v1/audio/transcriptions and /v1/audio/translations. The body is the
// multipart form OpenAI's take (file, model, language, prompt,
// response_format…); it goes to the model's provider at its OpenAI-style
// base + path as the client sent it, boundary and all, and the vendor's
// answer comes back as it said it, in its own type (json, text, srt, vtt).
// Only the form's model is rewritten to the provider's own id. It is routed, limited and
// recorded as an embeddings call is (serveRetrieval).
func (s *Server) transcribe(path, operation string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		body, admitted := s.requestBody(w, r, provider.Chat)
		if !admitted {
			return
		}
		start := time.Now()
		a, err := readAudioForm(r.Header.Get("Content-Type"), body)
		if err != nil {
			writeError(w, provider.Chat, 400, err.Error())
			return
		}
		if a.model == "" {
			writeError(w, provider.Chat, 400, "name the model: the form's \"model\" is a uni-switch model id, provider/model (openai/whisper-1, groq/whisper-large-v3)")
			return
		}
		s.serveRetrieval(w, r, path, operation, start, a.model, a)
	}
}

// audioForm is a speech-to-text request: its multipart body as sent, and
// the fields uni-switch reads or may rewrite.
type audioForm struct {
	raw      []byte
	ctype    string // as sent, its boundary in it
	boundary string
	model    string
	prompt   string
}

// readAudioForm reads the form's model and prompt; the rest of it, the
// audio among it, is left as it is.
func readAudioForm(ctype string, body []byte) (*audioForm, error) {
	mt, params, err := mime.ParseMediaType(ctype)
	if err != nil || mt != "multipart/form-data" || params["boundary"] == "" {
		return nil, errors.New("send the audio as multipart/form-data, as OpenAI's /v1/audio/transcriptions takes it: a file and a model")
	}
	a := &audioForm{raw: body, ctype: ctype, boundary: params["boundary"]}
	mr := multipart.NewReader(bytes.NewReader(body), a.boundary)
	for {
		p, err := mr.NextRawPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, errors.New("the multipart form doesn't parse: " + err.Error())
		}
		switch p.FormName() {
		case "model", "prompt":
			if p.FileName() != "" {
				continue
			}
			v, err := io.ReadAll(io.LimitReader(p, 1<<20))
			if err != nil {
				return nil, errors.New("the multipart form doesn't parse: " + err.Error())
			}
			if p.FormName() == "model" {
				a.model = strings.TrimSpace(string(v))
			} else {
				a.prompt = string(v)
			}
		}
	}
	return a, nil
}

// forModel is the form as sent when nothing in it changes; otherwise it is
// written again part by part, under the same boundary, with the model the
// provider's own id.
func (a *audioForm) forModel(model string) ([]byte, string, error) {
	if model == a.model {
		return a.raw, a.ctype, nil
	}
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	if err := mw.SetBoundary(a.boundary); err != nil {
		return nil, "", err
	}
	mr := multipart.NewReader(bytes.NewReader(a.raw), a.boundary)
	for {
		p, err := mr.NextRawPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, "", errors.New("the multipart form doesn't parse: " + err.Error())
		}
		pw, err := mw.CreatePart(p.Header)
		if err != nil {
			return nil, "", err
		}
		switch {
		case p.FormName() == "model" && p.FileName() == "":
			io.WriteString(pw, model)
		default:
			if _, err := io.Copy(pw, p); err != nil {
				return nil, "", err
			}
		}
	}
	if err := mw.Close(); err != nil {
		return nil, "", err
	}
	return buf.Bytes(), mw.FormDataContentType(), nil
}

// answerType is the vendor's own: a transcript asked as text, srt or vtt
// is no JSON.
func (a *audioForm) answerType(got string) string {
	if got == "" {
		return "application/json"
	}
	return got
}

// answeredTokens is the output tokens an answer says it counted:
// gpt-4o-transcribe's usage.output_tokens; whisper's answer counts none.
func answeredTokens(b []byte) int {
	var a struct {
		Usage struct {
			Output int `json:"output_tokens"`
		} `json:"usage"`
	}
	if json.Unmarshal(b, &a) != nil {
		return 0
	}
	return a.Usage.Output
}

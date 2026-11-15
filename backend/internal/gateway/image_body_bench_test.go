package gateway

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"math/rand/v2"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// noisePNG is a real PNG of w×h random pixels: it doesn't compress, so its
// base64 is as large as a dense screenshot's of the same size.
func noisePNG(w, h int, seed uint64) []byte {
	r := rand.New(rand.NewPCG(seed, seed))
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for i := 0; i < len(img.Pix); i += 4 {
		v := r.Uint32()
		img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3] = uint8(v), uint8(v>>8), uint8(v>>16), 255
	}
	_ = color.White
	var b bytes.Buffer
	png.Encode(&b, img)
	return b.Bytes()
}

// imageConversation is a conversation of turns turns, each with one image
// of about 2.6MB base64, as its client sends it.
func imageConversation(proto provider.Protocol, model string, turns int) []byte {
	type m = map[string]any
	var msgs []any
	for i := range turns {
		url := "data:image/png;base64," + base64.StdEncoding.EncodeToString(noisePNG(900, 700, uint64(i+1)))
		text := "what does this screenshot show?"
		switch proto {
		case provider.Anthropic:
			data := url[len("data:image/png;base64,"):]
			msgs = append(msgs, m{"role": "user", "content": []any{m{"type": "image", "source": m{"type": "base64", "media_type": "image/png", "data": data}}, m{"type": "text", "text": text}}},
				m{"role": "assistant", "content": []any{m{"type": "text", "text": "It shows noise."}}})
		case provider.Chat:
			msgs = append(msgs, m{"role": "user", "content": []any{m{"type": "image_url", "image_url": m{"url": url}}, m{"type": "text", "text": text}}},
				m{"role": "assistant", "content": "It shows noise."})
		default:
			msgs = append(msgs, m{"type": "message", "role": "user", "content": []any{m{"type": "input_image", "image_url": url}, m{"type": "input_text", "text": text}}},
				m{"type": "message", "role": "assistant", "content": []any{m{"type": "output_text", "text": "It shows noise."}}})
		}
	}
	body := m{"model": model, "stream": false}
	switch proto {
	case provider.Anthropic:
		msgs = append(msgs, m{"role": "user", "content": "go on"})
		body["max_tokens"] = 1000
		body["messages"] = msgs
	case provider.Chat:
		msgs = append(msgs, m{"role": "user", "content": "go on"})
		body["messages"] = msgs
	default:
		msgs = append(msgs, m{"type": "message", "role": "user", "content": []any{m{"type": "input_text", "text": "go on"}}})
		body["input"] = msgs
	}
	b, _ := json.Marshal(body)
	return b
}

func BenchmarkImageRequest(b *testing.B) {
	chatReply := `{"id":"c1","object":"chat.completion","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":7,"completion_tokens":1}}`
	chatSSE := "data: {\"id\":\"c1\",\"object\":\"chat.completion.chunk\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"ok\"},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":7,\"completion_tokens\":1}}\n\ndata: [DONE]\n\n"
	for _, tc := range []struct {
		name, path string
		in, up     provider.Protocol
		reply      string
		sse        bool
	}{
		{"responses-to-chat", "/v1/responses", provider.Responses, provider.Chat, chatSSE, true},
		{"chat-to-chat", "/v1/chat/completions", provider.Chat, provider.Chat, chatReply, false},
		{"messages-to-chat", "/v1/messages", provider.Anthropic, provider.Chat, chatSSE, true},
		{"responses-to-responses", "/v1/responses", provider.Responses, provider.Responses, `{"id":"r1","object":"response","status":"completed","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"ok"}]}],"usage":{"input_tokens":7,"output_tokens":1}}`, false},
	} {
		b.Run(tc.name, func(b *testing.B) {
			b.Setenv("XDG_CONFIG_HOME", b.TempDir())
			b.Setenv("XDG_CACHE_HOME", b.TempDir())
			f := &fake{ctype: "application/json", reply: tc.reply}
			if tc.sse {
				f.ctype = ""
			}
			up := httptest.NewServer(f)
			defer up.Close()
			p := provider.Provider{ID: "fake", Name: "Fake", Key: "k", Models: []string{"m1"}}
			switch tc.up {
			case provider.Chat:
				p.Chat = up.URL + "/v1"
			case provider.Responses:
				p.Responses = up.URL + "/v1"
			}
			if err := provider.Save(p); err != nil {
				b.Fatal(err)
			}
			body := imageConversation(tc.in, "fake/m1", 8)
			s := New()
			b.SetBytes(int64(len(body)))
			b.ResetTimer()
			for range b.N {
				ctx, done := context.WithCancel(context.Background())
				rec := httptest.NewRecorder()
				s.Handler().ServeHTTP(rec, httptest.NewRequest("POST", tc.path, bytes.NewReader(body)).WithContext(ctx))
				done()
				if rec.Code != 200 || f.calls == 0 {
					b.Fatalf("%d %s", rec.Code, rec.Body.String())
				}
			}
			b.ReportMetric(float64(len(body))/1e6, "MB/req")
		})
	}
}

// BenchmarkPreviousResponseTurn is a turn after an 8-image conversation
// that a client sends as previous_response_id and its new text alone
// (kevinzhow on X), against the whole conversation sent again (whole):
// to a provider that keeps responses, which is sent the new input after
// its own id (kept), and to one on Chat, which uni-switch gives the whole
// conversation (chat). up-MB/req is what went upstream.
func BenchmarkPreviousResponseTurn(b *testing.B) {
	for _, tc := range []struct {
		name        string
		chat, whole bool
	}{{"whole-to-responses", false, true}, {"prev-to-kept-responses", false, false}, {"prev-to-chat", true, false}} {
		b.Run(tc.name, func(b *testing.B) {
			b.Setenv("XDG_CONFIG_HOME", b.TempDir())
			b.Setenv("XDG_CACHE_HOME", b.TempDir())
			defer func(old func(provider.Provider) bool) { prevKeptBy = old }(prevKeptBy)
			prevKeptBy = func(p provider.Provider) bool { return p.ID == "fake" }
			var n, upBytes atomic.Int64
			up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				got, _ := io.ReadAll(r.Body)
				upBytes.Add(int64(len(got)))
				i := n.Add(1)
				if tc.chat {
					w.Header().Set("Content-Type", "text/event-stream")
					io.WriteString(w, "data: {\"id\":\"c1\",\"object\":\"chat.completion.chunk\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"ok\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
					return
				}
				w.Header().Set("Content-Type", "application/json")
				fmt.Fprintf(w, `{"id":"resp_%d","object":"response","status":"completed","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"ok"}]}],"usage":{"input_tokens":7,"output_tokens":1}}`, i)
			}))
			defer up.Close()
			p := provider.Provider{ID: "fake", Name: "Fake", Key: "k", Models: []string{"m1"}}
			if tc.chat {
				p.Chat = up.URL + "/v1"
			} else {
				p.Responses = up.URL + "/v1"
			}
			if err := provider.Save(p); err != nil {
				b.Fatal(err)
			}
			s := New()
			ask := func(body []byte) *httptest.ResponseRecorder {
				rec := httptest.NewRecorder()
				req := httptest.NewRequest("POST", "/v1/responses", bytes.NewReader(body))
				req.RemoteAddr = "127.0.0.1:5555"
				s.Handler().ServeHTTP(rec, req)
				if rec.Code != 200 {
					b.Fatalf("%d %.300s", rec.Code, rec.Body.String())
				}
				return rec
			}
			whole := imageConversation(provider.Responses, "fake/m1", 8)
			var seed struct{ ID string }
			json.Unmarshal(ask(whole).Body.Bytes(), &seed)
			turn := []byte(`{"model":"fake/m1","stream":false,"previous_response_id":"` + seed.ID + `","input":"go on"}`)
			if tc.whole {
				turn = whole
			}
			upBytes.Store(0)
			b.SetBytes(int64(len(turn)))
			b.ResetTimer()
			for range b.N {
				ask(turn)
			}
			b.StopTimer()
			b.ReportMetric(float64(len(turn))/1e6, "client-MB/req")
			b.ReportMetric(float64(upBytes.Load())/float64(b.N)/1e6, "up-MB/req")
		})
	}
}

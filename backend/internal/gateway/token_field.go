package gateway

import (
	"bytes"
	"context"
	"io"
	"log"
	"net/http"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// tokenFieldRefused is the unfit entry for a provider whose Chat
// Completions turned a reply-length field away, naming the other as the
// one to send: tokenFieldRefused+"max_tokens" means max_tokens is refused.
const tokenFieldRefused = "token-field-refused:"

// chatTokenField names a Chat Completions request's reply length as p's
// API reads it: max_completion_tokens on OpenAI's own API, whose newer
// models turn max_tokens away (GoxxKintBlade on X: a client's, or
// uni-switch's image describer's, max_tokens 400 "Use 'max_completion_tokens'
// instead"), and on any other the name it asked for once it turned the
// other away (forward). Every other vendor's body goes as it came:
// DeepSeek, Zhipu and most relays read max_tokens alone.
func (s *Server) chatTokenField(p provider.Provider, to provider.Protocol, body []byte) []byte {
	if to != provider.Chat {
		return body
	}
	want := ""
	switch {
	case !s.fits(p.ID, tokenFieldRefused+provider.CompletionTokField, to):
		want = provider.MaxTokensField
	case !s.fits(p.ID, tokenFieldRefused+provider.MaxTokensField, to) || provider.OpenAIHost(provider.HostOf(p.Base(to))):
		want = provider.CompletionTokField
	default:
		return body
	}
	out, _ := provider.WithTokenField(body, want)
	return out
}

// retryTokenField asks a Chat Completions request once more when its 400
// names the other reply-length field as the one to send, and remembers
// that for p, so the next request goes with it at once (chatTokenField).
func (s *Server) retryTokenField(ctx context.Context, p provider.Provider, to provider.Protocol, path string, body []byte, in http.Header, res *http.Response) (*http.Response, error) {
	if to != provider.Chat || !badRequest(res.StatusCode) {
		return res, nil
	}
	b, _ := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	res.Body.Close()
	res.Body = io.NopCloser(bytes.NewReader(b))
	want := provider.TokenFieldWanted(b)
	if want == "" {
		return res, nil
	}
	if _, ok := provider.WithTokenField(body, want); !ok {
		return res, nil
	}
	refused := provider.MaxTokensField
	if want == provider.MaxTokensField {
		refused = provider.CompletionTokField
	}
	log.Printf("gateway: %s turned away %s (%d); asked with %s from now on", p.ID, refused, res.StatusCode, want)
	s.mu.Lock()
	s.unfit[p.ID+"\x00"+tokenFieldRefused+refused+"\x00"+string(to)] = true
	delete(s.unfit, p.ID+"\x00"+tokenFieldRefused+want+"\x00"+string(to))
	s.mu.Unlock()
	again, err := s.forwardOnce(ctx, p, to, path, body, in)
	if err != nil {
		// the refusal stands
		res.Body = io.NopCloser(bytes.NewReader(b))
		return res, nil
	}
	return again, nil
}

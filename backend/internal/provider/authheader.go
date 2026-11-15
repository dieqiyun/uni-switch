package provider

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"sync"

	"github.com/dieqiyun/uni-switch/backend/internal/catalog"
)

// The headers a third-party Anthropic API's key goes in (Provider.AuthHeader).
const (
	AuthBoth    = ""                  // x-api-key and Authorization: Bearer, the default
	AuthXAPIKey = catalog.AuthXAPIKey // x-api-key alone
	AuthBearer  = catalog.AuthBearer  // Authorization: Bearer alone
)

// AuthChoices are the values Provider.AuthHeader takes.
var AuthChoices = []string{AuthBoth, AuthXAPIKey, AuthBearer}

// BothAuth says p's Anthropic requests carry its key in both x-api-key and
// Authorization, as they do by default to a third-party Anthropic API: the
// ones AuthHeader can narrow to one. Anthropic's, Bedrock's, Azure's and
// Vertex AI's never do, nor a signed-in account's.
func (p Provider) BothAuth() bool {
	if p.Account != nil || p.Anthropic == "" {
		return false
	}
	p.AuthHeader = AuthBoth
	return len(AuthHeaders(p, Anthropic)) == 2
}

// RetryOneAuth hands on res, the answer to req, which p signed for its
// Anthropic API; or, when the vendor refused it for carrying the key in
// both x-api-key and Authorization (catalog.BothAuthRefused), the answer to
// req asked again with each alone, x-api-key first, and remembers for p the
// one it took (LearnAuthHeader). do sends a request as req was sent.
func RetryOneAuth(p Provider, req *http.Request, res *http.Response, do func(*http.Request) (*http.Response, error)) (*http.Response, error) {
	if res == nil || res.StatusCode != http.StatusUnauthorized || p.AuthHeader != AuthBoth || !p.BothAuth() || req.GetBody == nil {
		return res, nil
	}
	// the user's own Authorization or x-api-key (Headers) is theirs
	if req.Header.Get("x-api-key") != p.Key || req.Header.Get("Authorization") != "Bearer "+p.Key {
		return res, nil
	}
	b, _ := io.ReadAll(io.LimitReader(res.Body, 64<<10))
	res.Body.Close()
	res.Body = io.NopCloser(bytes.NewReader(b))
	if !catalog.BothAuthRefused(res.StatusCode, b) {
		return res, nil
	}
	for i, only := range catalog.OneAuthOrder {
		body, err := req.GetBody()
		if err != nil {
			return res, nil
		}
		again := req.Clone(req.Context())
		again.Body = body
		catalog.OneAuth(again.Header, only)
		if i > 0 {
			res.Body.Close()
		}
		if res, err = do(again); err != nil {
			return nil, err
		}
		if res.StatusCode != http.StatusUnauthorized && res.StatusCode != http.StatusForbidden {
			LearnAuthHeader(p.ID, only)
			return res, nil
		}
	}
	return res, nil
}

var learnMu sync.Mutex

// LearnAuthHeader keeps only as the header provider id's key goes in, when
// it sends both still: one the user picked is theirs.
func LearnAuthHeader(id, only string) {
	learnMu.Lock()
	defer learnMu.Unlock()
	f, err := read()
	if err != nil {
		return
	}
	for i := range f.Providers {
		if f.Providers[i].ID == id {
			if f.Providers[i].AuthHeader != AuthBoth {
				return
			}
			f.Providers[i].AuthHeader = only
			_ = store(f)
			return
		}
	}
}

// listAuth is ctx for asking p's model list at its Anthropic base: its key
// in the header AuthHeader says, and learned as RetryOneAuth learns it.
func (p Provider) listAuth(ctx context.Context) context.Context {
	if !p.BothAuth() {
		return ctx
	}
	return catalog.WithListAuth(ctx, p.AuthHeader, func(only string) { LearnAuthHeader(p.ID, only) })
}

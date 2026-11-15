package gateway

import (
	"context"
	"errors"
	"net/http"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// probeServer is the gateway a translated test is asked through; a var so
// a test can give it a client of its own.
var probeServer = New

// probeTranslated asks model on p, on proto, the smallest "hi" as an
// agent's request to it is asked — built in the account's envelope by the
// translator — and reads the reply to the model's first word, thought or
// tool call, or its error (provider.ProbeTranslatedVia).
func probeTranslated(ctx context.Context, p provider.Provider, proto provider.Protocol, model string) (int, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel() // the reader stops once the first word is in
	req := &Request{MaxTokens: 16, Messages: []Message{{Role: "user", Parts: []Part{{Kind: Text, Text: "hi"}}}}}
	events, code, msg := probeServer().askTranslated(p, proto, model, http.Header{}, http.Header{})(ctx, req)
	if events == nil {
		return code, errors.New(msg)
	}
	for ev := range events {
		switch ev.Kind {
		case KError:
			return ev.Status, errors.New(ev.Text)
		case KText, KThink, KToolStart, KImage:
			return 0, nil
		}
	}
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	return 0, nil
}

func init() { provider.ProbeTranslatedVia(probeTranslated) }

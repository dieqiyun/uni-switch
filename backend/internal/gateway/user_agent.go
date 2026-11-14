package gateway

import (
	"context"
	"net/http"
	"strings"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// uniSwitchUA is the User-Agent uni-switch's requests go to a vendor with.
func uniSwitchUA() string { return "uni-switch/" + Version }

// userAgentFor is the User-Agent a request to p goes with, ua being the
// one the request came to uni-switch with: the agent's own, as it sent it,
// where p passes it on (provider.PassesUserAgent: a key+URL provider not
// set to go as uni-switch, #1851), and uni-switch's where it doesn't, where the
// agent sent none, and on uni-switch's own
// requests — its classifier's, vision's, search's, the image tool's
// (uni-switchsOwn) — which no agent sent. A User-Agent the user set among the
// provider's headers is put on after, when the request is signed, and
// stands over either.
func userAgentFor(p provider.Provider, ua string) string {
	if ua != "" && p.PassesUserAgent() && !uniSwitchsOwn(ua) {
		return ua
	}
	return uniSwitchUA()
}

// uni-switchsOwn is a User-Agent uni-switch's own requests carry: another uni-switch's
// (uni-switch/…), the classifier's, vision's and search's (uni-switch-router/1,
// uni-switch-vision/1, uni-switch-search/1), and the image tool's, which names the
// agent it draws for before its own (claude-code/2.1.0 uni-switch-image/1)
// from what the agent told it, not from a User-Agent the agent sent.
func uniSwitchsOwn(ua string) bool {
	return strings.HasPrefix(ua, "uni-switch/") || strings.HasPrefix(ua, "uni-switch-") || strings.HasSuffix(ua, " "+DrawAgent)
}

// callerUA is the User-Agent the request ctx is for came with (withCaller),
// or the agent's another uni-switch passed it on for; "" when there is none.
func callerUA(ctx context.Context) string {
	c, _ := ctx.Value(callerCtx{}).(caller)
	return c.ua
}

// stampUA puts on req, a request to p that isn't uni-switch's own forwarded
// one (an embedding, a drawing, a video), the User-Agent forwardOnce's
// carry: uni-switch's, or the agent's where p passes it on. A signed-in
// account's is left to its signing, as before, and another uni-switch's is
// set where it is asked.
func stampUA(ctx context.Context, p provider.Provider, h http.Header) {
	if p.Account != nil {
		return
	}
	h.Set("User-Agent", userAgentFor(p, callerUA(ctx)))
}

// agentUA returns the local client User-Agent.
func agentUA(ctx context.Context, in http.Header) string { return in.Get("User-Agent") }

package provider

import (
	"net/url"

	"github.com/dieqiyun/uni-switch/backend/internal/netproxy"
)

func init() { netproxy.HTTP1 = codexBackend }

// codexBackend says u is on the ChatGPT backend (CodexBase's host), which
// every transport netproxy.Dispatch makes speaks HTTP/1.1 to: the
// gateway's turns, the models list, the warm-up, the allowance and its
// resets. Codex itself does: codex-cli offers no ALPN at all there, and
// Pi's Codex provider speaks HTTP/1.1 or a WebSocket. On one shared HTTP/2
// connection a Codex stream was reset now and then mid-reply (jasonz3g on
// X, "stream ID 21; INTERNAL_ERROR; received from peer"), and through a
// local proxy a request failed with PROTOCOL_ERROR until HTTP/2 was turned
// off.
func codexBackend(u *url.URL) bool {
	b, err := url.Parse(CodexBase)
	return err == nil && u.Host == b.Host
}

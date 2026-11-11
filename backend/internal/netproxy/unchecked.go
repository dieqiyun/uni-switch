package netproxy

import (
	"context"
	"crypto/tls"
	"errors"
	"net"
	"net/http"
	"net/url"
	"strings"
)

type uncheckedKey struct{}

// WithUncheckedTLS is ctx for requests made on behalf of a provider the
// user added whose HTTPS server they asked uni-switch not to check the
// certificate of (#2383, fangzhengjin: vLLM on the LAN, Ollama behind a
// self-signed cert, a company CA): a request through a transport Dispatch
// made to the host of one of bases, over https, has its certificate taken
// as it is, through a transport kept for that host alone. Any other host —
// a redirect elsewhere, an image or a balance URL on another host — is
// checked as before, and an https:// proxy is refused for it
// (uncheckTLS). With (a request made for another provider) drops it.
func WithUncheckedTLS(ctx context.Context, bases ...string) context.Context {
	hosts := map[string]bool{}
	for _, b := range bases {
		u, err := url.Parse(strings.TrimSpace(b))
		if err != nil || !strings.EqualFold(u.Scheme, "https") || u.Hostname() == "" {
			continue
		}
		hosts[hostKey(u)] = true
	}
	if len(hosts) == 0 {
		return ctx
	}
	return context.WithValue(ctx, uncheckedKey{}, hosts)
}

func uncheckedOf(ctx context.Context) map[string]bool {
	if ctx == nil {
		return nil
	}
	h, _ := ctx.Value(uncheckedKey{}).(map[string]bool)
	return h
}

// uncheckedHost is the host:port of req when its certificate is not to be
// checked (WithUncheckedTLS), "" when it is.
func uncheckedHost(req *http.Request) string {
	hosts := uncheckedOf(req.Context())
	if len(hosts) == 0 || req.URL == nil || !strings.EqualFold(req.URL.Scheme, "https") {
		return ""
	}
	if k := hostKey(req.URL); hosts[k] {
		return k
	}
	return ""
}

// hostKey is u's host and port, the port https's when u names none.
func hostKey(u *url.URL) string {
	port := u.Port()
	if port == "" {
		port = "443"
	}
	return net.JoinHostPort(strings.ToLower(strings.TrimSuffix(u.Hostname(), ".")), port)
}

// ErrUncheckedOverHTTPSProxy is why a request to a host whose certificate
// is not checked isn't sent through an https:// proxy: the proxy's own TLS
// is dialled with the same config, so its certificate would go unchecked
// too.
var ErrUncheckedOverHTTPSProxy = errors.New("a provider whose certificate isn't checked can't be reached through an https:// proxy, whose certificate would go unchecked with it: give it an http:// or socks5:// proxy, or none")

// uncheckTLS has t, a clone kept for requests to one host whose
// certificate is not checked, take that host's certificate as it is. Its
// only other TLS would be an https:// proxy's, dialled with the same
// config: such a proxy is refused (ErrUncheckedOverHTTPSProxy).
func uncheckTLS(t *http.Transport) {
	if t.TLSClientConfig == nil {
		t.TLSClientConfig = &tls.Config{}
	}
	t.TLSClientConfig.InsecureSkipVerify = true
	if next := t.Proxy; next != nil {
		t.Proxy = func(req *http.Request) (*url.URL, error) {
			u, err := next(req)
			if err == nil && u != nil && strings.EqualFold(u.Scheme, "https") {
				return nil, ErrUncheckedOverHTTPSProxy
			}
			return u, err
		}
	}
}

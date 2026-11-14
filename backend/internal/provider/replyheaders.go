package provider

// Headers of a vendor's reply the gateway passes on to the agent
// (ReplyHeaders, #4145): the vendor's own request id, which the agent's
// side can then match with the vendor's logs.

import (
	"net/http"
	"slices"
	"strings"
)

// DefaultReplyHeaders are passed on from every provider's reply, without a
// word from the user: request ids, opaque to everyone but the vendor, which
// uni-switch sets no header of its own under. OpenAI's and Anthropic's names,
// and one-api's (new-api and the relays built on it).
var DefaultReplyHeaders = []string{"Request-Id", "X-Request-Id", "X-Oneapi-Request-Id"}

// replyHeadersNever are never passed on, listed or not: the reply's own
// framing, which uni-switch writes for what it sends (a translated or
// unpacked body has another length, encoding and type), what is said of
// the connection, cookies and sign-ins of the vendor's that would be set
// on uni-switch's address, and what tells a browser to trust or reach uni-switch
// otherwise. The prefixes below go with them.
var replyHeadersNever = map[string]bool{
	"Connection": true, "Keep-Alive": true, "Te": true, "Trailer": true, "Transfer-Encoding": true, "Upgrade": true,
	"Cache-Control": true, "X-Accel-Buffering": true, "Retry-After": true, "Date": true, "Server": true, "Vary": true,
	"Set-Cookie": true, "Set-Cookie2": true, "Cookie": true, "Authorization": true, "Www-Authenticate": true,
	"Authentication-Info": true, "X-Api-Key": true, "Api-Key": true,
	"Location": true, "Refresh": true, "Link": true, "Alt-Svc": true, "Strict-Transport-Security": true,
	"Content-Security-Policy": true, "Origin": true, "Host": true, "Clear-Site-Data": true,
}

// replyHeadersNeverPrefix: Content-Length, -Encoding, -Type and the rest;
// Proxy-Authenticate and the rest; CORS's, which would let a page read
// uni-switch; Sec- fetch metadata; and uni-switch's own, which the gateway reads
// as said by the uni-switch that answered (a rest, a refusal, a key limit).
var replyHeadersNeverPrefix = []string{"Content-", "Proxy-", "Access-Control-", "Sec-", "X-Uni-Switch-"}

// ReplyHeaderPassable says whether a header of a vendor's reply may be
// passed on to the agent at all.
func ReplyHeaderPassable(name string) bool {
	k := http.CanonicalHeaderKey(strings.TrimSpace(name))
	if !validHeaderName(k) || replyHeadersNever[k] {
		return false
	}
	for _, p := range replyHeadersNeverPrefix {
		if strings.HasPrefix(k, p) {
			return false
		}
	}
	return true
}

// validHeaderName is an HTTP token (RFC 9110): no space, colon or control
// character.
func validHeaderName(k string) bool {
	if k == "" {
		return false
	}
	for _, r := range k {
		if r <= ' ' || r >= 0x7f || strings.ContainsRune("\"(),/:;<=>?@[\\]{}", r) {
			return false
		}
	}
	return true
}

// ReplyHeadersOK says what is wrong with a list of reply headers to pass
// on, "" when nothing is: a name that isn't one, or one uni-switch never
// passes on.
func ReplyHeadersOK(names []string) string {
	for _, n := range names {
		n = strings.TrimSpace(n)
		if n == "" {
			continue
		}
		if !validHeaderName(n) {
			return "\"" + n + "\" isn't a header name"
		}
		if !ReplyHeaderPassable(n) {
			return n + " is never passed on: uni-switch writes it, or it would act on uni-switch's own address"
		}
	}
	return ""
}

// cleanReplyHeaders is the list as saved: names trimmed and canonical,
// each once, what can't be passed on dropped; nil when nothing is left.
func cleanReplyHeaders(names []string) []string {
	var out []string
	for _, n := range names {
		k := http.CanonicalHeaderKey(strings.TrimSpace(n))
		if ReplyHeaderPassable(k) && !slices.Contains(out, k) {
			out = append(out, k)
		}
	}
	return out
}

// PassedReplyHeaders are the headers of the vendor's reply h that go on to
// the agent: the defaults and the ones the user listed for the provider,
// matched whatever their case, never one ReplyHeaderPassable refuses. nil
// when there are none.
func (p Provider) PassedReplyHeaders(h http.Header) http.Header {
	if len(h) == 0 {
		return nil
	}
	var out http.Header
	for _, names := range [][]string{DefaultReplyHeaders, p.ReplyHeaders} {
		for _, n := range names {
			k := http.CanonicalHeaderKey(strings.TrimSpace(n))
			if out[k] != nil || !ReplyHeaderPassable(k) {
				continue
			}
			vs := h.Values(k)
			if len(vs) == 0 {
				// a header map filled by hand, its keys not canonical
				for hk, hv := range h {
					if strings.EqualFold(hk, k) {
						vs = hv
						break
					}
				}
			}
			if len(vs) == 0 {
				continue
			}
			if out == nil {
				out = http.Header{}
			}
			out[k] = slices.Clone(vs)
		}
	}
	return out
}

// ReplyHeadersNever are the names and prefixes ReplyHeaderPassable
// refuses, for the GUI's own check of a list typed (its test holds the
// two the same).
func ReplyHeadersNever() (names, prefixes []string) {
	for k := range replyHeadersNever {
		names = append(names, k)
	}
	slices.Sort(names)
	return names, slices.Clone(replyHeadersNeverPrefix)
}

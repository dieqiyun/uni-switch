package gateway

import (
	"context"
	"net/http"
	"path"
	"strings"
)

// Front serves the gateway behind localGuard. Client routes handed over
// by a management-page port carry a page-port mark and are refused.
func (s *Server) Front() http.Handler {
	// Diagnostics keep their own local-only checks ahead of potentially
	// blocking request wrappers; socket turns pass through all guards again.
	s.frontOnce.Do(func() { s.front = socketEntry(s.dumpFirst(localGuard(s.Handler()))) })
	return s.front
}

// clientPrefixes are the gateway's client routes (Handler) that a page
// served on a port of its own passes on to the gateway: everything an
// agent or an SDK calls with the gateway as its base URL.
var clientPrefixes = []string{
	"/v1", "/v1beta",
	"/models", "/chat/completions", "/responses", "/messages",
	"/images", "/embeddings", "/rerank", "/decisions", "/audio", "/videos",
	"/muse-code/models", CodexPath, AlphaSearchPath, "/mcp",
	// the key page (KeyPagePath): a key's holder opens it at the address
	// they give their agents
	KeyPagePath,
}

// ClientPath identifies inference routes on the management port so they
// are refused by the gateway instead of entering the page's own API.
// /api/* and Claude helper callbacks belong to the gateway port alone.
func ClientPath(p string) bool {
	if p == "" || path.Clean(p) != strings.TrimSuffix(p, "/") && p != "/" {
		return false
	}
	for _, pre := range clientPrefixes {
		if p == pre || strings.HasPrefix(p, pre+"/") {
			return true
		}
	}
	return false
}

// PagePortHeader marks an inference request handed over by the local
// management port. It always removes locality, including for a raw TCP
// forward with no other headers. localGuard consumes it before refusing
// the request, so it is never sent upstream. Local agents use the gateway
// port directly.
const PagePortHeader = "X-Uni-Switch-Page-Port"

// pagePort is the context key of a request the page's port handed over.
type pagePort struct{}

// FromPagePort is r marked as handed over by the page's port: for the
// gateway this process serves (Front), or the header toGateway passes on.
func FromPagePort(r *http.Request) *http.Request {
	r = r.Clone(r.Context())
	r.Header.Set(PagePortHeader, "1")
	return r
}

// viaPagePort: r came through `uni-switch web`'s page port (FromPagePort).
func viaPagePort(r *http.Request) bool {
	ok, _ := r.Context().Value(pagePort{}).(bool)
	return ok || r.Header.Get(PagePortHeader) != ""
}

// takePagePort moves the page port's mark from r's headers into its
// context, so nothing passes the header on to a vendor.
func takePagePort(r *http.Request) *http.Request {
	if r.Header.Get(PagePortHeader) == "" {
		return r
	}
	r = r.WithContext(context.WithValue(r.Context(), pagePort{}, true))
	r.Header = r.Header.Clone()
	r.Header.Del(PagePortHeader)
	return r
}

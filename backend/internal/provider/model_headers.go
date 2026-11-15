package provider

import (
	"context"
	"maps"
	"slices"
	"strings"
)

func (p Provider) listHeaders() map[string]string { return p.SentHeaders(context.Background()) }

func (p Provider) listHeadersAt(u string) map[string]string {
	h := p.listHeaders()
	if !OpenCodeURL(u) {
		return h
	}
	for k := range h {
		if strings.EqualFold(k, "x-opencode-session") {
			return h
		}
	}
	h = maps.Clone(h)
	if h == nil {
		h = map[string]string{}
	}
	id := openCodeSession("")
	h["X-Opencode-Session"], h["X-Opencode-Session-Id"] = id, id
	// as OpenCode 2 asks its gateway (#3491), unless the user set their own
	if !slices.ContainsFunc(slices.Collect(maps.Keys(h)), func(k string) bool { return strings.EqualFold(k, "User-Agent") }) {
		h["User-Agent"] = OpenCodeUserAgent()
	}
	if !slices.ContainsFunc(slices.Collect(maps.Keys(h)), func(k string) bool { return strings.EqualFold(k, "x-opencode-client") }) {
		h["X-Opencode-Client"] = "cli"
	}
	return h
}

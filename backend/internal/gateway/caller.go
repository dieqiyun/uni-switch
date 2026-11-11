package gateway

import (
	"context"
	"net/http"
	"os"
	"strings"
	"sync"
	"unicode"
)

// caller records only the local client that made this request.
type caller struct {
	agent, ua  string
	claudeHere bool
}
type callerCtx struct{}

func withCaller(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c := callerFrom(r)
		for _, h := range []string{"X-Uni-Switch-Agent", "X-Uni-Switch-Via", "X-Uni-Switch-Hops", "X-Uni-Switch-User-Agent", "X-Uni-Switch-Claude-Cache-TTL", "X-Uni-Switch-Searching", "X-Uni-Switch-Keep-Turn"} {
			r.Header.Del(h)
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), callerCtx{}, c)))
	})
}
func callerFrom(r *http.Request) caller {
	c := caller{agent: agentOf(r), ua: r.UserAgent()}
	c.claudeHere = (c.agent == "claude" || fromClaudeCode(r.Header)) && c.agent != "claude-desktop" && local(r)
	return c
}
func callerOf(r *http.Request) caller {
	if c, ok := r.Context().Value(callerCtx{}).(caller); ok {
		return c
	}
	return callerFrom(r)
}
func claudeTTLOf(c caller) string {
	if !c.claudeHere || !claudeSettingsKnown() {
		return ""
	}
	if ttl := claudePickedTTL(os.Environ()); ttl != "" {
		return ttl
	}
	return "default"
}
func label(v string) string {
	v = strings.TrimSpace(v)
	if v == "" || len(v) > 64 || strings.IndexFunc(v, func(r rune) bool { return !unicode.IsPrint(r) || unicode.IsSpace(r) }) >= 0 {
		return ""
	}
	return v
}

// boundedSession is a session header's value as uni-switch keeps it: trimmed,
// at most 128 bytes, as sessionOf takes it.
func boundedSession(v string) string {
	v = strings.TrimSpace(v)
	return v[:min(len(v), 128)]
}

var hostName = sync.OnceValue(func() string {
	h, _ := os.Hostname()
	h, _, _ = strings.Cut(h, ".")
	return label(h)
})

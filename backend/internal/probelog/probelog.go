// Package probelog writes down the requests uni-switch sends a provider on its
// own behalf, outside any agent's turn: the provider editor's Test, its
// Detect APIs and per-model detection, and a model list's refresh (neiko
// and gys619 on Discord: a test left no trace in the request log). Each
// such request is one row of the request log, marked as uni-switch's own test
// (usage.SourceProbe) and never counted in a usage or cost total; a model
// list that was answered is left out there (usage's quietProbe), as uni-switch
// asks for lists by itself all day.
//
// The provider package marks a context with the kind of test (With); every
// request sent under it is written down once, by the provider's Do or by
// the transport the HTTP client goes through (Transport), whichever sends
// it first. Package usage sets Sink.
package probelog

import (
	"context"
	"net/http"
	"strings"
	"time"
)

// The kinds a row is written under, as the request log's "Called for"
// names them (usage.purposeKinds).
const (
	KindTest   = "provider_test" // the editor's Test, a model's test
	KindDetect = "api_detect"    // Detect APIs, a model's API detection
	KindList   = "model_list"    // a model list asked for, refreshed
)

// Row is one request written down.
type Row struct {
	Time     time.Time
	Kind     string
	Provider string // the provider's id, or the host for one not yet saved
	Account  string
	Model    string
	Host     string
	Endpoint string
	Status   int
	Millis   int64
	Error    string
}

// Sink takes each row; nil writes nothing. usage sets it to its log.
var Sink func(Row)

type meta struct {
	kind, provider, account, model string
	done                           bool
}

type key struct{}

func from(ctx context.Context) (meta, bool) {
	if ctx == nil {
		return meta{}, false
	}
	m, ok := ctx.Value(key{}).(meta)
	return m, ok
}

// With marks ctx: the requests sent under it are of kind, for provider (its
// id) and account. A kind set inside another replaces it, so a model list
// fetched for a Test is written as a model list. The model set before stays.
func With(ctx context.Context, kind, provider, account string) context.Context {
	m, _ := from(ctx)
	m.kind, m.provider, m.account, m.done = kind, provider, account, false
	return context.WithValue(ctx, key{}, m)
}

// WithModel is ctx with the model the requests under it ask, for a ctx
// With marked; any other ctx is returned as it is.
func WithModel(ctx context.Context, model string) context.Context {
	m, ok := from(ctx)
	if !ok {
		return ctx
	}
	m.model = model
	return context.WithValue(ctx, key{}, m)
}

// Kind is the kind ctx is marked with, "" for none.
func Kind(ctx context.Context) string {
	m, _ := from(ctx)
	return m.kind
}

// Taken is ctx with its requests already written down by its caller, so
// the transport under it doesn't write them again.
func Taken(ctx context.Context) context.Context {
	m, ok := from(ctx)
	if !ok || m.done {
		return ctx
	}
	m.done = true
	return context.WithValue(ctx, key{}, m)
}

// Pending is whether a request sent under ctx is still to be written down:
// ctx is marked and no caller has taken it.
func Pending(ctx context.Context) bool {
	m, ok := from(ctx)
	return ok && !m.done && m.kind != ""
}

// Note writes down one request under ctx's mark, sent to u (a URL; only
// its host and path are kept) at start, answered status or failed with
// err. Nothing is written for a ctx with no mark.
func Note(ctx context.Context, u, method string, start time.Time, status int, err error) {
	m, ok := from(ctx)
	if !ok || m.kind == "" || Sink == nil {
		return
	}
	host, path := split(u)
	r := Row{Time: start, Kind: m.kind, Provider: m.provider, Account: m.account, Model: m.model,
		Host: host, Endpoint: path, Status: status, Millis: time.Since(start).Milliseconds()}
	if method != "" && method != http.MethodPost {
		r.Endpoint = method + " " + path
	}
	if err != nil {
		r.Error = err.Error()
	}
	if r.Provider == "" {
		r.Provider = host // a provider being added has no id yet
	}
	if r.Provider == "" {
		r.Provider = "uni-switch" // never "": the log reads that as turned away
	}
	Sink(r)
}

// NoteResult writes down one request that isn't sent through an HTTP
// client of uni-switch's (Claude Code run for a test, the gateway's translated
// test), from what came of it.
func NoteResult(ctx context.Context, endpoint string, start time.Time, status int, errText string) {
	m, ok := from(ctx)
	if !ok || m.kind == "" || Sink == nil {
		return
	}
	r := Row{Time: start, Kind: m.kind, Provider: m.provider, Account: m.account, Model: m.model,
		Endpoint: endpoint, Status: status, Millis: time.Since(start).Milliseconds(), Error: errText}
	if r.Provider == "" {
		r.Provider = "uni-switch"
	}
	Sink(r)
}

func split(u string) (host, path string) {
	s := u
	if i := strings.Index(s, "://"); i >= 0 {
		s = s[i+3:]
	}
	host, path = s, "/"
	if i := strings.IndexAny(s, "/?#"); i >= 0 {
		host, path = s[:i], s[i:]
	}
	if i := strings.IndexAny(path, "?#"); i >= 0 {
		path = path[:i] // a query can carry a key (Gemini's ?key=)
	}
	if path == "" {
		path = "/"
	}
	if i := strings.LastIndex(host, "@"); i >= 0 {
		host = host[i+1:] // never a URL's user:password
	}
	return host, path
}

// Transport writes down each request sent through next under a marked
// context no caller took. Every request the HTTP client sends goes through
// it (netproxy.Install), so a model list asked through the client of any
// package is written as the provider's own requests are.
func Transport(next http.RoundTripper) http.RoundTripper { return transport{next} }

type transport struct{ next http.RoundTripper }

func (t transport) RoundTrip(req *http.Request) (*http.Response, error) {
	ctx := req.Context()
	if !Pending(ctx) {
		return t.next.RoundTrip(req)
	}
	start := time.Now()
	res, err := t.next.RoundTrip(req)
	status := 0
	if err == nil {
		status = res.StatusCode
	}
	Note(ctx, req.URL.String(), req.Method, start, status, err)
	return res, err
}

// Unwrap is the transport under it, for code that looks for netproxy's.
func (t transport) Unwrap() http.RoundTripper { return t.next }

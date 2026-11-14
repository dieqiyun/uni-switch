package gateway

import "net/http"

// roundTripFunc is an http.RoundTripper made of a function, for a test
// that sends the gateway's client somewhere of its own.
type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

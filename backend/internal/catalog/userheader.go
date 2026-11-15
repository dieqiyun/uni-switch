package catalog

import (
	"net/http"
	"strings"
)

// PutUserHeader writes a request header the user typed (a provider's own
// headers) onto h. Every header already there under the same name in any
// case is taken off first: Header.Set keys a name by its canonical form,
// while the user's goes in as typed (some gateways match names
// case-sensitively), so a "user-agent" or "authorization" typed in lower
// case used to ride beside uni-switch's own — two of the header, uni-switch's
// first (#1487, a relay that checks the client turned the request away).
//
// User-Agent itself is kept under its canonical name: Go's HTTP/1 writer
// looks only there, and adds its own Go-http-client/1.1 when it finds
// none, ahead of a "user-agent" written in any other case.
func PutUserHeader(h http.Header, name, value string) {
	for k := range h {
		if strings.EqualFold(k, name) {
			delete(h, k)
		}
	}
	if strings.EqualFold(name, "User-Agent") {
		name = "User-Agent"
	}
	h[name] = []string{value}
}

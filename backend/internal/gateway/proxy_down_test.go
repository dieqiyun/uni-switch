package gateway

import (
	"net"

	"slices"

	"testing"
)

// deadAddr is a loopback address nothing listens on, for a proxy that is
// down. It is taken after the test's own servers are up and is never one of
// theirs: a port just let go can be handed out again at once (Linux CI gave
// the closed port to the next httptest server, so the "down" proxy answered).
func deadAddr(t *testing.T, taken ...string) string {
	t.Helper()
	for range 20 {
		l, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		a := l.Addr().String()
		l.Close()
		if !slices.Contains(taken, a) {
			return a
		}
	}
	t.Fatal("no free loopback port apart from the test's servers")
	return ""
}

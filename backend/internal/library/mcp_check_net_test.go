package library

import (
	"context"
	"errors"
	"net"
	"net/url"
	"os"
	"testing"
)

func TestNetHealth(t *testing.T) {
	wrapped := &url.Error{
		Op: "Post", URL: "http://127.0.0.1/mcp",
		Err: &net.OpError{Op: "dial", Net: "tcp", Err: os.NewSyscallError("connect", connectionRefused)},
	}
	for _, test := range []struct {
		name string
		err  error
		why  string
	}{
		{"native refusal", connectionRefused, "refused"},
		{"wrapped refusal", wrapped, "refused"},
		{"deadline", &url.Error{Op: "Post", Err: context.DeadlineExceeded}, "timeout"},
		{"dns", &url.Error{Op: "Post", Err: &net.DNSError{Name: "missing.invalid", IsNotFound: true}}, "unreachable"},
		{"other error", errors.New("network unavailable"), "unreachable"},
		{"text alone", errors.New("connection refused"), "unreachable"},
	} {
		t.Run(test.name, func(t *testing.T) {
			h := netHealth(context.Background(), test.err)
			if h.State != "error" || h.Why != test.why {
				t.Fatalf("got %+v, want error/%s", h, test.why)
			}
			if test.why == "refused" && h.Detail != test.err.Error() {
				t.Fatalf("lost the original error: %q, want %q", h.Detail, test.err.Error())
			}
		})
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if h := netHealth(ctx, wrapped); h.Why != "timeout" {
		t.Fatalf("an ended check should keep its timeout classification: %+v", h)
	}
}

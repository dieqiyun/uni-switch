//go:build !darwin && !linux

package handoff

import (
	"context"
	"net"
	"os"
	"time"
)

// Offer is the Unix one's; here none is ever made.
type Offer struct{}

func Open(dir, exe string) (*Offer, error) { return nil, ErrUnsupported }

func (o *Offer) Dir() string                               { return "" }
func (o *Offer) Close()                                    {}
func (o *Offer) Withdraw()                                 {}
func (o *Offer) Accept(ctx context.Context) (*Conn, error) { return nil, ErrUnsupported }

// Conn is the Unix one's.
type Conn struct{ Hello Hello }

func (c *Conn) Give(f *os.File, m Meta) error        { return ErrUnsupported }
func (c *Conn) AwaitAck(timeout time.Duration) error { return ErrUnsupported }
func (c *Conn) Close()                               {}

// Taken is the Unix one's.
type Taken struct {
	Listener net.Listener
	Meta     Meta
}

// Take finds nothing to take here.
func Take(dir, exe string, hello Hello, timeout time.Duration) (*Taken, error) { return nil, nil }

func (t *Taken) Ack() error { return ErrUnsupported }
func (t *Taken) Close()     {}

func alive(pid int) bool { return false }

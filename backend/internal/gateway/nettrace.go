package gateway

import (
	"crypto/tls"
	"math"
	"net/http/httptrace"
	"sync"
	"sync/atomic"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// TryNet is where a try's time went up to its reply's first byte, as Go's
// HTTP client saw the request that went out to the vendor last (a
// sign-in refreshed or a request asked again on the way are Before): what
// was uni-switch's, the network's and the vendor's, so a slow turn says
// whose it was (Discord, Magic_zero: "网关连接提供商和回复到agent的速度
// 有点慢"). Each is in ms, to a tenth; absent for a try whose request
// went out some other way (a plugin's own fetch, an agent's app).
type TryNet struct {
	// Own: uni-switch's, from the try's Start to its request going out, the
	// queue for a slot (Try.Queued) left out
	Own float64 `json:"own,omitempty"`
	// Before: requests made on the way before the last one, from the
	// first's going out to the last's (a token refreshed, a request
	// asked again); Asks counts them all, the last one too
	Before float64 `json:"before,omitempty"`
	Asks   int     `json:"asks,omitempty"`
	// Pool: waiting for a connection that wasn't dialed for it, the
	// client's limit per host out; DNS, Connect and TLS are a new
	// connection's; Reused: an open connection carried it, none of the
	// three spent
	Pool    float64 `json:"pool,omitempty"`
	DNS     float64 `json:"dns,omitempty"`
	Connect float64 `json:"connect,omitempty"`
	TLS     float64 `json:"tls,omitempty"`
	Reused  bool    `json:"reused,omitempty"`
	// Send: writing the request, headers and body, on the connection
	Send float64 `json:"send,omitempty"`
	// Wait: the vendor's, from the request written to its reply's first
	// byte (its headers)
	Wait float64 `json:"wait,omitempty"`
	// Head: from the try's Start to that first byte, which the try's TTFT
	// (to the reply's first content) is counted from too
	Head float64 `json:"head,omitempty"`
}

// netCycle is one request's way out, from GetConn on.
type netCycle struct {
	getConn, dnsStart, dnsDone, connStart, connDone time.Time
	tlsStart, tlsDone, gotConn, wrote, firstByte    time.Time
	reused                                          bool
}

// netTrace keeps a try's requests' times: its hooks run on the client's
// goroutines, the resolver's and dialer's among them.
type netTrace struct {
	mu        sync.Mutex
	first     time.Time // the first request's GetConn
	asks      int
	cur, last netCycle // the one going out, the last one written
}

// hooks are the try's client trace: the times above, and asking, asked
// and wrote as the usage record's split has them (#1532).
func (n *netTrace) hooks(asking, asked, wrote *atomic.Int64) *httptrace.ClientTrace {
	at := func(f func(c *netCycle, now time.Time)) {
		now := time.Now()
		n.mu.Lock()
		f(&n.cur, now)
		n.mu.Unlock()
	}
	return &httptrace.ClientTrace{
		GetConn: func(string) {
			now := time.Now()
			asking.Store(now.UnixNano())
			n.mu.Lock()
			if n.first.IsZero() {
				n.first = now
			}
			n.asks++
			n.cur = netCycle{getConn: now}
			n.mu.Unlock()
		},
		DNSStart: func(httptrace.DNSStartInfo) {
			at(func(c *netCycle, now time.Time) {
				if c.dnsStart.IsZero() {
					c.dnsStart = now
				}
			})
		},
		DNSDone: func(httptrace.DNSDoneInfo) { at(func(c *netCycle, now time.Time) { c.dnsDone = now }) },
		// the dialer may try more than one address: the first start
		// and the last end
		ConnectStart: func(string, string) {
			at(func(c *netCycle, now time.Time) {
				if c.connStart.IsZero() {
					c.connStart = now
				}
			})
		},
		ConnectDone: func(string, string, error) { at(func(c *netCycle, now time.Time) { c.connDone = now }) },
		TLSHandshakeStart: func() {
			at(func(c *netCycle, now time.Time) {
				if c.tlsStart.IsZero() {
					c.tlsStart = now
				}
			})
		},
		TLSHandshakeDone: func(tls.ConnectionState, error) { at(func(c *netCycle, now time.Time) { c.tlsDone = now }) },
		GotConn: func(i httptrace.GotConnInfo) {
			at(func(c *netCycle, now time.Time) { c.gotConn, c.reused = now, i.Reused })
		},
		WroteRequest: func(httptrace.WroteRequestInfo) {
			asked.Store(asking.Load())
			wrote.Store(time.Now().UnixNano())
			at(func(c *netCycle, now time.Time) {
				c.wrote = now
				n.last = *c
			})
		},
		GotFirstResponseByte: func() {
			at(func(c *netCycle, now time.Time) {
				c.firstByte = now
				if c.getConn.Equal(n.last.getConn) {
					n.last.firstByte = now
				}
			})
		},
	}
}

// timing is the try's TryNet, from its Start, its time queued for a
// slot (ms) taken out of its own; nil when no request of it was written.
func (n *netTrace) timing(began time.Time, queued int64) *TryNet {
	n.mu.Lock()
	defer n.mu.Unlock()
	c := n.last
	if c.wrote.IsZero() || c.getConn.IsZero() {
		return nil
	}
	span := func(a, b time.Time) float64 {
		if a.IsZero() || b.IsZero() || b.Before(a) {
			return 0
		}
		return tenth(b.Sub(a))
	}
	t := &TryNet{Asks: n.asks, Reused: c.reused}
	t.Own = max(span(began, n.first)-float64(queued), 0)
	t.Before = span(n.first, c.getConn)
	if !c.reused {
		// a connection dialed while an open one came free went to the
		// pool: its dial wasn't this request's
		t.DNS = span(c.dnsStart, c.dnsDone)
		t.Connect = span(c.connStart, c.connDone)
		t.TLS = span(c.tlsStart, c.tlsDone)
	}
	if !c.gotConn.IsZero() {
		t.Pool = max(math.Round((span(c.getConn, c.gotConn)-t.DNS-t.Connect-t.TLS)*10)/10, 0)
		t.Send = span(c.gotConn, c.wrote)
	}
	if !c.firstByte.IsZero() {
		t.Wait = span(c.wrote, c.firstByte)
		t.Head = span(began, c.firstByte)
	}
	return t
}

// sentBy is what sends a request to p's vendor when uni-switch's own client
// doesn't, which TryNet can't follow: "plugin", its plugin's fetch in the
// plugin host; "claude", Claude Code's own run (the subscription bridge,
// attempt); "" for uni-switch's client. The Routing page says it in place of
// where the time went (Discord, lute: some requests had it and some
// didn't, with nothing said).
func sentBy(p provider.Provider) string {
	switch {
	case p.IsPlugin():
		return "plugin"
	case p.Account != nil && p.Account.Agent == "claude":
		return "claude"
	}
	return ""
}

// tenth is d in ms, to a tenth.
func tenth(d time.Duration) float64 {
	return math.Round(float64(d)/float64(time.Millisecond)*10) / 10
}

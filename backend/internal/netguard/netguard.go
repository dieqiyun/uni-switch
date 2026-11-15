// Package netguard is the one rule for where a download of a URL uni-switch
// didn't choose may connect: the gateway's (a caller's image, web_fetch, a
// vendor's image or video, generate_image's download) and a provider's
// icon. It judges addresses only; each caller decides what to do with a
// name's answers (pin them, or hand a proxy the name).
package netguard

import "net/netip"

// specialNets are ranges never fetched from beyond those net/netip names
// (loopback, private, link-local, multicast, unspecified).
var specialNets = func() []netip.Prefix {
	var out []netip.Prefix
	for _, s := range []string{
		"0.0.0.0/8",       // "this" network
		"100.64.0.0/10",   // CGNAT, Tailscale
		"192.0.0.0/24",    // IETF protocol assignments
		"192.0.2.0/24",    // documentation
		"198.18.0.0/15",   // benchmarking; a fake-IP resolver's (FakeIP)
		"198.51.100.0/24", // documentation
		"203.0.113.0/24",  // documentation
		"240.0.0.0/4",     // reserved, broadcast
		"::/96",           // IPv4-compatible (deprecated)
		"100::/64",        // discard
		"2001::/32",       // Teredo
		"2001:db8::/32",   // documentation
		"fec0::/10",       // site-local (deprecated)
		"64:ff9b:1::/48",  // local-use NAT64
	} {
		out = append(out, netip.MustParsePrefix(s))
	}
	return out
}()

var (
	nat64   = netip.MustParsePrefix("64:ff9b::/96")
	fakeNet = netip.MustParsePrefix("198.18.0.0/15")
	loonNet = netip.MustParsePrefix("fd27:712::/96")
)

// Public says a is on the open internet: not loopback, RFC 1918, fc00::/7,
// link-local (169.254.169.254 among them), CGNAT, unspecified, multicast,
// documentation, reserved, benchmarking, Teredo, nor an IPv4-mapped,
// NAT64 or 6to4 form of one. A fake-IP resolver's answer is not public
// (FakeIP says when it may be fetched from all the same).
func Public(a netip.Addr) bool {
	a = a.Unmap() // ::ffff:127.0.0.1 is 127.0.0.1
	if !a.IsValid() || a.IsLoopback() || a.IsPrivate() || a.IsUnspecified() ||
		a.IsLinkLocalUnicast() || a.IsLinkLocalMulticast() || a.IsInterfaceLocalMulticast() || a.IsMulticast() {
		return false
	}
	for _, n := range specialNets {
		if n.Contains(a) {
			return false
		}
	}
	if a.Is6() {
		b := a.As16()
		// an IPv4 address inside an IPv6 one: NAT64 (64:ff9b::/96) and
		// 6to4 (2002::/16) reach it
		if nat64.Contains(a) {
			return Public(netip.AddrFrom4([4]byte{b[12], b[13], b[14], b[15]}))
		}
		if b[0] == 0x20 && b[1] == 0x02 {
			return Public(netip.AddrFrom4([4]byte{b[2], b[3], b[4], b[5]}))
		}
	}
	return true
}

// FakeIP says a is in 198.18.0.0/15, RFC 2544's benchmarking range, where
// Clash's TUN mode with fake-ip, Clash Verge, Surge's enhanced mode and
// Loon answer every name, and carry a connection to it to the real host
// the name stands for (#252, #2593). No machine on the user's network
// sits there, so a connection to a fake-IP answer reaches the internet
// host the name stands for, or nothing; but which host that is, the
// fake-IP resolver decides where uni-switch can't see.
func FakeIP(a netip.Addr) bool { return fakeNet.Contains(a.Unmap()) }

// LoonPaired says a is the AAAA record Loon synthesizes for an A answer
// (its IPv4 address inside fd27:712::/96) and that A answer is among
// answers. Such an a stands for that IPv4 address, which the caller
// judges on its own; a is not dialled. An unpaired one, or any other
// fc00::/7 address, is private as ever.
func LoonPaired(a netip.Addr, answers []netip.Addr) bool {
	if !loonNet.Contains(a) {
		return false
	}
	b := a.As16()
	v4 := netip.AddrFrom4([4]byte{b[12], b[13], b[14], b[15]})
	for _, o := range answers {
		if o.Unmap() == v4 {
			return true
		}
	}
	return false
}

package settings

import (
	"fmt"
	"net"
	"strconv"
)

// LocalAddr validates a TCP listener and normalizes localhost without DNS.
// Wildcards, non-loopback IPs, zone IDs and arbitrary names never bind.
func LocalAddr(addr string) (string, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return "", fmt.Errorf("a local address is localhost:port or a loopback IP:port: %q", addr)
	}
	if host == "localhost" {
		host = "127.0.0.1"
	}
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		return "", fmt.Errorf("only loopback addresses are supported: %q", addr)
	}
	n, err := strconv.Atoi(port)
	if err != nil || n < 0 || n > 65535 {
		return "", fmt.Errorf("invalid local port: %q", port)
	}
	return net.JoinHostPort(ip.String(), strconv.Itoa(n)), nil
}

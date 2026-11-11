package netproxy

import "strings"

// parseWindows reads Internet Options' ProxyServer ("host:port", or
// "http=h:p;https=h:p;socks=h:p") and ProxyOverride ("a;*.b;<local>").
// HTTP is the server only when it is one for every protocol: reqwest
// takes ProxyServer as one address and can't read a list per protocol.
func parseWindows(server, override string) Proxy {
	server = strings.TrimSpace(server)
	if server == "" {
		return Proxy{}
	}
	bypass := strings.Split(override, ";")
	if !strings.Contains(server, "=") {
		if !strings.Contains(server, "://") {
			server = "http://" + server
		}
		return Proxy{URL: server, HTTP: server, Bypass: bypass}
	}
	by := map[string]string{}
	for _, part := range strings.Split(server, ";") {
		if k, v, ok := strings.Cut(part, "="); ok {
			by[strings.ToLower(strings.TrimSpace(k))] = strings.TrimSpace(v)
		}
	}
	entry := func(key, scheme string) string {
		v := by[key]
		if v != "" && !strings.Contains(v, "://") {
			v = scheme + "://" + v
		}
		return v
	}
	socks := entry("socks", "socks5")
	for _, u := range []string{entry("https", "http"), entry("http", "http"), socks} {
		if u != "" {
			return Proxy{URL: u, SOCKS: socks, Bypass: bypass}
		}
	}
	return Proxy{}
}

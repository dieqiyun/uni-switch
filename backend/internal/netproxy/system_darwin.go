package netproxy

// system reads what System Settings → Network → Proxies says, through
// scutil (the same dictionary CFNetwork uses).
func system() (Proxy, bool) { return readTool(parseScutil, "scutil", "--proxy") }

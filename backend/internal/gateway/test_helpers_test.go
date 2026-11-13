package gateway

import "strconv"

// Shared by the gateway's synthetic protocol and routing fixtures.
func itoa(i int) string { return strconv.Itoa(i) }

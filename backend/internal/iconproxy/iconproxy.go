// Package iconproxy resolves explicitly requested remote picture URLs.
package iconproxy

import (
	"net/url"
	"os"
)

// Base is empty because this distribution does not operate an image proxy.
const Base = ""

// URL uses an explicitly configured proxy, or the supplied picture URL.
func URL(u string) string {
	if v := os.Getenv("UNI_SWITCH_ICON_PROXY"); v != "" {
		return v + "?url=" + url.QueryEscape(u)
	}
	return u
}

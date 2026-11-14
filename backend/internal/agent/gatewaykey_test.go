package agent

import (
	"net/url"

	"strings"
)

// aionuiKey is the api_key AionUi's link hands it.
func aionuiKey(link string) string {
	v, _ := url.ParseQuery(strings.TrimPrefix(link, AionUiScheme))
	return v.Get("api_key")
}

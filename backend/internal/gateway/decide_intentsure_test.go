package gateway

import (
	"fmt"
)

// #5101: the confidence an intent needs of Jev was fixed at 0.4 for every
// group. A group may set its own (IntentSure): lower it and Jev's unsure
// picks count, raise it and fewer do, set it to 0 and every pick but
// "none of these" counts.

// chatFor is chat for the group named, not group/r.
func chatFor(group, first string, extra string) string {
	if extra != "" {
		extra = "," + extra
	}
	return fmt.Sprintf(`{"model":%s%s,"messages":[{"role":"user","content":%s}]}`, quote(group), extra, quote(first))
}

// ptrSure is the group's IntentSure as a test names it.
func ptrSure(sure float64) *float64 { return &sure }

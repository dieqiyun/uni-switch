package modelmeta

import (
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

var claudeLine = regexp.MustCompile(`(?:^|[^a-z0-9])(?:claude-)?(opus|sonnet|haiku|fable|mythos)-(\d{1,2})(?:[-.](\d{1,2}))?(?:[^0-9]|$)`)

// Compatibility projects Magpie's gateway adaptiveOnly/thinkingOffOf and
// samplingRefused into our existing bridge's switches. These are request-format
// constraints, not evidence of generic model capabilities.
func Compatibility(id, base string) map[string]any {
	out := map[string]any{}
	u, _ := url.Parse(base)
	if u != nil && strings.EqualFold(u.Hostname(), "api.deepseek.com") {
		out["thinkingFormat"] = "deepseek"
		return out
	}
	m := claudeLine.FindStringSubmatch(strings.ToLower(id))
	if m == nil {
		return out
	}
	major, _ := strconv.Atoi(m[2])
	minor, _ := strconv.Atoi(m[3])
	family := m[1]
	if major > 4 || major == 4 && minor >= 6 {
		out["thinkingFormat"] = "adaptive"
	} else {
		out["thinkingFormat"] = "budget"
	}
	never := (family == "fable" || family == "mythos") && major == 5 || family == "opus" && major == 5 && minor == 5
	between := family == "sonnet" && major == 5 && minor == 5
	if never || between {
		out["thinkingAlwaysOn"] = true
		out["thinkingBetweenTools"] = between
		out["forcedToolChoice"] = false
	}
	refused := (family == "fable" || family == "mythos") && major == 5 || family == "opus" && (major == 4 && minor >= 7 || major == 5 && (minor == 0 || minor == 5)) || family == "sonnet" && major == 5 && (minor == 0 || minor == 5) || family == "haiku" && major == 5 && minor == 5
	if refused {
		out["samplingParameters"] = false
	}
	// Mid-conversation system messages are a bridge formatting option, not
	// universally implied by a vendor name. Keep unknown unless the user says.
	return out
}

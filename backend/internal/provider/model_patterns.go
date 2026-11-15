package provider

// A routing group may name its members by pattern as well as by id (#766):
// "openrouter/*:free" is every free model OpenRouter lists, today's and
// next week's, where a list of ids goes stale as the vendor's catalog
// changes and a member left out is lost unsaid.
//
// The patterns are kept beside the members (Group.Match), never written
// into them: what they match is worked out each time the groups are read
// (groupsIn), from the models uni-switch serves now, so a model the vendor adds
// joins the group and one it drops leaves it without the group being saved
// again. Every reader — the gateway's routing, /v1/models, rules, the
// Routing view, the TUI and the CLI — gets the group from there, so all of
// them see the same members: those the user named, in their order, then
// those the patterns match, in the catalog's order, or in the order the
// user put them in (Group.Placed). A model named and matched both keeps
// the place it was named at.

import (
	"fmt"
	"regexp"

	"strings"
)

// RegexpPrefix starts a member that is a regular expression over the whole
// "provider/model" id, in any case as a glob is: "re:openrouter/.*:(free|beta)".
// "(?-i)" in it makes case matter from there on: "re:(?-i)openrouter/.*:FREE".
const RegexpPrefix = "re:"

// IsPattern reports whether a member as typed is a pattern rather than a
// model's id: a regular expression ("re:…") or a glob with "*" in it. No
// vendor's model id has a "*" in it, so a glob can't be taken for one.
func IsPattern(s string) bool {
	s = strings.TrimSpace(s)
	return strings.HasPrefix(s, RegexpPrefix) || strings.Contains(s, "*")
}

// compilePattern is a pattern as the regexp it means. A glob's "*" is any
// run of characters, "/" too, and the rest is the id as spelt, in any case
// ("openrouter/*:free", "*-free"); a regexp must match the whole id, in any
// case as a glob does unless it says "(?-i)" (usrname1833 on Discord).
func compilePattern(p string) (*regexp.Regexp, error) {
	if re, ok := strings.CutPrefix(p, RegexpPrefix); ok {
		if strings.TrimSpace(re) == "" {
			return nil, fmt.Errorf("%q has no regular expression after %s", p, RegexpPrefix)
		}
		x, err := regexp.Compile("(?i)^(?:" + re + ")$")
		if err != nil {
			return nil, fmt.Errorf("%q is not a regular expression uni-switch can read: %v", p, err)
		}
		return x, nil
	}
	parts := strings.Split(p, "*")
	for i, s := range parts {
		parts[i] = regexp.QuoteMeta(s)
	}
	return regexp.Compile("(?i)^" + strings.Join(parts, ".*") + "$")
}
